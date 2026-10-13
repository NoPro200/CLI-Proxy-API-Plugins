package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
)

const moderationBody = `{"error":{"message":"Your request was rejected by the safety system.","type":"image_generation_user_error","code":"moderation_blocked"}}`

// fakeHost stands in for CLIProxyAPI's /v1/images/generations.
type fakeHost struct {
	mu      sync.Mutex
	replies map[string]func(w http.ResponseWriter)
	calls   []map[string]any
	auth    []string
	hops    []string
}

func newFakeHost(t *testing.T) (*fakeHost, *httptest.Server) {
	host := &fakeHost{replies: make(map[string]func(w http.ResponseWriter))}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		host.mu.Lock()
		host.calls = append(host.calls, body)
		host.auth = append(host.auth, r.Header.Get("Authorization"))
		host.hops = append(host.hops, r.Header.Get(HopHeader))
		reply := host.replies[body["model"].(string)]
		host.mu.Unlock()
		if reply == nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"no reply configured"}}`))
			return
		}
		reply(w)
	}))
	t.Cleanup(srv.Close)
	return host, srv
}

func (h *fakeHost) models() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.calls))
	for _, call := range h.calls {
		out = append(out, call["model"].(string))
	}
	return out
}

func reply(status int, body string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func imageReply(tag string) func(w http.ResponseWriter) {
	return reply(http.StatusOK, `{"created":1,"data":[{"b64_json":"`+tag+`"}]}`)
}

func virtualModel(t *testing.T, extra string) []byte {
	t.Helper()
	raw := `{"type":"model-merger","alias":"mixed-image-gen","type_of_model":"openai-image","targets":[{"model":"gpt-image-2.5-sunburst"},{"model":"qwen-image-3-pro"}]` + extra + `}`
	if !json.Valid([]byte(raw)) {
		t.Fatalf("invalid test model: %s", raw)
	}
	return []byte(raw)
}

func imageRequest(storage []byte) pluginapi.ExecutorRequest {
	return pluginapi.ExecutorRequest{
		Model:        "mixed-image-gen",
		SourceFormat: authpkg.ImageModelType,
		Payload:      []byte(`{"model":"mixed-image-gen","prompt":"a red fox in the snow","size":"1536x1024","n":1,"response_format":"b64_json"}`),
		StorageJSON:  storage,
		Headers:      http.Header{},
	}
}

func newTestExecutor(srv *httptest.Server) *Executor {
	return NewExecutor(NewLoopback(srv.URL, "test-access-key"), authpkg.NewRegistry(), nil)
}

func statusOf(t *testing.T, err error) (int, string) {
	t.Helper()
	var withStatus interface{ StatusCode() int }
	if !errors.As(err, &withStatus) {
		t.Fatalf("error has no status: %v", err)
	}
	return withStatus.StatusCode(), err.Error()
}

func TestPrimarySuccessSkipsFallback(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = imageReply("primary")
	resp, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, "")))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(string(resp.Payload), "primary") || resp.Headers.Get(TargetHeader) != "gpt-image-2.5-sunburst" {
		t.Fatalf("payload = %s headers = %v", resp.Payload, resp.Headers)
	}
	if got := host.models(); len(got) != 1 {
		t.Fatalf("calls = %v", got)
	}
	if host.auth[0] != "Bearer test-access-key" || host.hops[0] != "mixed-image-gen" {
		t.Fatalf("auth = %q hop = %q", host.auth[0], host.hops[0])
	}
}

func TestServerErrorFallsBackWithSameRequest(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusBadGateway, `{"error":{"message":"upstream down"}}`)
	host.replies["qwen-image-3-pro"] = imageReply("fallback")
	resp, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, "")))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(string(resp.Payload), "fallback") {
		t.Fatalf("payload = %s", resp.Payload)
	}
	calls := host.calls
	if len(calls) != 2 || calls[0]["model"] != "gpt-image-2.5-sunburst" || calls[1]["model"] != "qwen-image-3-pro" {
		t.Fatalf("calls = %v", host.models())
	}
	// Only the model differs; the prompt and every other field are sent as is.
	for _, key := range []string{"prompt", "size", "n", "response_format"} {
		if calls[0][key] != calls[1][key] {
			t.Fatalf("%s changed: %v -> %v", key, calls[0][key], calls[1][key])
		}
	}
	if calls[1]["prompt"] != "a red fox in the snow" {
		t.Fatalf("prompt = %v", calls[1]["prompt"])
	}
}

func TestModerationBlockedWithSwitchOffReturnsTheBlock(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusBadRequest, moderationBody)
	host.replies["qwen-image-3-pro"] = imageReply("fallback")
	// 400 is even listed for fallback: the switch alone decides on blocks.
	_, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, `,"fallback_on_moderation":false,"fallback_on_status":[400,500]`)))
	if err == nil {
		t.Fatal("expected the content filter error")
	}
	status, body := statusOf(t, err)
	if status != http.StatusBadRequest || body != moderationBody {
		t.Fatalf("status = %d body = %s", status, body)
	}
	if got := host.models(); len(got) != 1 {
		t.Fatalf("the fallback was called: %v", got)
	}
}

func TestModerationBlockedWithSwitchOnTriesNextTarget(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusBadRequest, moderationBody)
	host.replies["qwen-image-3-pro"] = imageReply("fallback")
	resp, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, `,"fallback_on_moderation":true,"cooldown_seconds":60`)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Headers.Get(TargetHeader) != "qwen-image-3-pro" {
		t.Fatalf("answered by %q", resp.Headers.Get(TargetHeader))
	}
	if host.calls[0]["prompt"] != host.calls[1]["prompt"] {
		t.Fatal("the prompt was changed for the fallback")
	}
	// A blocked prompt does not put the primary into cooldown.
	host.replies["gpt-image-2.5-sunburst"] = imageReply("primary")
	resp, err = newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, `,"fallback_on_moderation":true,"cooldown_seconds":60`)))
	if err != nil || resp.Headers.Get(TargetHeader) != "gpt-image-2.5-sunburst" {
		t.Fatalf("second request: err=%v target=%q", err, resp.Headers.Get(TargetHeader))
	}
}

func TestModerationOnEveryTargetReturnsTheLastError(t *testing.T) {
	host, srv := newFakeHost(t)
	lastBody := `{"error":{"message":"venice: content policy violation","type":"invalid_request_error"}}`
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusBadRequest, moderationBody)
	host.replies["qwen-image-3-pro"] = reply(http.StatusBadRequest, lastBody)
	_, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, `,"fallback_on_moderation":true`)))
	status, body := statusOf(t, err)
	if status != http.StatusBadRequest || body != lastBody {
		t.Fatalf("status = %d body = %s", status, body)
	}
}

func TestFailedFallbackNeverReturnsToPrimary(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusServiceUnavailable, `{"error":{"message":"primary busy"}}`)
	host.replies["qwen-image-3-pro"] = reply(http.StatusInternalServerError, `{"error":{"message":"qwen broke"}}`)
	_, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, "")))
	status, body := statusOf(t, err)
	if status != http.StatusInternalServerError || !strings.Contains(body, "qwen broke") {
		t.Fatalf("status = %d body = %s", status, body)
	}
	if got := host.models(); len(got) != 2 || got[0] != "gpt-image-2.5-sunburst" || got[1] != "qwen-image-3-pro" {
		t.Fatalf("calls = %v", got)
	}
}

func TestUnlistedStatusStopsTheChain(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusBadRequest, `{"error":{"message":"size not supported","type":"invalid_request_error"}}`)
	_, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, "")))
	if status, _ := statusOf(t, err); status != http.StatusBadRequest {
		t.Fatalf("status = %d", status)
	}
	if got := host.models(); len(got) != 1 {
		t.Fatalf("calls = %v", got)
	}
}

func TestNoFallbackStatusWins(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`)
	_, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, `,"no_fallback_on_status":[429]`)))
	if status, _ := statusOf(t, err); status != http.StatusTooManyRequests {
		t.Fatalf("status = %d", status)
	}
	if got := host.models(); len(got) != 1 {
		t.Fatalf("calls = %v", got)
	}
}

func TestTimeoutFallsBack(t *testing.T) {
	host, srv := newFakeHost(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	host.replies["gpt-image-2.5-sunburst"] = func(w http.ResponseWriter) { <-release }
	host.replies["qwen-image-3-pro"] = imageReply("fallback")
	storage := []byte(`{"type":"model-merger","alias":"mixed-image-gen","targets":[{"model":"gpt-image-2.5-sunburst","timeout":1},{"model":"qwen-image-3-pro"}]}`)
	start := time.Now()
	resp, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(storage))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.Headers.Get(TargetHeader) != "qwen-image-3-pro" || time.Since(start) > 10*time.Second {
		t.Fatalf("target = %q after %s", resp.Headers.Get(TargetHeader), time.Since(start))
	}
}

func TestTransportErrorFallsBackAndIsReported(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // nothing listens any more
	executor := NewExecutor(NewLoopback(srv.URL, "k"), authpkg.NewRegistry(), nil)
	_, err := executor.Execute(context.Background(), imageRequest(virtualModel(t, "")))
	status, body := statusOf(t, err)
	if status != http.StatusBadGateway || !strings.Contains(body, "qwen-image-3-pro") {
		t.Fatalf("status = %d body = %s", status, body)
	}
}

func TestCooldownSkipsFailedTargetButKeepsOrder(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusServiceUnavailable, `{"error":{"message":"busy"}}`)
	host.replies["qwen-image-3-pro"] = imageReply("fallback")
	executor := newTestExecutor(srv)
	storage := virtualModel(t, `,"cooldown_seconds":300`)
	for i := 0; i < 2; i++ {
		if _, err := executor.Execute(context.Background(), imageRequest(storage)); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if got := host.models(); strings.Join(got, ",") != "gpt-image-2.5-sunburst,qwen-image-3-pro,qwen-image-3-pro" {
		t.Fatalf("calls = %v", got)
	}
	// Once the cooldown is over the primary is first again.
	executor.cooldowns.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	host.replies["gpt-image-2.5-sunburst"] = imageReply("primary")
	resp, err := executor.Execute(context.Background(), imageRequest(storage))
	if err != nil || resp.Headers.Get(TargetHeader) != "gpt-image-2.5-sunburst" {
		t.Fatalf("after cooldown: err=%v target=%q", err, resp.Headers.Get(TargetHeader))
	}
}

func TestKeepAliveStatusLostIsTreatedAsFailure(t *testing.T) {
	host, srv := newFakeHost(t)
	host.replies["gpt-image-2.5-sunburst"] = reply(http.StatusOK, "\n\n"+`{"error":{"message":"upstream exploded"}}`)
	host.replies["qwen-image-3-pro"] = imageReply("fallback")
	resp, err := newTestExecutor(srv).Execute(context.Background(), imageRequest(virtualModel(t, "")))
	if err != nil || resp.Headers.Get(TargetHeader) != "qwen-image-3-pro" {
		t.Fatalf("err=%v target=%q", err, resp.Headers.Get(TargetHeader))
	}
}

func TestLoopAndMisuseGuards(t *testing.T) {
	_, srv := newFakeHost(t)
	executor := newTestExecutor(srv)

	req := imageRequest(virtualModel(t, ""))
	req.Headers.Set(HopHeader, "other-alias")
	if _, err := executor.Execute(context.Background(), req); err == nil || !strings.Contains(err.Error(), "loop_detected") {
		t.Fatalf("hop header: %v", err)
	}

	registry := authpkg.NewRegistry()
	provider := authpkg.NewProvider(registry)
	_, _ = provider.ParseAuth(context.Background(), pluginapi.AuthParseRequest{FileName: "b.json", RawJSON: []byte(`{"type":"model-merger","alias":"qwen-image-3-pro","targets":[{"model":"x"}]}`)})
	guarded := NewExecutor(NewLoopback(srv.URL, "k"), registry, nil)
	if _, err := guarded.Execute(context.Background(), imageRequest([]byte(`{"type":"model-merger","alias":"a","targets":[{"model":"qwen-image-3-pro"}]}`))); err == nil || !strings.Contains(err.Error(), "is a virtual model") {
		t.Fatalf("virtual target: %v", err)
	}

	chat := imageRequest(virtualModel(t, ""))
	chat.SourceFormat = "openai"
	if _, err := executor.Execute(context.Background(), chat); err == nil {
		t.Fatal("a chat request was accepted")
	}

	disabled := imageRequest(virtualModel(t, `,"enabled":false`))
	if _, err := executor.Execute(context.Background(), disabled); err == nil {
		t.Fatal("a disabled model answered")
	}

	unconfigured := NewExecutor(NewLoopback(srv.URL, ""), authpkg.NewRegistry(), nil)
	if _, err := unconfigured.Execute(context.Background(), imageRequest(virtualModel(t, ""))); err == nil || !strings.Contains(err.Error(), "access-key") {
		t.Fatalf("missing access key: %v", err)
	}
}

func TestClientCancelStopsTheChain(t *testing.T) {
	host, srv := newFakeHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	host.replies["gpt-image-2.5-sunburst"] = func(w http.ResponseWriter) {
		cancel()
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusBadGateway)
	}
	host.replies["qwen-image-3-pro"] = imageReply("fallback")
	if _, err := newTestExecutor(srv).Execute(ctx, imageRequest(virtualModel(t, ""))); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got := host.models(); len(got) != 1 {
		t.Fatalf("calls = %v", got)
	}
}
