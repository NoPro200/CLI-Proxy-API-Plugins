package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// Registry remembers the alias of every virtual model the host has parsed, so
// the executor can refuse a target that is itself a virtual model even when a
// file was edited by hand.
type Registry struct {
	mu       sync.RWMutex
	byAuthID map[string]string
}

func NewRegistry() *Registry {
	return &Registry{byAuthID: make(map[string]string)}
}

func (r *Registry) set(authID, alias string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAuthID[authID] = alias
}

// Aliases returns every known virtual model alias.
func (r *Registry) Aliases() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byAuthID))
	for _, alias := range r.byAuthID {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

// IsAlias reports whether model is a known virtual model alias.
func (r *Registry) IsAlias(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, alias := range r.byAuthID {
		if strings.ToLower(alias) == model {
			return true
		}
	}
	return false
}

// Provider parses virtual model auth files. There is nothing to log in to:
// virtual models are created on the management page.
type Provider struct {
	registry *Registry
}

func NewProvider(registry *Registry) *Provider {
	return &Provider{registry: registry}
}

func (p *Provider) Identifier() string { return ProviderKey }

func (p *Provider) ParseAuth(_ context.Context, req pluginapi.AuthParseRequest) (pluginapi.AuthParseResponse, error) {
	model, errParse := ParseVirtualModel(req.RawJSON)
	if errors.Is(errParse, ErrNotModelMerger) {
		return pluginapi.AuthParseResponse{}, nil
	}
	if errParse != nil {
		return pluginapi.AuthParseResponse{Handled: true}, errParse
	}
	// Other aliases are unknown while the host loads files one by one; the
	// management page checks them, and the executor checks the registry.
	if errValidate := model.Validate(nil); errValidate != nil {
		return pluginapi.AuthParseResponse{Handled: true}, fmt.Errorf("%s: %w", req.FileName, errValidate)
	}
	auth := AuthData(req.FileName, model)
	p.registry.set(auth.ID, model.Alias)
	return pluginapi.AuthParseResponse{Handled: true, Auth: auth, Auths: []pluginapi.AuthData{auth}}, nil
}

func (p *Provider) StartLogin(context.Context, pluginapi.AuthLoginStartRequest) (pluginapi.AuthLoginStartResponse, error) {
	return pluginapi.AuthLoginStartResponse{}, fmt.Errorf("virtual models have no login; create them on /v0/resource/plugins/%s/models", PluginID)
}

func (p *Provider) PollLogin(context.Context, pluginapi.AuthLoginPollRequest) (pluginapi.AuthLoginPollResponse, error) {
	return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "virtual models have no login"}, nil
}

// RefreshAuth only re-reads the file: a virtual model holds no credentials.
func (p *Provider) RefreshAuth(_ context.Context, req pluginapi.AuthRefreshRequest) (pluginapi.AuthRefreshResponse, error) {
	model, errParse := ParseVirtualModel(req.StorageJSON)
	if errParse != nil {
		return pluginapi.AuthRefreshResponse{}, errParse
	}
	auth := AuthData(req.AuthID, model)
	return pluginapi.AuthRefreshResponse{Auth: auth}, nil
}

// AuthData turns a virtual model into the host's auth record.
func AuthData(id string, model VirtualModel) pluginapi.AuthData {
	id = strings.TrimSpace(id)
	if id == "" {
		id = FileName(model.Alias)
	}
	return pluginapi.AuthData{
		Provider:    ProviderKey,
		ID:          id,
		FileName:    id,
		Label:       model.Alias,
		Disabled:    !model.Enabled,
		StorageJSON: model.JSON(),
		// A virtual model is no credential: when its last target fails, the host
		// must not lock the alias. The targets' own auths still cool down.
		Metadata:   map[string]any{"disable_cooling": true},
		Attributes: map[string]string{"alias": model.Alias, "type_of_model": model.TypeOfModel},
	}
}

var _ pluginapi.AuthProvider = (*Provider)(nil)
