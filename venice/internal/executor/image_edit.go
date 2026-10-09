package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/trungking/cpa-plugin-venice/internal/models"
)

// imageEditURL is the web app's edit endpoint for Venice's "-edit" models.
const imageEditURL = "https://outerface.venice.ai/api/inference/multi-edit"

type imageEditRequest struct {
	Prompt  string
	Size    string
	Quality string
	N       int
	Images  []imageUpload
	Mask    *imageUpload
}

type imageUpload struct {
	Name        string
	ContentType string
	Data        []byte
}

// isImageEditRequest reports a /v1/images/edits call. CLIProxyAPI forwards
// edits as they arrive: multipart uploads, or JSON on the edits path.
func isImageEditRequest(req pluginapi.ExecutorRequest) bool {
	if strings.HasSuffix(metadataString(req.Metadata, "request_path"), "/images/edits") {
		return true
	}
	mediaType, _, _ := mime.ParseMediaType(req.Headers.Get("Content-Type"))
	return strings.HasPrefix(mediaType, "multipart/")
}

// executeImageEdit answers /v1/images/edits for Venice edit models. Like
// generation, the web app returns one raw image per call.
func (e *Executor) executeImageEdit(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	editReq, errParse := parseImageEditRequest(req)
	if errParse != nil {
		return pluginapi.ExecutorResponse{}, errParse
	}
	storage, errStorage := refreshedStorage(ctx, req)
	if errStorage != nil {
		return pluginapi.ExecutorResponse{}, errStorage
	}
	client := requireClient(req.HTTPClient)
	spec, errSpec := fetchSpec(ctx, client, models.EditKind, req.Model)
	if errSpec != nil {
		return pluginapi.ExecutorResponse{}, errSpec
	}
	if errValidate := validateImageEdit(req.Model, editReq, spec); errValidate != nil {
		return pluginapi.ExecutorResponse{}, errValidate
	}
	headers := veniceHeaders(*storage, false)
	headers.Set("Accept", "application/json, text/event-stream")
	data := make([]map[string]any, 0, 4)
	for range min(max(editReq.N, 1), 4) {
		body, contentType, errBody := buildImageEditBody(req.Model, editReq, spec)
		if errBody != nil {
			return pluginapi.ExecutorResponse{}, errBody
		}
		headers.Set("Content-Type", contentType)
		resp, errDo := client.Do(ctx, pluginapi.HTTPRequest{Method: http.MethodPost, URL: imageEditURL, Headers: headers, Body: body})
		if errDo != nil {
			return pluginapi.ExecutorResponse{}, errDo
		}
		image, errImage := imageFromResponse("venice image edit failed", resp)
		if errImage != nil {
			return pluginapi.ExecutorResponse{}, errImage
		}
		data = append(data, image)
	}
	return imagesResponse(data)
}

func validateImageEdit(model string, req imageEditRequest, spec models.ImageSpec) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return badRequest("prompt is required")
	}
	if len(req.Images) == 0 {
		return badRequest("image is required")
	}
	if spec.PromptLimit > 0 && utf8.RuneCountInString(req.Prompt) > spec.PromptLimit {
		return badRequest("prompt exceeds %d characters for %s", spec.PromptLimit, model)
	}
	limit := spec.MaxInputImages
	if limit == 0 && !spec.CombineImages {
		limit = 1
	}
	if limit > 0 && len(req.Images) > limit {
		return badRequest("%s accepts at most %d input images, got %d", model, limit, len(req.Images))
	}
	return nil
}

// buildImageEditBody writes the multipart form the web app sends: every
// option as its own text field, the inputs as "files" and the mask as "mask".
func buildImageEditBody(model string, req imageEditRequest, spec models.ImageSpec) ([]byte, string, error) {
	fields := [][2]string{{"modelId", model}, {"prompt", req.Prompt}, {"requestId", randomID()}}
	if width, height, ok := explicitImageSize(req.Size); ok {
		if ratio := nearestAspectRatio(width, height, spec.AspectRatios); ratio != "" {
			fields = append(fields, [2]string{"aspectRatio", ratio})
		}
	}
	if spec.DefaultResolution != "" {
		fields = append(fields, [2]string{"resolution", spec.DefaultResolution})
	}
	if slices.Contains(spec.Qualities, req.Quality) {
		fields = append(fields, [2]string{"quality", req.Quality})
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range fields {
		if errWrite := writer.WriteField(field[0], field[1]); errWrite != nil {
			return nil, "", errWrite
		}
	}
	for _, image := range req.Images {
		if errWrite := writeUpload(writer, "files", image); errWrite != nil {
			return nil, "", errWrite
		}
	}
	if req.Mask != nil {
		if errWrite := writeUpload(writer, "mask", *req.Mask); errWrite != nil {
			return nil, "", errWrite
		}
	}
	if errClose := writer.Close(); errClose != nil {
		return nil, "", errClose
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func writeUpload(writer *multipart.Writer, field string, upload imageUpload) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", multipart.FileContentDisposition(field, upload.Name))
	header.Set("Content-Type", upload.ContentType)
	part, errCreate := writer.CreatePart(header)
	if errCreate != nil {
		return errCreate
	}
	_, errWrite := part.Write(upload.Data)
	return errWrite
}

func parseImageEditRequest(req pluginapi.ExecutorRequest) (imageEditRequest, error) {
	mediaType, params, _ := mime.ParseMediaType(req.Headers.Get("Content-Type"))
	if strings.HasPrefix(mediaType, "multipart/") {
		return parseMultipartImageEdit(req.Payload, params["boundary"])
	}
	return parseJSONImageEdit(req.Payload)
}

func parseMultipartImageEdit(payload []byte, boundary string) (imageEditRequest, error) {
	if boundary == "" {
		return imageEditRequest{}, badRequest("multipart boundary is missing")
	}
	// The payload is already in memory, so keep the parsed files there too.
	form, errRead := multipart.NewReader(bytes.NewReader(payload), boundary).ReadForm(int64(len(payload)) + 1<<20)
	if errRead != nil {
		return imageEditRequest{}, badRequest("read multipart form: %v", errRead)
	}
	defer func() { _ = form.RemoveAll() }()
	value := func(key string) string {
		if values := form.Value[key]; len(values) > 0 {
			return strings.TrimSpace(values[0])
		}
		return ""
	}
	n, _ := strconv.Atoi(value("n"))
	out := imageEditRequest{Prompt: value("prompt"), Size: value("size"), Quality: value("quality"), N: n}
	for _, key := range []string{"image[]", "image"} {
		for _, fileHeader := range form.File[key] {
			upload, errUpload := readUpload(fileHeader)
			if errUpload != nil {
				return imageEditRequest{}, errUpload
			}
			out.Images = append(out.Images, upload)
		}
	}
	if masks := form.File["mask"]; len(masks) > 0 {
		mask, errMask := readUpload(masks[0])
		if errMask != nil {
			return imageEditRequest{}, errMask
		}
		out.Mask = &mask
	}
	return out, nil
}

func readUpload(fileHeader *multipart.FileHeader) (imageUpload, error) {
	file, errOpen := fileHeader.Open()
	if errOpen != nil {
		return imageUpload{}, badRequest("open upload %s: %v", fileHeader.Filename, errOpen)
	}
	defer func() { _ = file.Close() }()
	data, errRead := io.ReadAll(file)
	if errRead != nil {
		return imageUpload{}, badRequest("read upload %s: %v", fileHeader.Filename, errRead)
	}
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	return imageUpload{Name: firstNonEmpty(fileHeader.Filename, "image"), ContentType: contentType, Data: data}, nil
}

func parseJSONImageEdit(payload []byte) (imageEditRequest, error) {
	var raw struct {
		Prompt  string            `json:"prompt"`
		Size    string            `json:"size"`
		Quality string            `json:"quality"`
		N       int               `json:"n"`
		Image   json.RawMessage   `json:"image"`
		Images  []json.RawMessage `json:"images"`
		Mask    json.RawMessage   `json:"mask"`
	}
	if errDecode := json.Unmarshal(payload, &raw); errDecode != nil {
		return imageEditRequest{}, badRequest("decode image edit request: %v", errDecode)
	}
	out := imageEditRequest{Prompt: strings.TrimSpace(raw.Prompt), Size: raw.Size, Quality: raw.Quality, N: raw.N}
	refs := raw.Images
	if len(raw.Image) > 0 && string(raw.Image) != "null" {
		refs = append([]json.RawMessage{raw.Image}, refs...)
	}
	for i, ref := range refs {
		upload, errDecode := decodeDataURL(imageRefURL(ref), fmt.Sprintf("image-%d", i+1))
		if errDecode != nil {
			return imageEditRequest{}, errDecode
		}
		out.Images = append(out.Images, upload)
	}
	if len(raw.Mask) > 0 && string(raw.Mask) != "null" {
		mask, errDecode := decodeDataURL(imageRefURL(raw.Mask), "mask")
		if errDecode != nil {
			return imageEditRequest{}, errDecode
		}
		out.Mask = &mask
	}
	return out, nil
}

// imageRefURL reads the image reference shapes OpenAI clients send: a plain
// string, {"url"}, {"image_url": "..."} or {"image_url": {"url"}}.
func imageRefURL(ref json.RawMessage) string {
	var value string
	if json.Unmarshal(ref, &value) == nil {
		return value
	}
	var object struct {
		URL      string          `json:"url"`
		ImageURL json.RawMessage `json:"image_url"`
	}
	if json.Unmarshal(ref, &object) != nil {
		return ""
	}
	if object.URL != "" {
		return object.URL
	}
	if json.Unmarshal(object.ImageURL, &value) == nil {
		return value
	}
	var nested struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(object.ImageURL, &nested)
	return nested.URL
}

func decodeDataURL(raw, name string) (imageUpload, error) {
	meta, encoded, ok := strings.Cut(strings.TrimSpace(raw), ",")
	contentType, isBase64 := strings.CutSuffix(strings.TrimPrefix(meta, "data:"), ";base64")
	if !ok || !strings.HasPrefix(meta, "data:") || !isBase64 {
		// shortcut: remote image URLs are not downloaded (the proxy would fetch any URL a client names); add fetching through the host client if a client needs it.
		return imageUpload{}, badRequest("%s must be a base64 data: URL; upload remote images as multipart instead", name)
	}
	data, errDecode := base64.StdEncoding.DecodeString(encoded)
	if errDecode != nil {
		return imageUpload{}, badRequest("decode %s: %v", name, errDecode)
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return imageUpload{Name: name + "." + strings.TrimPrefix(contentType, "image/"), ContentType: contentType, Data: data}, nil
}
