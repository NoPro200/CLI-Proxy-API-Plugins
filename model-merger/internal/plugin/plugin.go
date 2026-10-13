package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
	"github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/executor"
	"github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/management"
	"github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/models"
)

const (
	DisplayName = "Model Merger (NoPro200)"
	Description = "Virtuelle Modelle, die Anfragen der Reihe nach an mehrere echte Modelle weiterleiten (Fallback), mit eigener Verwaltungsseite."
	defaultPort = 8317
)

// Config is the plugin's block under plugins.configs.nopro200-model-merger in
// CLIProxyAPI's config.yaml. The access key must be one of the host's api-keys;
// it is only ever sent to the loopback address.
type Config struct {
	Port        int    `yaml:"port"`
	LoopbackURL string `yaml:"loopback-url"`
	AccessKey   string `yaml:"access-key"`
}

// ParseConfig reads the plugin config; a missing or broken block falls back
// to the defaults, and the executor then reports the missing access key.
func ParseConfig(configYAML []byte) Config {
	var cfg Config
	_ = yaml.Unmarshal(configYAML, &cfg)
	cfg.LoopbackURL = strings.TrimRight(strings.TrimSpace(cfg.LoopbackURL), "/")
	cfg.AccessKey = strings.TrimSpace(cfg.AccessKey)
	if cfg.Port <= 0 {
		cfg.Port = defaultPort
	}
	if cfg.LoopbackURL == "" {
		cfg.LoopbackURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	}
	return cfg
}

type ModelMergerPlugin struct {
	auth     *authpkg.Provider
	models   *models.Provider
	executor *executor.Executor
	mgmt     *management.Provider
}

func New(cfg Config, log executor.LogFunc) *ModelMergerPlugin {
	registry := authpkg.NewRegistry()
	loopback := executor.NewLoopback(cfg.LoopbackURL, cfg.AccessKey)
	return &ModelMergerPlugin{
		auth:     authpkg.NewProvider(registry),
		models:   models.NewProvider(),
		executor: executor.NewExecutor(loopback, registry, log),
		mgmt:     management.New(loopback),
	}
}

func Build(configYAML []byte, log executor.LogFunc) pluginapi.Plugin {
	p := New(ParseConfig(configYAML), log)
	return pluginapi.Plugin{
		Metadata: pluginapi.Metadata{
			Name:             DisplayName,
			Version:          "0.1.0",
			Author:           "NoPro200",
			GitHubRepository: "https://github.com/NoPro200/CLI-Proxy-API-Plugins",
			ConfigFields: []pluginapi.ConfigField{
				{Name: "access-key", Type: pluginapi.ConfigFieldTypeString, Description: "One of the host's api-keys. The plugin calls the targets through http://127.0.0.1:<port>/v1/images/generations with it."},
				{Name: "port", Type: pluginapi.ConfigFieldTypeInteger, Description: "Port CLIProxyAPI listens on (default 8317)."},
				{Name: "loopback-url", Type: pluginapi.ConfigFieldTypeString, Description: "Optional base URL instead of http://127.0.0.1:<port>, e.g. https://127.0.0.1:8317 with TLS."},
			},
		},
		Capabilities: pluginapi.Capabilities{
			AuthProvider:          p,
			ModelProvider:         p,
			Executor:              p,
			ExecutorModelScope:    pluginapi.ExecutorModelScopeOAuth, // each auth file is one virtual model
			ExecutorInputFormats:  []string{authpkg.ImageModelType},
			ExecutorOutputFormats: []string{authpkg.ImageModelType},
			ManagementAPI:         p,
		},
	}
}

func (p *ModelMergerPlugin) Identifier() string { return authpkg.ProviderKey }

func (p *ModelMergerPlugin) ParseAuth(ctx context.Context, req pluginapi.AuthParseRequest) (pluginapi.AuthParseResponse, error) {
	return p.auth.ParseAuth(ctx, req)
}

func (p *ModelMergerPlugin) StartLogin(ctx context.Context, req pluginapi.AuthLoginStartRequest) (pluginapi.AuthLoginStartResponse, error) {
	return p.auth.StartLogin(ctx, req)
}

func (p *ModelMergerPlugin) PollLogin(ctx context.Context, req pluginapi.AuthLoginPollRequest) (pluginapi.AuthLoginPollResponse, error) {
	return p.auth.PollLogin(ctx, req)
}

func (p *ModelMergerPlugin) RefreshAuth(ctx context.Context, req pluginapi.AuthRefreshRequest) (pluginapi.AuthRefreshResponse, error) {
	return p.auth.RefreshAuth(ctx, req)
}

func (p *ModelMergerPlugin) StaticModels(ctx context.Context, req pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
	return p.models.StaticModels(ctx, req)
}

func (p *ModelMergerPlugin) ModelsForAuth(ctx context.Context, req pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	return p.models.ModelsForAuth(ctx, req)
}

func (p *ModelMergerPlugin) Execute(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return p.executor.Execute(ctx, req)
}

func (p *ModelMergerPlugin) ExecuteStream(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
	return p.executor.ExecuteStream(ctx, req)
}

func (p *ModelMergerPlugin) CountTokens(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return p.executor.CountTokens(ctx, req)
}

func (p *ModelMergerPlugin) HttpRequest(ctx context.Context, req pluginapi.ExecutorHTTPRequest) (pluginapi.ExecutorHTTPResponse, error) {
	return p.executor.HttpRequest(ctx, req)
}

func (p *ModelMergerPlugin) RegisterManagement(ctx context.Context, req pluginapi.ManagementRegistrationRequest) (pluginapi.ManagementRegistrationResponse, error) {
	return p.mgmt.RegisterManagement(ctx, req)
}

func (p *ModelMergerPlugin) HandleManagement(ctx context.Context, req pluginapi.ManagementRequest) (pluginapi.ManagementResponse, error) {
	return p.mgmt.HandleManagement(ctx, req)
}

func (p *ModelMergerPlugin) HandleManagementWithHost(ctx context.Context, req pluginapi.ManagementRequest, host management.HostClient) (pluginapi.ManagementResponse, error) {
	return p.mgmt.HandleManagementWithHost(ctx, req, host)
}

var _ pluginapi.AuthProvider = (*ModelMergerPlugin)(nil)
var _ pluginapi.ModelProvider = (*ModelMergerPlugin)(nil)
var _ pluginapi.ProviderExecutor = (*ModelMergerPlugin)(nil)
var _ pluginapi.ManagementAPI = (*ModelMergerPlugin)(nil)
var _ pluginapi.ManagementHandler = (*ModelMergerPlugin)(nil)
