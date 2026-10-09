package plugin

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestBuildReportsForkIdentity(t *testing.T) {
	plugin := Build(nil)
	if plugin.Metadata.Name != "Venice Provider (NoPro200)" {
		t.Fatalf("metadata name = %q", plugin.Metadata.Name)
	}
	if plugin.Metadata.Author != "NoPro200" {
		t.Fatalf("metadata author = %q", plugin.Metadata.Author)
	}
	if plugin.Metadata.GitHubRepository != "https://github.com/NoPro200/CLI-Proxy-API-Plugins" {
		t.Fatalf("metadata repository = %q", plugin.Metadata.GitHubRepository)
	}
	if plugin.Capabilities.ManagementAPI == nil {
		t.Fatal("management API capability is nil")
	}
	if plugin.Capabilities.QuotaProvider == nil || plugin.Capabilities.QuotaProvider.Identifier() != Provider {
		t.Fatal("quota provider must serve the auth provider key")
	}
	if plugin.Capabilities.ExecutorModelScope != pluginapi.ExecutorModelScopeOAuth {
		t.Fatalf("executor model scope = %q", plugin.Capabilities.ExecutorModelScope)
	}
}
