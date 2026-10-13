package management

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
	"github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/executor"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

const (
	modelsPath     = "/plugins/model-merger/models"
	modelsJSONPath = "/plugins/model-merger/models.json"
	resourcePath   = "/models"
)

type HostClient interface {
	ListAuths(context.Context) ([]pluginapi.HostAuthFileEntry, error)
	GetAuth(context.Context, string) (pluginapi.HostAuthGetResponse, error)
	SaveAuth(context.Context, string, json.RawMessage) (pluginapi.HostAuthSaveResponse, error)
}

// Loopback is the part of the host's HTTP API the management page needs.
type Loopback interface {
	ModelIDs(context.Context) (map[string]string, error)
	DeleteAuthFile(ctx context.Context, managementAuthorization, name string) error
}

type Provider struct {
	loopback Loopback
}

func New(loopback Loopback) *Provider {
	return &Provider{loopback: loopback}
}

// modelEntry is one virtual model as the management API returns it. Auth
// files of this plugin hold no secrets, so the whole model is shown.
type modelEntry struct {
	Name          string `json:"name"`
	AuthIndex     string `json:"auth_index,omitempty"`
	Status        string `json:"status,omitempty"`
	StatusMessage string `json:"status_message,omitempty"`
	authpkg.VirtualModel
}

func (p *Provider) RegisterManagement(context.Context, pluginapi.ManagementRegistrationRequest) (pluginapi.ManagementRegistrationResponse, error) {
	routes := make([]pluginapi.ManagementRoute, 0, 8)
	for _, path := range []string{modelsPath, modelsJSONPath} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			routes = append(routes, pluginapi.ManagementRoute{
				Method:      method,
				Path:        path,
				Description: "Model Merger virtual models (" + method + ").",
				Handler:     p,
			})
		}
	}
	return pluginapi.ManagementRegistrationResponse{
		Routes: routes,
		Resources: []pluginapi.ResourceRoute{{
			Path:        resourcePath,
			Menu:        "Model Merger",
			Description: "Virtuelle Modelle mit Fallback verwalten.",
			Handler:     p,
		}},
	}, nil
}

func (p *Provider) HandleManagement(ctx context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	return p.HandleManagementWithHost(ctx, req, nil)
}

func (p *Provider) HandleManagementWithHost(ctx context.Context, req pluginapi.ManagementRequest, host HostClient) (pluginapi.ManagementResponse, error) {
	// The host serves /v0/resource/plugins/... without a management key, so
	// resource requests only get the static page, which loads its data from
	// the key-checked /v0/management routes.
	if strings.HasPrefix(strings.TrimSpace(req.Path), "/v0/resource/") {
		if strings.HasSuffix(strings.TrimRight(req.Path, "/"), resourcePath) {
			return pageResponse(), nil
		}
		return jsonResponse(http.StatusNotFound, map[string]any{"error": "not found"}), nil
	}
	switch strings.ToUpper(req.Method) {
	case http.MethodGet:
		if !wantsJSON(req) {
			return pageResponse(), nil
		}
		return p.list(ctx, host), nil
	case http.MethodPost:
		return p.save(ctx, req, host, true), nil
	case http.MethodPut:
		return p.save(ctx, req, host, false), nil
	case http.MethodDelete:
		return p.remove(ctx, req, host), nil
	default:
		return jsonResponse(http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"}), nil
	}
}

func (p *Provider) list(ctx context.Context, host HostClient) pluginapi.ManagementResponse {
	if host == nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": "CLIProxyAPI host callbacks are unavailable"})
	}
	entries, errList := p.entries(ctx, host)
	if errList != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": errList.Error()})
	}
	out := map[string]any{
		"provider": authpkg.ProviderKey,
		"models":   entries,
		"defaults": map[string]any{
			"fallback_on_status":  authpkg.DefaultFallbackStatus,
			"moderation_patterns": authpkg.DefaultModerationPatterns,
		},
	}
	if ids, errIDs := p.modelIDs(ctx); errIDs == nil {
		available := make([]string, 0, len(ids))
		for id, owner := range ids {
			if owner != authpkg.ProviderKey {
				available = append(available, id)
			}
		}
		sort.Strings(available)
		out["available_models"] = available
	} else {
		out["warnings"] = []string{"Modellliste nicht abrufbar: " + errIDs.Error()}
	}
	return jsonResponse(http.StatusOK, out)
}

// entries loads every model-merger auth file through the host.
func (p *Provider) entries(ctx context.Context, host HostClient) ([]modelEntry, error) {
	files, errList := host.ListAuths(ctx)
	if errList != nil {
		return nil, errList
	}
	out := make([]modelEntry, 0)
	for _, file := range files {
		if !isModelMergerEntry(file) || file.AuthIndex == "" {
			continue
		}
		got, errGet := host.GetAuth(ctx, file.AuthIndex)
		if errGet != nil {
			return nil, errGet
		}
		model, errParse := authpkg.ParseVirtualModel(got.JSON)
		if errParse != nil {
			continue
		}
		out = append(out, modelEntry{
			Name:          firstNonEmpty(file.Name, got.Name, file.ID),
			AuthIndex:     file.AuthIndex,
			Status:        file.Status,
			StatusMessage: file.StatusMessage,
			VirtualModel:  model,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Alias) < strings.ToLower(out[j].Alias) })
	return out, nil
}

func isModelMergerEntry(entry pluginapi.HostAuthFileEntry) bool {
	return strings.EqualFold(entry.Provider, authpkg.ProviderKey) || strings.EqualFold(entry.Type, authpkg.StorageType)
}

type saveRequest struct {
	// Name identifies the file to update; creating derives it from the alias.
	Name string `json:"name"`
}

func (p *Provider) save(ctx context.Context, req pluginapi.ManagementRequest, host HostClient, create bool) pluginapi.ManagementResponse {
	if host == nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": "CLIProxyAPI host callbacks are unavailable"})
	}
	var target saveRequest
	if errDecode := json.Unmarshal(req.Body, &target); errDecode != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
	}
	var raw map[string]json.RawMessage
	if errDecode := json.Unmarshal(req.Body, &raw); errDecode != nil || raw == nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "body must be a JSON object"})
	}
	delete(raw, "name")
	delete(raw, "auth_index")
	delete(raw, "status")
	delete(raw, "status_message")
	raw["type"], _ = json.Marshal(authpkg.StorageType)
	body, _ := json.Marshal(raw)
	model, errParse := authpkg.ParseVirtualModel(body)
	if errParse != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errParse.Error()})
	}

	existing, errList := p.entries(ctx, host)
	if errList != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": errList.Error()})
	}
	name := strings.TrimSpace(firstNonEmpty(target.Name, req.Query.Get("name")))
	if create {
		name = authpkg.FileName(model.Alias)
	} else if name == "" {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "name is required to update a virtual model"})
	}
	found := false
	otherAliases := make([]string, 0, len(existing))
	for _, entry := range existing {
		if entry.Name == name {
			found = true
			continue
		}
		otherAliases = append(otherAliases, entry.Alias)
		if strings.EqualFold(entry.Alias, model.Alias) {
			return jsonResponse(http.StatusConflict, map[string]any{"error": fmt.Sprintf("alias %q is already used by %s", model.Alias, entry.Name)})
		}
	}
	if create && found {
		return jsonResponse(http.StatusConflict, map[string]any{"error": fmt.Sprintf("%s already exists", name)})
	}
	if !create && !found {
		return jsonResponse(http.StatusNotFound, map[string]any{"error": fmt.Sprintf("virtual model %s not found", name)})
	}
	if errValidate := model.Validate(otherAliases); errValidate != nil {
		var validation *authpkg.ValidationError
		if errors.As(errValidate, &validation) {
			return jsonResponse(http.StatusBadRequest, map[string]any{"error": errValidate.Error(), "problems": validation.Problems})
		}
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": errValidate.Error()})
	}

	var warnings []string
	if ids, errIDs := p.modelIDs(ctx); errIDs == nil {
		// The host would route the alias to both this plugin and the real
		// provider, so an alias must not reuse a real model's name.
		if owner, taken := ids[model.Alias]; taken && owner != authpkg.ProviderKey {
			return jsonResponse(http.StatusConflict, map[string]any{"error": fmt.Sprintf("alias %q is the name of a real model", model.Alias)})
		}
		for _, target := range model.Targets {
			if _, ok := ids[target.Model]; !ok {
				warnings = append(warnings, fmt.Sprintf("Target %s ist derzeit nicht in /v1/models gelistet.", target.Model))
			}
		}
	} else {
		warnings = append(warnings, "Modellliste nicht abrufbar, Alias und Targets wurden nicht gegen echte Modelle geprüft: "+errIDs.Error())
	}

	saved, errSave := host.SaveAuth(ctx, name, model.JSON())
	if errSave != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": "saving failed: " + errSave.Error()})
	}
	status := http.StatusOK
	if create {
		status = http.StatusCreated
	}
	out := map[string]any{"name": firstNonEmpty(saved.Name, name), "model": model}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return jsonResponse(status, out)
}

func (p *Provider) remove(ctx context.Context, req pluginapi.ManagementRequest, host HostClient) pluginapi.ManagementResponse {
	if host == nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": "CLIProxyAPI host callbacks are unavailable"})
	}
	name := strings.TrimSpace(req.Query.Get("name"))
	if name == "" {
		var body saveRequest
		_ = json.Unmarshal(req.Body, &body)
		name = strings.TrimSpace(body.Name)
	}
	if name == "" {
		return jsonResponse(http.StatusBadRequest, map[string]any{"error": "name is required"})
	}
	existing, errList := p.entries(ctx, host)
	if errList != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": errList.Error()})
	}
	found := false
	for _, entry := range existing {
		found = found || entry.Name == name
	}
	// Only this plugin's own files can be deleted through this route.
	if !found {
		return jsonResponse(http.StatusNotFound, map[string]any{"error": fmt.Sprintf("virtual model %s not found", name)})
	}
	if p.loopback == nil {
		return jsonResponse(http.StatusServiceUnavailable, map[string]any{"error": executor.ErrLoopbackUnconfigured.Error()})
	}
	if errDelete := p.loopback.DeleteAuthFile(ctx, managementAuthorization(req.Headers), name); errDelete != nil {
		return jsonResponse(http.StatusBadGateway, map[string]any{"error": "deleting failed: " + errDelete.Error()})
	}
	return jsonResponse(http.StatusOK, map[string]any{"deleted": name})
}

// managementAuthorization returns the caller's management key as an
// Authorization header value; it is forwarded once and never stored.
func managementAuthorization(headers http.Header) string {
	if value := strings.TrimSpace(headers.Get("Authorization")); value != "" {
		return value
	}
	if key := strings.TrimSpace(headers.Get("X-Management-Key")); key != "" {
		return "Bearer " + key
	}
	return ""
}

func (p *Provider) modelIDs(ctx context.Context) (map[string]string, error) {
	if p.loopback == nil {
		return nil, executor.ErrLoopbackUnconfigured
	}
	return p.loopback.ModelIDs(ctx)
}

func wantsJSON(req pluginapi.ManagementRequest) bool {
	if strings.HasSuffix(req.Path, ".json") || strings.Contains(req.Query.Get("format"), "json") {
		return true
	}
	return strings.Contains(strings.ToLower(req.Headers.Get("Accept")), "application/json")
}

func jsonResponse(status int, value any) pluginapi.ManagementResponse {
	raw, _ := json.Marshal(value)
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}, "Cache-Control": []string{"no-store"}},
		Body:       raw,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
