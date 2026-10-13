package models

import (
	"context"
	"strings"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// Provider lists each virtual model under its alias. Every auth file holds
// exactly one virtual model, so there are no static models.
type Provider struct{}

func NewProvider() *Provider { return &Provider{} }

func (p *Provider) StaticModels(context.Context, pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
	return pluginapi.ModelResponse{Provider: authpkg.ProviderKey, Models: []pluginapi.ModelInfo{}}, nil
}

func (p *Provider) ModelsForAuth(_ context.Context, req pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	model, errParse := authpkg.ParseVirtualModel(req.StorageJSON)
	if errParse != nil || !model.Enabled || model.Validate(nil) != nil {
		// A disabled or broken virtual model disappears from /v1/models.
		return pluginapi.ModelResponse{Provider: authpkg.ProviderKey, Models: []pluginapi.ModelInfo{}}, nil
	}
	return pluginapi.ModelResponse{Provider: authpkg.ProviderKey, Models: []pluginapi.ModelInfo{ModelInfo(model)}}, nil
}

// ModelInfo describes a virtual model the way CLIProxyAPI lists image models,
// so /v1/images/generations accepts its alias.
func ModelInfo(model authpkg.VirtualModel) pluginapi.ModelInfo {
	chain := make([]string, 0, len(model.Targets))
	for _, target := range model.Targets {
		chain = append(chain, target.Model)
	}
	return pluginapi.ModelInfo{
		ID:                         model.Alias,
		Object:                     "model",
		OwnedBy:                    authpkg.ProviderKey,
		Type:                       authpkg.ImageModelType,
		DisplayName:                model.Alias,
		Name:                       model.Alias,
		Description:                "Virtual model: " + strings.Join(chain, " -> "),
		SupportedGenerationMethods: []string{"images.generations"},
		SupportedInputModalities:   []string{"text"},
		SupportedOutputModalities:  []string{"image"},
	}
}
