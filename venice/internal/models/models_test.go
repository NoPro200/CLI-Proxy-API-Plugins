package models

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestModelsForAuthFetchesCatalogIDsOnly(t *testing.T) {
	resp, err := NewProvider().ModelsForAuth(context.Background(), pluginapi.AuthModelRequest{
		HTTPClient: fakeCatalogClient{},
	})
	if err != nil {
		t.Fatalf("ModelsForAuth error: %v", err)
	}
	if !hasModel(resp.Models, "gemini-3-5-flash") {
		t.Fatalf("catalog model missing: %#v", resp.Models)
	}
	if hasModel(resp.Models, "gemini-3.5-flash") {
		t.Fatalf("unexpected non-catalog alias present: %#v", resp.Models)
	}
}

func TestModelsForAuthUsesSpecNamesAndSkipsUnusableModels(t *testing.T) {
	resp, err := NewProvider().ModelsForAuth(context.Background(), pluginapi.AuthModelRequest{
		HTTPClient: specCatalogClient{},
	})
	if err != nil {
		t.Fatalf("ModelsForAuth error: %v", err)
	}
	if len(resp.Models) != 1 {
		t.Fatalf("models = %#v", resp.Models)
	}
	model := resp.Models[0]
	if model.ID != "zai-org-glm-5-2" || model.DisplayName != "GLM 5.2" || model.Type != "chat" || model.OutputTokenLimit != 32_768 {
		t.Fatalf("model = %#v", model)
	}
}

func TestModelsForAuthAddsImageModelsAndSpecs(t *testing.T) {
	resp, err := NewProvider().ModelsForAuth(context.Background(), pluginapi.AuthModelRequest{
		HTTPClient: imageCatalogClient{},
	})
	if err != nil {
		t.Fatalf("ModelsForAuth error: %v", err)
	}
	var image *pluginapi.ModelInfo
	for i := range resp.Models {
		if resp.Models[i].ID == "flux-2-pro" {
			image = &resp.Models[i]
		}
	}
	if image == nil || image.Type != ImageModelType || image.DisplayName != "Flux 2 Pro" || hasModel(resp.Models, "retired-image") {
		t.Fatalf("models = %#v", resp.Models)
	}
	var edit *pluginapi.ModelInfo
	for i := range resp.Models {
		if resp.Models[i].ID == "flux-2-max-edit" {
			edit = &resp.Models[i]
		}
	}
	if edit == nil || edit.Type != ImageModelType || edit.DisplayName != "Flux 2 Max Edit" || len(edit.SupportedGenerationMethods) != 1 || edit.SupportedGenerationMethods[0] != "images.edits" {
		t.Fatalf("edit model = %#v", edit)
	}
	spec, err := FetchImageSpec(context.Background(), imageCatalogClient{}, ImageKind, "flux-2-pro")
	if err != nil {
		t.Fatalf("FetchImageSpec error: %v", err)
	}
	if spec.Steps != 20 || spec.Divisor != 16 || spec.DefaultResolution != "1K" || len(spec.AspectRatios) != 2 || len(spec.Qualities) != 2 {
		t.Fatalf("spec = %#v", spec)
	}
	editSpec, err := FetchImageSpec(context.Background(), imageCatalogClient{}, EditKind, "flux-2-max-edit")
	if err != nil || editSpec.MaxInputImages != 6 || editSpec.PromptLimit != 3000 || !editSpec.CombineImages {
		t.Fatalf("edit spec = %#v, %v", editSpec, err)
	}
	if _, err := FetchImageSpec(context.Background(), imageCatalogClient{}, ImageKind, "flux-2-max-edit"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("edit model in image catalog: err = %v, want ErrModelNotFound", err)
	}
}

type imageCatalogClient struct{}

func (imageCatalogClient) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	if strings.Contains(req.URL, "type=inpaint") {
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[
			{"id":"flux-2-max-edit","type":"inpaint","model_spec":{"name":"Flux 2 Max","constraints":{"aspectRatios":["auto","1:1"],"combineImages":true,"maxInputImages":6,"promptCharacterLimit":3000}}}
		]}`)}, nil
	}
	if strings.Contains(req.URL, "type=image") {
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[
			{"id":"flux-2-pro","model_spec":{"name":"Flux 2 Pro","constraints":{"steps":{"default":20,"max":50},"widthHeightDivisor":16,"aspectRatios":["1:1","16:9"],"defaultResolution":"1K","qualities":["low","high"]}}},
			{"id":"retired-image","model_spec":{"name":"Retired","offline":true}}
		]}`)}, nil
	}
	return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"zai-org-glm-5-2","model_spec":{"name":"GLM 5.2"}}]}`)}, nil
}

func (imageCatalogClient) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, nil
}

type specCatalogClient struct{}

func (specCatalogClient) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	if strings.Contains(req.URL, "type=image") {
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[]}`)}, nil
	}
	return pluginapi.HTTPResponse{
		StatusCode: 200,
		Body: []byte(`{"data":[
			{"id":"zai-org-glm-5-2","type":"text","context_length":1000000,"model_spec":{"name":"GLM 5.2","maxCompletionTokens":32768,"capabilities":{"supportsE2EE":false}}},
			{"id":"e2ee-glm-5-2-p","type":"text","model_spec":{"name":"GLM 5.2","capabilities":{"supportsE2EE":true}}},
			{"id":"retired-model","type":"text","model_spec":{"name":"Retired","offline":true}}
		]}`),
	}, nil
}

func (specCatalogClient) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, nil
}

type fakeCatalogClient struct{}

func (fakeCatalogClient) Do(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	return pluginapi.HTTPResponse{
		StatusCode: 200,
		Body:       []byte(`{"data":[{"id":"gemini-3-5-flash","owned_by":"venice.ai","context_length":1000000},{"id":"zai-org-glm-5-2","owned_by":"venice.ai"}]}`),
	}, nil
}

func (fakeCatalogClient) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, nil
}

func hasModel(models []pluginapi.ModelInfo, id string) bool {
	for _, model := range models {
		if model.ID == id {
			return true
		}
	}
	return false
}
