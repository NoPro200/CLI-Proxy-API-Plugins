package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/trungking/cpa-plugin-venice/internal/models"
)

const editTestStorage = `{"type":"venice","cookie":"__client=x","authorization":"Bearer token","authorization_expires_at":"2099-01-01T00:00:00Z"}`

func TestExecuteImageEditSendsWebAppMultiEditForm(t *testing.T) {
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	_ = writer.WriteField("model", "gpt-image-2-edit")
	_ = writer.WriteField("prompt", "make it blue")
	_ = writer.WriteField("size", "1536x1024")
	_ = writer.WriteField("quality", "high")
	part, _ := writer.CreateFormFile("image", "cat.png")
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\ninput"))
	_ = writer.Close()

	client := &fakeEditClient{}
	resp, err := NewExecutor().Execute(context.Background(), pluginapi.ExecutorRequest{
		Model:        "gpt-image-2-edit",
		SourceFormat: models.ImageModelType,
		Headers:      http.Header{"Content-Type": []string{writer.FormDataContentType()}},
		Payload:      form.Bytes(),
		StorageJSON:  []byte(editTestStorage),
		HTTPClient:   client,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertOneImage(t, resp.Payload)
	if client.fields["modelId"] != "gpt-image-2-edit" || client.fields["prompt"] != "make it blue" || client.fields["requestId"] == "" {
		t.Fatalf("fields = %#v", client.fields)
	}
	if client.fields["aspectRatio"] != "3:2" || client.fields["resolution"] != "1K" || client.fields["quality"] != "high" {
		t.Fatalf("fields = %#v", client.fields)
	}
	if len(client.files) != 1 || client.files[0].field != "files" || client.files[0].contentType != "image/png" || !strings.HasSuffix(client.files[0].data, "input") {
		t.Fatalf("files = %#v", client.files)
	}
	if client.authorization != "Bearer token" {
		t.Fatalf("authorization = %q", client.authorization)
	}
}

func TestExecuteImageEditAcceptsJSONDataURLs(t *testing.T) {
	image := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-input"))
	mask := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("png-mask"))
	client := &fakeEditClient{}
	resp, err := NewExecutor().Execute(context.Background(), pluginapi.ExecutorRequest{
		Model:        "gpt-image-2-edit",
		SourceFormat: models.ImageModelType,
		Headers:      http.Header{"Content-Type": []string{"application/json"}},
		Metadata:     map[string]any{"request_path": "/v1/images/edits"},
		Payload:      []byte(`{"model":"gpt-image-2-edit","prompt":"add a hat","images":[{"image_url":"` + image + `"}],"mask":{"image_url":{"url":"` + mask + `"}}}`),
		StorageJSON:  []byte(editTestStorage),
		HTTPClient:   client,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	assertOneImage(t, resp.Payload)
	if _, ok := client.fields["aspectRatio"]; ok {
		t.Fatalf("aspectRatio sent without an explicit size: %#v", client.fields)
	}
	if len(client.files) != 2 || client.files[0].data != "png-input" || client.files[1].field != "mask" || client.files[1].data != "png-mask" {
		t.Fatalf("files = %#v", client.files)
	}
}

func TestImageRequestsRejectWrongModelsAndLimitsAsClientErrors(t *testing.T) {
	image := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("x"))
	edit := func(model, images string) error {
		_, err := NewExecutor().Execute(context.Background(), pluginapi.ExecutorRequest{
			Model:        model,
			SourceFormat: models.ImageModelType,
			Metadata:     map[string]any{"request_path": "/v1/images/edits"},
			Payload:      []byte(`{"prompt":"p","images":[` + images + `]}`),
			StorageJSON:  []byte(editTestStorage),
			HTTPClient:   &fakeEditClient{},
		})
		return err
	}
	one := `{"image_url":"` + image + `"}`
	cases := map[string]struct {
		err  error
		want string
	}{
		"generation model on edits": {edit("flux-2-pro", one), "use its -edit model"},
		"too many images":           {edit("gpt-image-2-edit", one+","+one+","+one), "at most 2 input images"},
		"remote image url":          {edit("gpt-image-2-edit", `{"image_url":"https://example.com/a.png"}`), "base64 data: URL"},
	}
	_, errGenerate := NewExecutor().Execute(context.Background(), pluginapi.ExecutorRequest{
		Model:        "gpt-image-2-edit",
		SourceFormat: models.ImageModelType,
		Payload:      []byte(`{"prompt":"p"}`),
		StorageJSON:  []byte(editTestStorage),
		HTTPClient:   &fakeEditClient{},
	})
	cases["edit model on generations"] = struct {
		err  error
		want string
	}{errGenerate, "send it to /v1/images/edits"}
	for name, tc := range cases {
		var status interface{ StatusCode() int }
		if !errors.As(tc.err, &status) || status.StatusCode() != http.StatusBadRequest || !strings.Contains(tc.err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want 400 containing %q", name, tc.err, tc.want)
		}
	}
}

func assertOneImage(t *testing.T, payload []byte) {
	t.Helper()
	var out struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
		} `json:"data"`
	}
	if errDecode := json.Unmarshal(payload, &out); errDecode != nil || len(out.Data) != 1 || out.Data[0].B64JSON != base64.StdEncoding.EncodeToString([]byte("png-out")) {
		t.Fatalf("payload = %s, %v", payload, errDecode)
	}
}

type sentFile struct {
	field, contentType, data string
}

type fakeEditClient struct {
	fields        map[string]string
	files         []sentFile
	authorization string
}

func (c *fakeEditClient) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	switch {
	case strings.Contains(req.URL, "type=inpaint"):
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"gpt-image-2-edit","type":"inpaint","model_spec":{"name":"GPT Image 2","constraints":{"aspectRatios":["auto","1:1","3:2","2:3"],"combineImages":true,"maxInputImages":2,"promptCharacterLimit":100,"defaultResolution":"1K","qualities":["low","medium","high"]}}}]}`)}, nil
	case strings.Contains(req.URL, "type=image"):
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Body: []byte(`{"data":[{"id":"flux-2-pro","type":"image","model_spec":{"name":"Flux 2 Pro","constraints":{"aspectRatios":["1:1"]}}}]}`)}, nil
	case req.URL == imageEditURL:
		c.authorization = req.Headers.Get("Authorization")
		_, params, _ := mime.ParseMediaType(req.Headers.Get("Content-Type"))
		reader := multipart.NewReader(bytes.NewReader(req.Body), params["boundary"])
		c.fields = map[string]string{}
		for {
			part, errPart := reader.NextPart()
			if errPart != nil {
				break
			}
			data, _ := io.ReadAll(part)
			if part.FileName() != "" {
				c.files = append(c.files, sentFile{field: part.FormName(), contentType: part.Header.Get("Content-Type"), data: string(data)})
			} else {
				c.fields[part.FormName()] = string(data)
			}
		}
		return pluginapi.HTTPResponse{StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": []string{"image/png"}}, Body: []byte("png-out")}, nil
	default:
		return pluginapi.HTTPResponse{StatusCode: http.StatusNotFound}, nil
	}
}

func (c *fakeEditClient) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, nil
}
