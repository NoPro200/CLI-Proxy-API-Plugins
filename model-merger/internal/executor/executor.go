package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const imagesPath = "/v1/images/generations"

// TargetHeader names the target that produced a response.
const TargetHeader = "X-Model-Merger-Target"

// LogFunc reports fallback decisions to the host log.
type LogFunc func(level, message string, fields map[string]any)

// Executor serves a virtual model by trying its targets in order. It only
// moves forward: a failed fallback never returns to an earlier target.
type Executor struct {
	loopback  *Loopback
	registry  *authpkg.Registry
	cooldowns *cooldowns
	log       LogFunc
}

func NewExecutor(loopback *Loopback, registry *authpkg.Registry, log LogFunc) *Executor {
	if log == nil {
		log = func(string, string, map[string]any) {}
	}
	return &Executor{loopback: loopback, registry: registry, cooldowns: newCooldowns(), log: log}
}

func (e *Executor) Identifier() string { return authpkg.ProviderKey }

// attempt is the outcome of one target call.
type attempt struct {
	target       string
	resp         Response
	statusLost   bool
	timeout      bool
	timeoutAfter time.Duration
	transportErr error
}

func (a attempt) ok() bool {
	return a.transportErr == nil && !a.timeout && !a.statusLost && a.resp.Status >= 200 && a.resp.Status < 300
}

// moderationStatus is 0 when the host already sent 200 before the error (its
// non-streaming keep-alive), so the content filter check still runs.
func (a attempt) moderationStatus() int {
	if a.statusLost {
		return 0
	}
	return a.resp.Status
}

// reportedStatus is the status the status lists and the client see.
func (a attempt) reportedStatus() int {
	switch {
	case a.timeout:
		return http.StatusGatewayTimeout
	case a.transportErr != nil, a.statusLost:
		return http.StatusBadGateway
	default:
		return a.resp.Status
	}
}

// err returns the target's own error body, so the client sees what the last
// target said. CLIProxyAPI writes a JSON error message through unchanged.
func (a attempt) err() error {
	switch {
	case a.timeout:
		return newStatusError(http.StatusGatewayTimeout, "timeout", fmt.Sprintf("model-merger: target %s timed out after %s", a.target, a.timeoutAfter))
	case a.transportErr != nil:
		return newStatusError(http.StatusBadGateway, "transport_error", fmt.Sprintf("model-merger: target %s: %v", a.target, a.transportErr))
	}
	body := strings.TrimSpace(string(a.resp.Body))
	if body == "" || !json.Valid([]byte(body)) {
		return newStatusError(a.reportedStatus(), "upstream_error", fmt.Sprintf("model-merger: target %s failed with status %d: %s", a.target, a.resp.Status, truncate(body, 300)))
	}
	return statusError{status: a.reportedStatus(), body: []byte(body)}
}

func (e *Executor) Execute(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	if req.Headers.Get(HopHeader) != "" {
		return pluginapi.ExecutorResponse{}, newStatusError(http.StatusLoopDetected, "loop_detected", "model-merger: a virtual model was called by another virtual model; targets must be real models")
	}
	model, errParse := authpkg.ParseVirtualModel(req.StorageJSON)
	if errParse != nil {
		return pluginapi.ExecutorResponse{}, newStatusError(http.StatusInternalServerError, "config_error", "model-merger: "+errParse.Error())
	}
	if !model.Enabled {
		return pluginapi.ExecutorResponse{}, newStatusError(http.StatusServiceUnavailable, "model_disabled", "model-merger: virtual model "+model.Alias+" is disabled")
	}
	if model.TypeOfModel != authpkg.ImageModelType || req.SourceFormat != authpkg.ImageModelType || isImageEditRequest(req) {
		return pluginapi.ExecutorResponse{}, badRequest("model-merger: %s only serves %s", model.Alias, imagesPath)
	}
	if !e.loopback.Configured() {
		return pluginapi.ExecutorResponse{}, newStatusError(http.StatusInternalServerError, "config_error", ErrLoopbackUnconfigured.Error())
	}
	payload := req.Payload
	if len(payload) == 0 {
		payload = req.OriginalRequest
	}
	var request map[string]json.RawMessage
	if errDecode := json.Unmarshal(payload, &request); errDecode != nil || request == nil {
		return pluginapi.ExecutorResponse{}, badRequest("model-merger: request body must be a JSON object")
	}

	var last attempt
	for i, target := range e.plan(model) {
		if e.registry != nil && e.registry.IsAlias(target.Model) {
			return pluginapi.ExecutorResponse{}, newStatusError(http.StatusInternalServerError, "config_error", fmt.Sprintf("model-merger: target %s of %s is a virtual model", target.Model, model.Alias))
		}
		last = e.try(ctx, model.Alias, target, request)
		if last.ok() {
			e.cooldowns.clear(model.Alias, target.Model)
			if i > 0 {
				e.log("info", "model-merger: fallback target answered", map[string]any{"alias": model.Alias, "target": target.Model, "position": i + 1})
			}
			return pluginapi.ExecutorResponse{
				Payload: last.resp.Body,
				Headers: http.Header{"Content-Type": []string{"application/json"}, TargetHeader: []string{target.Model}},
			}, nil
		}
		if errCtx := ctx.Err(); errCtx != nil {
			// The client is gone; no later target can answer it.
			return pluginapi.ExecutorResponse{}, errCtx
		}
		v := decide(model, last)
		e.log("warn", "model-merger: target failed", map[string]any{"alias": model.Alias, "target": target.Model, "status": last.reportedStatus(), "reason": v.reason, "fallback": v.fallback})
		if !v.fallback {
			return pluginapi.ExecutorResponse{}, last.err()
		}
		// A blocked prompt says nothing about the target's health.
		if !v.moderation {
			e.cooldowns.start(model.Alias, target.Model, model.CooldownSeconds)
		}
	}
	return pluginapi.ExecutorResponse{}, last.err()
}

// plan keeps the configured order and skips targets that are cooling down.
// When every target is cooling down, all of them are tried in order anyway.
func (e *Executor) plan(model authpkg.VirtualModel) []authpkg.Target {
	out := make([]authpkg.Target, 0, len(model.Targets))
	for _, target := range model.Targets {
		if !e.cooldowns.active(model.Alias, target.Model) {
			out = append(out, target)
		}
	}
	if len(out) == 0 {
		return model.Targets
	}
	return out
}

// try sends the client's request unchanged except for the model name. The
// prompt is never rewritten: each target applies its own rules.
func (e *Executor) try(ctx context.Context, alias string, target authpkg.Target, request map[string]json.RawMessage) attempt {
	result := attempt{target: target.Model}
	body := make(map[string]json.RawMessage, len(request))
	for key, value := range request {
		body[key] = value
	}
	body["model"], _ = json.Marshal(target.Model)
	raw, errMarshal := json.Marshal(body)
	if errMarshal != nil {
		result.transportErr = errMarshal
		return result
	}
	callCtx := ctx
	if target.Timeout > 0 {
		result.timeoutAfter = time.Duration(target.Timeout) * time.Second
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, result.timeoutAfter)
		defer cancel()
	}
	resp, errDo := e.loopback.Do(callCtx, http.MethodPost, imagesPath, raw, "", http.Header{HopHeader: []string{alias}})
	if errDo != nil {
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			result.timeout = true
		} else {
			result.transportErr = errDo
		}
		return result
	}
	result.resp = resp
	if resp.Status >= 200 && resp.Status < 300 && !hasImages(resp.Body) {
		// With non-streaming keep-alive on, the host commits 200 before it
		// knows the outcome and then writes the error body.
		result.statusLost = true
	}
	return result
}

func hasImages(body []byte) bool {
	var payload struct {
		Data []json.RawMessage `json:"data"`
	}
	return json.Unmarshal(body, &payload) == nil && len(payload.Data) > 0
}

func isImageEditRequest(req pluginapi.ExecutorRequest) bool {
	if path, _ := req.Metadata["request_path"].(string); strings.HasSuffix(path, "/images/edits") {
		return true
	}
	mediaType, _, _ := mime.ParseMediaType(req.Headers.Get("Content-Type"))
	return strings.HasPrefix(mediaType, "multipart/")
}

func (e *Executor) ExecuteStream(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
	return pluginapi.ExecutorStreamResponse{}, badRequest("model-merger: virtual image models do not stream; send stream=false")
}

func (e *Executor) CountTokens(context.Context, pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return pluginapi.ExecutorResponse{}, badRequest("model-merger: token counting is not supported")
}

func (e *Executor) HttpRequest(context.Context, pluginapi.ExecutorHTTPRequest) (pluginapi.ExecutorHTTPResponse, error) {
	return pluginapi.ExecutorHTTPResponse{}, badRequest("model-merger: raw HTTP requests are not supported")
}

// statusError carries the HTTP status to CLIProxyAPI, which writes a JSON body
// to the client as is.
type statusError struct {
	status int
	body   []byte
}

func (e statusError) Error() string   { return string(e.body) }
func (e statusError) StatusCode() int { return e.status }

func newStatusError(status int, code, message string) statusError {
	errType := "server_error"
	if status >= 400 && status < 500 {
		errType = "invalid_request_error"
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"message": message, "type": errType, "code": code}})
	return statusError{status: status, body: body}
}

func badRequest(format string, args ...any) error {
	return newStatusError(http.StatusBadRequest, "invalid_request", fmt.Sprintf(format, args...))
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

var _ pluginapi.ProviderExecutor = (*Executor)(nil)
