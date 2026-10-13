package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// HopHeader marks requests the plugin sends to the host, so a request that
// comes back to a virtual model through a misconfiguration stops at once.
const HopHeader = "X-Model-Merger-Hop"

// maxResponseBytes caps a loopback response; several base64 images fit easily.
const maxResponseBytes = 512 << 20

// ErrLoopbackUnconfigured reports a loopback without a base URL.
var ErrLoopbackUnconfigured = errors.New("model-merger: the loopback address of CLIProxyAPI is unknown")

// Loopback calls CLIProxyAPI's own HTTP API. The host's plugin callback
// host.model.execute refuses gpt-image-* models outside the images routes, so
// targets are reached through the public /v1/images/generations route instead.
type Loopback struct {
	BaseURL   string
	AccessKey string
	Client    *http.Client
}

// NewLoopback builds a client that never uses a proxy: the host is local.
func NewLoopback(baseURL, accessKey string) *Loopback {
	return &Loopback{
		BaseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		AccessKey: strings.TrimSpace(accessKey),
		Client:    &http.Client{Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: false}},
	}
}

// Configured reports whether the host can be reached.
func (l *Loopback) Configured() bool {
	return l != nil && l.BaseURL != ""
}

// HasAccessKey reports whether the plugin config sets its own key. Without
// one, the caller's own credentials are forwarded.
func (l *Loopback) HasAccessKey() bool {
	return l != nil && l.AccessKey != ""
}

// Response is a loopback reply; a transport failure leaves Status at 0.
type Response struct {
	Status  int
	Headers http.Header
	Body    []byte
}

// Do sends one request. On /v1 routes a configured access key wins over the
// given credentials; management routes always use the caller's management
// key. Without any credentials the request goes out bare, which a host
// without api-keys accepts.
func (l *Loopback) Do(ctx context.Context, method, path string, body []byte, authorization string, extra http.Header) (Response, error) {
	if l == nil || l.BaseURL == "" {
		return Response{}, ErrLoopbackUnconfigured
	}
	if l.AccessKey != "" && !strings.HasPrefix(path, "/v0/management/") {
		authorization = "Bearer " + l.AccessKey
		extra = withoutClientKeys(extra)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, errReq := http.NewRequestWithContext(ctx, method, l.BaseURL+path, reader)
	if errReq != nil {
		return Response{}, errReq
	}
	for key, values := range extra {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		return Response{}, errDo
	}
	defer resp.Body.Close()
	raw, errRead := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if errRead != nil {
		return Response{}, errRead
	}
	return Response{Status: resp.StatusCode, Headers: resp.Header, Body: raw}, nil
}

// ModelIDs lists the models a client key can use, for target suggestions and
// to keep an alias from shadowing a real model.
func (l *Loopback) ModelIDs(ctx context.Context, authorization string) (map[string]string, error) {
	resp, errDo := l.Do(ctx, http.MethodGet, "/v1/models", nil, authorization, nil)
	if errDo != nil {
		return nil, errDo
	}
	if resp.Status != http.StatusOK {
		return nil, fmt.Errorf("list models: status %d", resp.Status)
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if errDecode := json.Unmarshal(resp.Body, &payload); errDecode != nil {
		return nil, fmt.Errorf("decode models: %w", errDecode)
	}
	out := make(map[string]string, len(payload.Data))
	for _, model := range payload.Data {
		if id := strings.TrimSpace(model.ID); id != "" {
			out[id] = model.OwnedBy
		}
	}
	return out, nil
}

// ClientKey reads one of the host's api-keys with the caller's management key,
// so the management page can list models without a key in the plugin config.
// It returns "" when the host has no api-keys and accepts any request. The key
// stays in the plugin and is never sent back to the browser.
func (l *Loopback) ClientKey(ctx context.Context, managementAuthorization string) (string, error) {
	if strings.TrimSpace(managementAuthorization) == "" {
		return "", errors.New("management key is missing")
	}
	resp, errDo := l.Do(ctx, http.MethodGet, "/v0/management/api-keys", nil, managementAuthorization, nil)
	if errDo != nil {
		return "", errDo
	}
	if resp.Status != http.StatusOK {
		return "", fmt.Errorf("read api-keys: status %d", resp.Status)
	}
	var payload struct {
		Keys []string `json:"api-keys"`
	}
	if errDecode := json.Unmarshal(resp.Body, &payload); errDecode != nil {
		return "", fmt.Errorf("decode api-keys: %w", errDecode)
	}
	for _, key := range payload.Keys {
		if key = strings.TrimSpace(key); key != "" {
			return key, nil
		}
	}
	return "", nil
}

// withoutClientKeys drops forwarded client keys when the access key is used.
func withoutClientKeys(headers http.Header) http.Header {
	out := headers.Clone()
	for _, name := range clientKeyHeaders {
		out.Del(name)
	}
	return out
}

// DeleteAuthFile removes an auth file through the host's management API. The
// plugin host offers no delete callback, so the caller's management key is
// forwarded for this one request.
func (l *Loopback) DeleteAuthFile(ctx context.Context, managementAuthorization, name string) error {
	if strings.TrimSpace(managementAuthorization) == "" {
		return errors.New("management key is missing")
	}
	resp, errDo := l.Do(ctx, http.MethodDelete, "/v0/management/auth-files?name="+url.QueryEscape(name), nil, managementAuthorization, nil)
	if errDo != nil {
		return errDo
	}
	if resp.Status != http.StatusOK {
		return fmt.Errorf("delete %s: status %d: %s", name, resp.Status, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}
