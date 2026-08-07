package plugin

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	authpkg "github.com/router-for-me/CLIProxyAPIPlugins/geminicli/internal/auth"
	"github.com/router-for-me/CLIProxyAPIPlugins/geminicli/internal/executor"
	"github.com/router-for-me/CLIProxyAPIPlugins/geminicli/internal/models"
	thinkingpkg "github.com/router-for-me/CLIProxyAPIPlugins/geminicli/internal/thinking"
	"github.com/router-for-me/CLIProxyAPIPlugins/geminicli/internal/translator"
)

const Provider = "gemini-cli"
const executorFormat = "gemini"

type GeminiCLIPlugin struct {
	auth       *authpkg.Provider
	models     *models.Provider
	thinking   *thinkingpkg.Applier
	translator *translator.Translator
	executor   *executor.Executor
}

func New() *GeminiCLIPlugin { return NewWithSettings(authpkg.Settings{}) }

// NewWithSettings builds the plugin around the operator's plugin-owned
// configuration, which seeds logins that arrive without a project selection.
func NewWithSettings(settings authpkg.Settings) *GeminiCLIPlugin {
	return &GeminiCLIPlugin{
		auth:       authpkg.NewProviderWithSettings(settings),
		models:     models.NewProvider(),
		thinking:   thinkingpkg.NewApplier(),
		translator: translator.NewTranslator(),
		executor:   executor.NewExecutor(),
	}
}

func Build(configYAML []byte) pluginapi.Plugin {
	p := NewWithSettings(authpkg.ParseSettings(configYAML))
	return pluginapi.Plugin{
		Metadata: pluginapi.Metadata{
			Name:             "Gemini CLI Provider (NoPro200)",
			Version:          "0.1.0",
			Author:           "NoPro200",
			GitHubRepository: "https://github.com/NoPro200/CLI-Proxy-API-Plugins",
			ConfigFields:     authpkg.ConfigFields(),
		},
		Capabilities: pluginapi.Capabilities{
			AuthProvider:          p,
			ModelProvider:         p,
			Executor:              p,
			ExecutorModelScope:    pluginapi.ExecutorModelScopeOAuth,
			ExecutorInputFormats:  []string{executorFormat},
			ExecutorOutputFormats: []string{executorFormat},
			RequestTranslator:     p,
			ResponseTranslator:    p,
			ThinkingApplier:       p,
			CommandLinePlugin:     p,
		},
	}
}

func (p *GeminiCLIPlugin) Identifier() string { return Provider }

func (p *GeminiCLIPlugin) ParseAuth(ctx context.Context, req pluginapi.AuthParseRequest) (pluginapi.AuthParseResponse, error) {
	return p.auth.ParseAuth(ctx, req)
}

func (p *GeminiCLIPlugin) StartLogin(ctx context.Context, req pluginapi.AuthLoginStartRequest) (pluginapi.AuthLoginStartResponse, error) {
	return p.auth.StartLogin(ctx, req)
}

func (p *GeminiCLIPlugin) PollLogin(ctx context.Context, req pluginapi.AuthLoginPollRequest) (pluginapi.AuthLoginPollResponse, error) {
	return p.auth.PollLogin(ctx, req)
}

func (p *GeminiCLIPlugin) RefreshAuth(ctx context.Context, req pluginapi.AuthRefreshRequest) (pluginapi.AuthRefreshResponse, error) {
	return p.auth.RefreshAuth(ctx, req)
}

func (p *GeminiCLIPlugin) StaticModels(ctx context.Context, req pluginapi.StaticModelRequest) (pluginapi.ModelResponse, error) {
	return p.models.StaticModels(ctx, req)
}

func (p *GeminiCLIPlugin) ModelsForAuth(ctx context.Context, req pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	return p.models.ModelsForAuth(ctx, req)
}

func (p *GeminiCLIPlugin) TranslateRequest(ctx context.Context, req pluginapi.RequestTransformRequest) (pluginapi.PayloadResponse, error) {
	return p.translator.TranslateRequest(ctx, req)
}

func (p *GeminiCLIPlugin) TranslateResponse(ctx context.Context, req pluginapi.ResponseTransformRequest) (pluginapi.PayloadResponse, error) {
	return p.translator.TranslateResponse(ctx, req)
}

func (p *GeminiCLIPlugin) ApplyThinking(ctx context.Context, req pluginapi.ThinkingApplyRequest) (pluginapi.PayloadResponse, error) {
	return p.thinking.ApplyThinking(ctx, req)
}

func (p *GeminiCLIPlugin) Execute(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return p.executor.Execute(ctx, req)
}

func (p *GeminiCLIPlugin) ExecuteStream(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorStreamResponse, error) {
	return p.executor.ExecuteStream(ctx, req)
}

func (p *GeminiCLIPlugin) CountTokens(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	return p.executor.CountTokens(ctx, req)
}

func (p *GeminiCLIPlugin) HttpRequest(ctx context.Context, req pluginapi.ExecutorHTTPRequest) (pluginapi.ExecutorHTTPResponse, error) {
	return p.executor.HttpRequest(ctx, req)
}

func (p *GeminiCLIPlugin) RegisterCommandLine(ctx context.Context, req pluginapi.CommandLineRegistrationRequest) (pluginapi.CommandLineRegistrationResponse, error) {
	return p.auth.RegisterCommandLine(ctx, req)
}

func (p *GeminiCLIPlugin) ExecuteCommandLine(ctx context.Context, req pluginapi.CommandLineExecutionRequest) (pluginapi.CommandLineExecutionResponse, error) {
	return p.auth.ExecuteCommandLine(ctx, req)
}

var _ pluginapi.AuthProvider = (*GeminiCLIPlugin)(nil)
var _ pluginapi.ModelProvider = (*GeminiCLIPlugin)(nil)
var _ pluginapi.ProviderExecutor = (*GeminiCLIPlugin)(nil)
var _ pluginapi.RequestTranslator = (*GeminiCLIPlugin)(nil)
var _ pluginapi.ResponseTranslator = (*GeminiCLIPlugin)(nil)
var _ pluginapi.ThinkingApplier = (*GeminiCLIPlugin)(nil)
var _ pluginapi.CommandLinePlugin = (*GeminiCLIPlugin)(nil)
