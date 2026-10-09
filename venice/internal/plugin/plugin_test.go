package plugin

import "testing"

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
}
