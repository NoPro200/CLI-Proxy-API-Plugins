package plugin

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
)

func TestBuildReportsIdentity(t *testing.T) {
	plugin := Build(nil, nil)
	if plugin.Metadata.Name != "Model Merger (NoPro200)" || plugin.Metadata.Author != "NoPro200" {
		t.Fatalf("metadata = %+v", plugin.Metadata)
	}
	if plugin.Metadata.GitHubRepository != "https://github.com/NoPro200/CLI-Proxy-API-Plugins" {
		t.Fatalf("repository = %q", plugin.Metadata.GitHubRepository)
	}
	if plugin.Capabilities.ExecutorModelScope != pluginapi.ExecutorModelScopeOAuth {
		t.Fatalf("scope = %q", plugin.Capabilities.ExecutorModelScope)
	}
	if plugin.Capabilities.ManagementAPI == nil || plugin.Capabilities.Executor.Identifier() != authpkg.ProviderKey {
		t.Fatal("management or executor capability is wrong")
	}
}

func TestParseConfig(t *testing.T) {
	cfg := ParseConfig([]byte("enabled: true\npriority: 0\nport: 9000\naccess-key: \" sk-test \"\n"))
	if cfg.LoopbackURL != "http://127.0.0.1:9000" || cfg.AccessKey != "sk-test" {
		t.Fatalf("cfg = %+v", cfg)
	}
	cfg = ParseConfig([]byte("loopback-url: https://127.0.0.1:8443/\n"))
	if cfg.LoopbackURL != "https://127.0.0.1:8443" || cfg.AccessKey != "" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg = ParseConfig(nil); cfg.LoopbackURL != "http://127.0.0.1:8317" {
		t.Fatalf("default cfg = %+v", cfg)
	}
}
