package auth

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestParseVirtualModelAppliesDefaults(t *testing.T) {
	model, err := ParseVirtualModel([]byte(`{"type":"model-merger","alias":"mixed-image-gen","targets":[{"model":"gpt-image-2.5-sunburst"},{"model":" qwen-image-3-pro ","timeout":90}]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !model.Enabled {
		t.Fatal("a file without enabled must be enabled")
	}
	if model.TypeOfModel != ImageModelType {
		t.Fatalf("type_of_model = %q", model.TypeOfModel)
	}
	if !slices.Equal(model.FallbackOnStatus, DefaultFallbackStatus) {
		t.Fatalf("fallback_on_status = %v", model.FallbackOnStatus)
	}
	if !slices.Equal(model.ModerationPatterns, DefaultModerationPatterns) {
		t.Fatalf("moderation_patterns = %v", model.ModerationPatterns)
	}
	if model.FallbackOnModeration {
		t.Fatal("fallback_on_moderation must default to false")
	}
	if model.Targets[1].Model != "qwen-image-3-pro" || model.Targets[1].Timeout != 90 {
		t.Fatalf("targets = %+v", model.Targets)
	}
}

func TestParseVirtualModelKeepsExplicitEmptyLists(t *testing.T) {
	model, err := ParseVirtualModel([]byte(`{"type":"model-merger","alias":"a","enabled":false,"targets":[{"model":"x"}],"fallback_on_status":[],"moderation_patterns":[]}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if model.Enabled {
		t.Fatal("enabled:false was ignored")
	}
	if len(model.FallbackOnStatus) != 0 || len(model.ModerationPatterns) != 0 {
		t.Fatalf("explicit empty lists were replaced: %v %v", model.FallbackOnStatus, model.ModerationPatterns)
	}
}

func TestParseVirtualModelIgnoresOtherProviders(t *testing.T) {
	if _, err := ParseVirtualModel([]byte(`{"type":"venice","cookie":"x"}`)); !errors.Is(err, ErrNotModelMerger) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateRejectsBrokenModels(t *testing.T) {
	tests := []struct {
		name   string
		model  VirtualModel
		others []string
		want   string
	}{
		{"no targets", VirtualModel{Alias: "a", TypeOfModel: ImageModelType}, nil, "at least one target"},
		{"duplicate target", VirtualModel{Alias: "a", TypeOfModel: ImageModelType, Targets: []Target{{Model: "x"}, {Model: "X"}}}, nil, "listed twice"},
		{"self target", VirtualModel{Alias: "a", TypeOfModel: ImageModelType, Targets: []Target{{Model: "a"}}}, nil, "virtual model"},
		{"other virtual target", VirtualModel{Alias: "a", TypeOfModel: ImageModelType, Targets: []Target{{Model: "x"}, {Model: "b"}}}, []string{"b"}, "virtual model"},
		{"chat not yet", VirtualModel{Alias: "a", TypeOfModel: "chat", Targets: []Target{{Model: "x"}}}, nil, "not supported yet"},
		{"bad status", VirtualModel{Alias: "a", TypeOfModel: ImageModelType, Targets: []Target{{Model: "x"}}, FallbackOnStatus: []int{42}}, nil, "not an HTTP status"},
		{"no alias", VirtualModel{TypeOfModel: ImageModelType, Targets: []Target{{Model: "x"}}}, nil, "alias is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.model.Validate(tt.others)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
	valid := VirtualModel{Alias: "mixed-image-gen", TypeOfModel: ImageModelType, Targets: []Target{{Model: "gpt-image-2.5-sunburst"}, {Model: "qwen-image-3-pro"}}}
	if err := valid.Validate([]string{"other"}); err != nil {
		t.Fatalf("valid model rejected: %v", err)
	}
}

func TestParseAuthRegistersAlias(t *testing.T) {
	registry := NewRegistry()
	provider := NewProvider(registry)
	resp, err := provider.ParseAuth(context.Background(), pluginapi.AuthParseRequest{
		FileName: "model-merger-mixed-image-gen.json",
		RawJSON:  []byte(`{"type":"model-merger","alias":"mixed-image-gen","enabled":false,"targets":[{"model":"a"},{"model":"b"}]}`),
	})
	if err != nil || !resp.Handled {
		t.Fatalf("parse: %v handled=%v", err, resp.Handled)
	}
	if resp.Auth.Provider != ProviderKey || !resp.Auth.Disabled {
		t.Fatalf("auth = %+v", resp.Auth)
	}
	if resp.Auth.Metadata["disable_cooling"] != true {
		t.Fatalf("the host would cool down the alias itself: metadata = %v", resp.Auth.Metadata)
	}
	if !registry.IsAlias("Mixed-Image-Gen") {
		t.Fatal("alias was not registered")
	}
	other, err := provider.ParseAuth(context.Background(), pluginapi.AuthParseRequest{RawJSON: []byte(`{"type":"venice"}`)})
	if err != nil || other.Handled {
		t.Fatalf("foreign auth: handled=%v err=%v", other.Handled, err)
	}
}

func TestFileName(t *testing.T) {
	if got := FileName("Mixed Image/Gen"); got != "model-merger-mixed-image-gen.json" {
		t.Fatalf("FileName = %q", got)
	}
}
