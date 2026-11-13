package models

import (
	"context"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestModelsForAuthListsOnlyModelsTheWebAppOffers(t *testing.T) {
	t.Cleanup(func() { webIDs.Store(nil) })
	storage := []byte(`{"type":"venice","cookie":"__client=x","authorization":"Bearer x","authorization_expires_at":"2099-01-01T00:00:00Z"}`)
	resp, err := NewProvider().ModelsForAuth(context.Background(), pluginapi.AuthModelRequest{StorageJSON: storage, HTTPClient: webCatalogClient{}})
	if err != nil {
		t.Fatalf("ModelsForAuth error: %v", err)
	}
	for _, id := range []string{"zai-org-glm-5-2", "zai-org-glm-5-1", "gemini-3-5-flash", "grok-imagine-image"} {
		if !hasModel(resp.Models, id) {
			t.Fatalf("offered model %s missing: %#v", id, resp.Models)
		}
	}
	for _, id := range []string{"deepseek-v4-1-flash-1017", "retired-in-web", "qwen-image"} {
		if hasModel(resp.Models, id) {
			t.Fatalf("model %s is listed although the web app does not offer it", id)
		}
	}
	for _, tc := range []struct{ kind, apiID, webID string }{
		{TextKind, "zai-org-glm-5-2", "zai-org-glm-5.2"},
		{TextKind, "zai-org-glm-5-1", "zai-org-glm-5.1"},
		{TextKind, "gemini-3-5-flash", "gemini-3-5-flash"},
		{ImageKind, "grok-imagine-image", "grok-imagine"},
	} {
		if got, ok := WebID(tc.kind, tc.apiID); !ok || got != tc.webID {
			t.Fatalf("WebID(%s, %s) = %q, %v; want %q", tc.kind, tc.apiID, got, ok, tc.webID)
		}
	}

	// A failed web fetch keeps the last list instead of showing every API model again.
	resp, err = NewProvider().ModelsForAuth(context.Background(), pluginapi.AuthModelRequest{StorageJSON: storage, HTTPClient: webCatalogClient{webStatus: 500}})
	if err != nil || hasModel(resp.Models, "deepseek-v4-1-flash-1017") || !hasModel(resp.Models, "zai-org-glm-5-2") {
		t.Fatalf("after failed web fetch: err %v, models %#v", err, resp.Models)
	}
}

type webCatalogClient struct{ webStatus int }

func (c webCatalogClient) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	switch {
	case strings.HasPrefix(req.URL, "https://outerface.venice.ai/api/app/models"):
		if c.webStatus != 0 {
			return pluginapi.HTTPResponse{StatusCode: c.webStatus}, nil
		}
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{
			"text":{"models":[
				{"id":"zai-org-glm-5.2","apiModelId":"zai-org-glm-5-2","active":true},
				{"id":"zai-org-glm-5.1","aliasIds":["zai-org-glm-5-1"],"active":true},
				{"id":"gemini-3-5-flash","active":true},
				{"id":"retired-in-web","active":false}]},
			"image":{"models":[{"id":"grok-imagine","apiModelId":"grok-imagine-image","active":true}]},
			"inpaint":{"models":[{"id":"grok-imagine-edit","apiModelId":"grok-imagine-edit","active":true}]},
			"video":{"models":[]}}`)}, nil
	case strings.Contains(req.URL, "type=inpaint"):
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"grok-imagine-edit","type":"inpaint","model_spec":{"name":"Grok Imagine"}}]}`)}, nil
	case strings.Contains(req.URL, "type=image"):
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"grok-imagine-image","model_spec":{"name":"Grok Imagine"}},{"id":"qwen-image","model_spec":{"name":"Qwen Image"}}]}`)}, nil
	default:
		return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"zai-org-glm-5-2"},{"id":"zai-org-glm-5-1"},{"id":"gemini-3-5-flash"},{"id":"retired-in-web"},{"id":"deepseek-v4-1-flash-1017"}]}`)}, nil
	}
}

func (webCatalogClient) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, nil
}
