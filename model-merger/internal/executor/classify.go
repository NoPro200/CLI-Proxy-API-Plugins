package executor

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
)

// errorDetails is what the classifier reads from an OpenAI-style error body.
type errorDetails struct {
	Codes    []string
	Types    []string
	Messages []string
}

// parseErrorDetails collects code, type and message from {"error":{...}},
// {"error":"..."} and {"message":"..."} bodies. CLIProxyAPI often wraps an
// upstream error body as the message string, so JSON messages are unpacked too.
func parseErrorDetails(body []byte) errorDetails {
	var out errorDetails
	collectErrorDetails(body, &out, 0)
	return out
}

func collectErrorDetails(body []byte, out *errorDetails, depth int) {
	trimmed := strings.TrimSpace(string(body))
	if depth > 3 || trimmed == "" {
		return
	}
	var decoded struct {
		Error   json.RawMessage `json:"error"`
		Message string          `json:"message"`
		Code    any             `json:"code"`
		Type    string          `json:"type"`
	}
	if json.Unmarshal([]byte(trimmed), &decoded) != nil {
		out.Messages = append(out.Messages, trimmed)
		return
	}
	addString(&out.Codes, anyString(decoded.Code))
	addString(&out.Types, decoded.Type)
	addMessage(out, decoded.Message, depth)
	if len(decoded.Error) == 0 {
		return
	}
	var text string
	if json.Unmarshal(decoded.Error, &text) == nil {
		addMessage(out, text, depth)
		return
	}
	collectErrorDetails(decoded.Error, out, depth+1)
}

func addMessage(out *errorDetails, message string, depth int) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if strings.HasPrefix(message, "{") && json.Valid([]byte(message)) {
		collectErrorDetails([]byte(message), out, depth+1)
		return
	}
	out.Messages = append(out.Messages, message)
}

func addString(list *[]string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		*list = append(*list, value)
	}
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

// isModerationBlock recognises a content filter block: a 400 whose error code
// is moderation_blocked, whose type is image_generation_user_error, or whose
// message contains one of the configured patterns. Status 0 stands for a reply
// whose status was lost (see attempt.statusLost) and is checked the same way.
func isModerationBlock(status int, body []byte, patterns []string) bool {
	if status != http.StatusBadRequest && status != 0 {
		return false
	}
	details := parseErrorDetails(body)
	if slices.ContainsFunc(details.Codes, func(code string) bool { return strings.EqualFold(code, "moderation_blocked") }) {
		return true
	}
	if slices.ContainsFunc(details.Types, func(kind string) bool { return strings.EqualFold(kind, "image_generation_user_error") }) {
		return true
	}
	for _, message := range details.Messages {
		lower := strings.ToLower(message)
		for _, pattern := range patterns {
			if pattern = strings.ToLower(strings.TrimSpace(pattern)); pattern != "" && strings.Contains(lower, pattern) {
				return true
			}
		}
	}
	return false
}

// verdict says what to do after a failed target.
type verdict struct {
	fallback   bool
	moderation bool
	reason     string
}

// decide applies the virtual model's rules to one failed attempt. A content
// filter block is decided by fallback_on_moderation alone, so a 400 listed in
// fallback_on_status never moves a blocked prompt on behind the user's back.
func decide(model authpkg.VirtualModel, a attempt) verdict {
	switch {
	case a.timeout:
		return verdict{fallback: true, reason: "timeout"}
	case a.transportErr != nil:
		return verdict{fallback: true, reason: "transport error"}
	}
	if isModerationBlock(a.moderationStatus(), a.resp.Body, model.ModerationPatterns) {
		if model.FallbackOnModeration {
			return verdict{fallback: true, moderation: true, reason: "content filter"}
		}
		return verdict{moderation: true, reason: "content filter (fallback_on_moderation is off)"}
	}
	status := a.reportedStatus()
	if slices.Contains(model.NoFallbackOnStatus, status) {
		return verdict{reason: "status listed in no_fallback_on_status"}
	}
	if slices.Contains(model.FallbackOnStatus, status) {
		return verdict{fallback: true, reason: "status listed in fallback_on_status"}
	}
	return verdict{reason: "status not listed in fallback_on_status"}
}
