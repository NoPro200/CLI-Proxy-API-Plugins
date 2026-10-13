package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
)

type fakeHost struct {
	files map[string][]byte
	types map[string]string
}

func newFakeHost() *fakeHost {
	return &fakeHost{files: make(map[string][]byte), types: make(map[string]string)}
}

func (h *fakeHost) put(name, provider string, raw string) {
	h.files[name] = []byte(raw)
	h.types[name] = provider
}

func (h *fakeHost) ListAuths(context.Context) ([]pluginapi.HostAuthFileEntry, error) {
	out := make([]pluginapi.HostAuthFileEntry, 0, len(h.files))
	for name := range h.files {
		out = append(out, pluginapi.HostAuthFileEntry{Name: name, AuthIndex: "idx-" + name, Provider: h.types[name], Type: h.types[name]})
	}
	return out, nil
}

func (h *fakeHost) GetAuth(_ context.Context, index string) (pluginapi.HostAuthGetResponse, error) {
	name := strings.TrimPrefix(index, "idx-")
	raw, ok := h.files[name]
	if !ok {
		return pluginapi.HostAuthGetResponse{}, errors.New("not found")
	}
	return pluginapi.HostAuthGetResponse{AuthIndex: index, Name: name, JSON: raw}, nil
}

func (h *fakeHost) SaveAuth(_ context.Context, name string, raw json.RawMessage) (pluginapi.HostAuthSaveResponse, error) {
	h.put(name, authpkg.ProviderKey, string(raw))
	return pluginapi.HostAuthSaveResponse{Name: name}, nil
}

type fakeLoopback struct {
	models      map[string]string
	modelsErr   error
	deleted     []string
	deleteAuths []string
}

func (l *fakeLoopback) ModelIDs(context.Context) (map[string]string, error) {
	return l.models, l.modelsErr
}

func (l *fakeLoopback) DeleteAuthFile(_ context.Context, authorization, name string) error {
	l.deleteAuths = append(l.deleteAuths, authorization)
	l.deleted = append(l.deleted, name)
	return nil
}

func newFixture() (*Provider, *fakeHost, *fakeLoopback) {
	host := newFakeHost()
	host.put("venice-a.json", "venice", `{"type":"venice","cookie":"__client=secret-cookie"}`)
	loopback := &fakeLoopback{models: map[string]string{"gpt-image-2.5-sunburst": "openai", "qwen-image-3-pro": "venice"}}
	return New(loopback), host, loopback
}

func call(t *testing.T, p *Provider, host HostClient, method, path, body string, query url.Values) (int, map[string]any) {
	t.Helper()
	resp, err := p.HandleManagementWithHost(context.Background(), pluginapi.ManagementRequest{
		Method:  method,
		Path:    path,
		Body:    []byte(body),
		Query:   query,
		Headers: http.Header{"Authorization": []string{"Bearer mgmt-secret"}},
	}, host)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	var out map[string]any
	if strings.Contains(resp.Headers.Get("Content-Type"), "json") {
		if errDecode := json.Unmarshal(resp.Body, &out); errDecode != nil {
			t.Fatalf("decode %s: %v", resp.Body, errDecode)
		}
		if strings.Contains(string(resp.Body), "mgmt-secret") || strings.Contains(string(resp.Body), "secret-cookie") {
			t.Fatalf("response leaks a secret: %s", resp.Body)
		}
	}
	return resp.StatusCode, out
}

const jsonPath = "/v0/management/plugins/model-merger/models.json"

func TestCreateListUpdateDelete(t *testing.T) {
	p, host, loopback := newFixture()
	create := `{"alias":"mixed-image-gen","targets":[{"model":"gpt-image-2.5-sunburst"},{"model":"qwen-image-3-pro","timeout":120}],"fallback_on_moderation":true}`
	status, out := call(t, p, host, http.MethodPost, jsonPath, create, nil)
	if status != http.StatusCreated || out["name"] != "model-merger-mixed-image-gen.json" {
		t.Fatalf("create: %d %v", status, out)
	}
	saved, _ := authpkg.ParseVirtualModel(host.files["model-merger-mixed-image-gen.json"])
	if saved.Type != authpkg.StorageType || !saved.Enabled || !saved.FallbackOnModeration || len(saved.Targets) != 2 {
		t.Fatalf("saved = %+v", saved)
	}

	status, out = call(t, p, host, http.MethodGet, jsonPath, "", nil)
	models := out["models"].([]any)
	if status != http.StatusOK || len(models) != 1 {
		t.Fatalf("list: %d %v", status, out)
	}
	if available := out["available_models"].([]any); len(available) != 2 {
		t.Fatalf("available = %v", available)
	}

	// Reorder the targets and turn the model off.
	update := `{"name":"model-merger-mixed-image-gen.json","alias":"mixed-image-gen","enabled":false,"targets":[{"model":"qwen-image-3-pro"},{"model":"gpt-image-2.5-sunburst"}],"fallback_on_status":[500],"no_fallback_on_status":[503],"cooldown_seconds":30}`
	status, out = call(t, p, host, http.MethodPut, jsonPath, update, nil)
	if status != http.StatusOK {
		t.Fatalf("update: %d %v", status, out)
	}
	saved, _ = authpkg.ParseVirtualModel(host.files["model-merger-mixed-image-gen.json"])
	if saved.Enabled || saved.Targets[0].Model != "qwen-image-3-pro" || saved.CooldownSeconds != 30 || saved.FallbackOnModeration {
		t.Fatalf("updated = %+v", saved)
	}

	status, out = call(t, p, host, http.MethodDelete, jsonPath, "", url.Values{"name": {"model-merger-mixed-image-gen.json"}})
	if status != http.StatusOK || len(loopback.deleted) != 1 || loopback.deleteAuths[0] != "Bearer mgmt-secret" {
		t.Fatalf("delete: %d %v %v", status, out, loopback.deleted)
	}
}

func TestSaveRejectsInvalidModels(t *testing.T) {
	p, host, _ := newFixture()
	host.put("model-merger-other.json", authpkg.ProviderKey, `{"type":"model-merger","alias":"other","targets":[{"model":"qwen-image-3-pro"}]}`)
	tests := []struct {
		name   string
		body   string
		status int
	}{
		{"virtual target", `{"alias":"x","targets":[{"model":"other"}]}`, http.StatusBadRequest},
		{"duplicate target", `{"alias":"x","targets":[{"model":"a"},{"model":"a"}]}`, http.StatusBadRequest},
		{"no target", `{"alias":"x","targets":[]}`, http.StatusBadRequest},
		{"alias taken", `{"alias":"OTHER","targets":[{"model":"a"}]}`, http.StatusConflict},
		{"alias is a real model", `{"alias":"qwen-image-3-pro","targets":[{"model":"gpt-image-2.5-sunburst"}]}`, http.StatusConflict},
		{"null body", `null`, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if status, out := call(t, p, host, http.MethodPost, jsonPath, tt.body, nil); status != tt.status {
				t.Fatalf("status = %d, want %d: %v", status, tt.status, out)
			}
		})
	}
}

func TestUnknownTargetsOnlyWarn(t *testing.T) {
	p, host, loopback := newFixture()
	loopback.modelsErr = errors.New("connection refused")
	status, out := call(t, p, host, http.MethodPost, jsonPath, `{"alias":"x","targets":[{"model":"not-listed"}]}`, nil)
	if status != http.StatusCreated || out["warnings"] == nil {
		t.Fatalf("status = %d out = %v", status, out)
	}
}

func TestDeleteOnlyTouchesOwnFiles(t *testing.T) {
	p, host, loopback := newFixture()
	status, _ := call(t, p, host, http.MethodDelete, jsonPath, "", url.Values{"name": {"venice-a.json"}})
	if status != http.StatusNotFound || len(loopback.deleted) != 0 {
		t.Fatalf("status = %d deleted = %v", status, loopback.deleted)
	}
}

func TestUpdateNeedsExistingModel(t *testing.T) {
	p, host, _ := newFixture()
	status, _ := call(t, p, host, http.MethodPut, jsonPath, `{"name":"model-merger-missing.json","alias":"m","targets":[{"model":"a"}]}`, nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d", status)
	}
}

func TestPagesAreStaticAndKeyFree(t *testing.T) {
	p, host, _ := newFixture()
	for _, path := range []string{"/v0/resource/plugins/nopro200-model-merger/models", "/v0/management/plugins/model-merger/models"} {
		resp, err := p.HandleManagementWithHost(context.Background(), pluginapi.ManagementRequest{Method: http.MethodGet, Path: path, Headers: http.Header{"Authorization": []string{"Bearer mgmt-secret"}}}, host)
		if err != nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Headers.Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %v %d", path, err, resp.StatusCode)
		}
		page := string(resp.Body)
		if strings.Contains(page, "mgmt-secret") || !strings.Contains(page, "sessionStorage") {
			t.Fatalf("%s: page leaks the key or lacks the key form", path)
		}
		if !strings.Contains(page, "Bei Inhaltsfilter zum nächsten Modell wechseln") || !strings.Contains(page, "Das nächste Modell wendet seine eigenen Regeln an.") {
			t.Fatalf("%s: moderation switch text missing", path)
		}
	}
}

func TestRegisterManagementRoutes(t *testing.T) {
	resp, err := New(nil).RegisterManagement(context.Background(), pluginapi.ManagementRegistrationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, route := range resp.Routes {
		seen[route.Method+" "+route.Path] = true
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		for _, path := range []string{modelsPath, modelsJSONPath} {
			if !seen[method+" "+path] {
				t.Fatalf("missing route %s %s", method, path)
			}
		}
	}
	if len(resp.Resources) != 1 || resp.Resources[0].Path != resourcePath {
		t.Fatalf("resources = %+v", resp.Resources)
	}
}
