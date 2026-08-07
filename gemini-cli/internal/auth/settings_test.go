package auth

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestParseSettingsReadsPluginBlock(t *testing.T) {
	// The host hands over the plugin's own block, so enabled and priority sit
	// beside the plugin keys and must not disturb the decode.
	settings := ParseSettings([]byte("enabled: true\npriority: 0\nproject_id: proj-a, proj-b\nmanual_projects: true\n"))
	if !settings.ManualProjects {
		t.Fatalf("manual projects = %v, want true", settings.ManualProjects)
	}
	got := settings.defaults()
	if len(got.IDs) != 2 || got.IDs[0] != "proj-a" || got.IDs[1] != "proj-b" {
		t.Fatalf("project ids = %#v, want [proj-a proj-b]", got.IDs)
	}
	if !got.Manual {
		t.Fatal("manual selection = false, want true")
	}
}

func TestParseSettingsAcceptsProjectIDList(t *testing.T) {
	settings := ParseSettings([]byte("project_id:\n  - proj-a\n  - proj-b\n"))
	got := settings.defaults()
	if len(got.IDs) != 2 || got.IDs[0] != "proj-a" || got.IDs[1] != "proj-b" {
		t.Fatalf("project ids = %#v, want [proj-a proj-b]", got.IDs)
	}
}

func TestParseSettingsToleratesEmptyAndMalformedYAML(t *testing.T) {
	for name, input := range map[string][]byte{
		"nil":       nil,
		"empty":     []byte(""),
		"malformed": []byte("project_id: [unterminated\n"),
	} {
		settings := ParseSettings(input)
		got := settings.defaults()
		if len(got.IDs) != 0 || got.Manual {
			t.Fatalf("%s: selection = %#v, want zero value", name, got)
		}
	}
}

func TestWithDefaultsPrefersExplicitSelection(t *testing.T) {
	defaults := projectSelection{Manual: true, IDs: []string{"config-a"}}
	got := projectSelection{IDs: []string{"explicit-a"}}.withDefaults(defaults)
	if len(got.IDs) != 1 || got.IDs[0] != "explicit-a" {
		t.Fatalf("project ids = %#v, want [explicit-a]", got.IDs)
	}
	// Manual has no negative form, so a configured true still applies.
	if !got.Manual {
		t.Fatal("manual = false, want true from configuration")
	}
}

func TestWithDefaultsFillsUnsetSelection(t *testing.T) {
	defaults := projectSelection{Manual: true, IDs: []string{"config-a", "config-b"}}
	got := projectSelection{}.withDefaults(defaults)
	if len(got.IDs) != 2 || got.IDs[0] != "config-a" {
		t.Fatalf("project ids = %#v, want config defaults", got.IDs)
	}
	if !got.Manual {
		t.Fatal("manual = false, want true")
	}
}

func TestConfigFieldsDeclareTheSettingsKeys(t *testing.T) {
	fields := ConfigFields()
	if len(fields) != 2 {
		t.Fatalf("config fields = %d, want 2", len(fields))
	}
	byName := map[string]pluginapi.ConfigField{}
	for _, field := range fields {
		byName[field.Name] = field
	}
	if got := byName[projectIDKey].Type; got != pluginapi.ConfigFieldTypeString {
		t.Fatalf("%s type = %q, want string", projectIDKey, got)
	}
	if got := byName[manualProjectsKey].Type; got != pluginapi.ConfigFieldTypeBoolean {
		t.Fatalf("%s type = %q, want boolean", manualProjectsKey, got)
	}
	for name, field := range byName {
		if field.Description == "" {
			t.Fatalf("config field %q has no description", name)
		}
	}
}

func TestStartLoginAppliesConfiguredProjectDefaults(t *testing.T) {
	provider := NewProviderWithSettings(Settings{ProjectID: "config-a", ManualProjects: true})
	resp, errStart := provider.StartLogin(context.Background(), pluginapi.AuthLoginStartRequest{
		BaseURL:  "http://127.0.0.1:8317/oauth2callback",
		Metadata: map[string]any{},
	})
	if errStart != nil {
		t.Fatalf("StartLogin() error = %v", errStart)
	}
	if got := resp.Metadata[projectIDKey]; got != "config-a" {
		t.Fatalf("project id metadata = %#v, want config-a", got)
	}
	if got := resp.Metadata[manualProjectsKey]; got != true {
		t.Fatalf("manual projects metadata = %#v, want true", got)
	}
}

func TestStartLoginPrefersRequestMetadataOverConfig(t *testing.T) {
	provider := NewProviderWithSettings(Settings{ProjectID: "config-a"})
	resp, errStart := provider.StartLogin(context.Background(), pluginapi.AuthLoginStartRequest{
		BaseURL:  "http://127.0.0.1:8317/oauth2callback",
		Metadata: map[string]any{projectIDKey: "request-a"},
	})
	if errStart != nil {
		t.Fatalf("StartLogin() error = %v", errStart)
	}
	if got := resp.Metadata[projectIDKey]; got != "request-a" {
		t.Fatalf("project id metadata = %#v, want request-a", got)
	}
}

func TestStartLoginRejectsManualConfigWithoutProjects(t *testing.T) {
	provider := NewProviderWithSettings(Settings{ManualProjects: true})
	_, errStart := provider.StartLogin(context.Background(), pluginapi.AuthLoginStartRequest{
		BaseURL:  "http://127.0.0.1:8317/oauth2callback",
		Metadata: map[string]any{},
	})
	if errStart == nil {
		t.Fatal("StartLogin() error = nil, want manual mode validation failure")
	}
}
