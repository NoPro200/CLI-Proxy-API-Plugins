package models

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"

	authpkg "github.com/NoPro200/CLI-Proxy-API-Plugins/model-merger/internal/auth"
)

func TestModelsForAuthListsTheAliasAsImageModel(t *testing.T) {
	resp, err := NewProvider().ModelsForAuth(context.Background(), pluginapi.AuthModelRequest{
		StorageJSON: []byte(`{"type":"model-merger","alias":"mixed-image-gen","targets":[{"model":"gpt-image-2.5-sunburst"},{"model":"qwen-image-3-pro"}]}`),
	})
	if err != nil || len(resp.Models) != 1 {
		t.Fatalf("models = %+v err = %v", resp.Models, err)
	}
	model := resp.Models[0]
	if model.ID != "mixed-image-gen" || model.Type != authpkg.ImageModelType || model.SupportedGenerationMethods[0] != "images.generations" {
		t.Fatalf("model = %+v", model)
	}
}

func TestModelsForAuthHidesDisabledAndBrokenModels(t *testing.T) {
	for _, raw := range []string{
		`{"type":"model-merger","alias":"a","enabled":false,"targets":[{"model":"x"}]}`,
		`{"type":"model-merger","alias":"a","targets":[]}`,
		`{"type":"model-merger","alias":"a","targets":[{"model":"a"}]}`,
		`not json`,
	} {
		resp, err := NewProvider().ModelsForAuth(context.Background(), pluginapi.AuthModelRequest{StorageJSON: []byte(raw)})
		if err != nil || len(resp.Models) != 0 {
			t.Fatalf("%s: models = %+v err = %v", raw, resp.Models, err)
		}
	}
}
