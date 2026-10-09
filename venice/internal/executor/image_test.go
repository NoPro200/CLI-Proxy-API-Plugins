package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/trungking/cpa-plugin-venice/internal/models"
)

func TestExecuteImageReturnsOpenAIImages(t *testing.T) {
	sent := map[string]any{}
	resp, err := NewExecutor().Execute(context.Background(), pluginapi.ExecutorRequest{
		Model:        "flux-2-pro",
		SourceFormat: models.ImageModelType,
		Payload:      []byte(`{"model":"flux-2-pro","prompt":"an apple","size":"1792x1024","quality":"high","moderation":"low"}`),
		StorageJSON:  []byte(`{"type":"venice","cookie":"__client=x","authorization":"Bearer token","authorization_expires_at":"2099-01-01T00:00:00Z","account_plan":"PRO"}`),
		HTTPClient:   fakeImageClient{sent: &sent},
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	var out struct {
		Data []struct {
			B64JSON  string `json:"b64_json"`
			MimeType string `json:"mime_type"`
		} `json:"data"`
	}
	if errDecode := json.Unmarshal(resp.Payload, &out); errDecode != nil || len(out.Data) != 1 {
		t.Fatalf("payload = %s, %v", resp.Payload, errDecode)
	}
	if out.Data[0].B64JSON != base64.StdEncoding.EncodeToString([]byte("png-bytes")) || out.Data[0].MimeType != "image/png" {
		t.Fatalf("image = %#v", out.Data[0])
	}
	if sent["width"] != float64(1792) || sent["height"] != float64(1024) || sent["aspectRatio"] != "16:9" || sent["steps"] != float64(20) {
		t.Fatalf("venice request = %#v", sent)
	}
	if sent["quality"] != "high" || sent["resolution"] != "1K" || sent["matureFilter"] != false || sent["hideWatermark"] != true {
		t.Fatalf("venice request = %#v", sent)
	}
}

func TestImageSizeRoundsToModelDivisor(t *testing.T) {
	if w, h := imageSize("1000x600", 16); w != 992 || h != 592 {
		t.Fatalf("imageSize = %dx%d", w, h)
	}
	if w, h := imageSize("auto", 8); w != 1024 || h != 1024 {
		t.Fatalf("imageSize(auto) = %dx%d", w, h)
	}
	if got := nearestAspectRatio(1024, 1536, []string{"auto", "1:1", "2:3", "3:2"}); got != "2:3" {
		t.Fatalf("nearestAspectRatio = %q", got)
	}
}

type fakeImageClient struct {
	sent *map[string]any
}

func (c fakeImageClient) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	switch {
	case strings.Contains(req.URL, "type=image"):
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"flux-2-pro","model_spec":{"name":"Flux 2 Pro","constraints":{"steps":{"default":20},"widthHeightDivisor":16,"aspectRatios":["1:1","16:9","9:16"],"defaultResolution":"1K","qualities":["low","high"]}}}]}`)}, nil
	case req.URL == imageURL:
		_ = json.Unmarshal(req.Body, c.sent)
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": []string{"image/png"}}, Body: []byte("png-bytes")}, nil
	default:
		return pluginapi.HTTPResponse{StatusCode: http.StatusNotFound}, nil
	}
}

func (c fakeImageClient) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, nil
}
