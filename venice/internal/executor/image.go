package executor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"github.com/trungking/cpa-plugin-venice/internal/models"
)

const imageURL = "https://outerface.venice.ai/api/inference/image"

type openAIImageRequest struct {
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	Quality        string `json:"quality"`
	Moderation     string `json:"moderation"`
	NegativePrompt string `json:"negative_prompt"`
}

// executeImage answers CLIProxyAPI's /v1/images/generations for Venice image
// models. The web app's image endpoint returns the raw image, one per call.
func (e *Executor) executeImage(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	var imageReq openAIImageRequest
	if errDecode := json.Unmarshal(req.Payload, &imageReq); errDecode != nil {
		return pluginapi.ExecutorResponse{}, fmt.Errorf("decode image request: %w", errDecode)
	}
	if strings.TrimSpace(imageReq.Prompt) == "" {
		return pluginapi.ExecutorResponse{}, fmt.Errorf("prompt is required")
	}
	storage, errStorage := refreshedStorage(ctx, req)
	if errStorage != nil {
		return pluginapi.ExecutorResponse{}, errStorage
	}
	client := requireClient(req.HTTPClient)
	spec, errSpec := models.FetchImageSpec(ctx, client, req.Model)
	if errSpec != nil {
		return pluginapi.ExecutorResponse{}, errSpec
	}
	body := buildImageRequest(req.Model, imageReq, spec, storage.AccountPlan)
	headers := veniceHeaders(*storage, false)
	headers.Set("Accept", "application/json, text/event-stream")
	data := make([]map[string]any, 0, 4)
	for range min(max(imageReq.N, 1), 4) {
		body["seed"] = rand.IntN(1_000_000_000)
		body["requestId"] = randomID()
		raw, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			return pluginapi.ExecutorResponse{}, errMarshal
		}
		resp, errDo := client.Do(ctx, pluginapi.HTTPRequest{Method: http.MethodPost, URL: imageURL, Headers: headers, Body: raw})
		if errDo != nil {
			return pluginapi.ExecutorResponse{}, errDo
		}
		contentType := resp.Headers.Get("Content-Type")
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(contentType, "image/") {
			return pluginapi.ExecutorResponse{}, fmt.Errorf("venice image failed: status %d: %.300s", resp.StatusCode, resp.Body)
		}
		data = append(data, map[string]any{"b64_json": base64.StdEncoding.EncodeToString(resp.Body), "mime_type": contentType})
	}
	payload, errMarshal := json.Marshal(map[string]any{"created": time.Now().Unix(), "data": data})
	if errMarshal != nil {
		return pluginapi.ExecutorResponse{}, errMarshal
	}
	return pluginapi.ExecutorResponse{
		Payload: payload,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// buildImageRequest maps an OpenAI image request onto the web app's request.
// Paid plans may drop the watermark, and OpenAI's moderation "low" is the
// closest match for turning Venice's mature filter off.
func buildImageRequest(model string, req openAIImageRequest, spec models.ImageSpec, plan string) map[string]any {
	width, height := imageSize(req.Size, spec.Divisor)
	steps := spec.Steps
	if steps <= 0 {
		steps = 20
	}
	body := map[string]any{
		"clientProcessingTime": 1,
		"embedExifMetadata":    false,
		"format":               "png",
		"height":               height,
		"hideWatermark":        plan != "" && !strings.EqualFold(plan, "free"),
		"matureFilter":         !strings.EqualFold(req.Moderation, "low"),
		"modelId":              model,
		"prompt":               req.Prompt,
		"steps":                steps,
		"width":                width,
	}
	if ratio := nearestAspectRatio(width, height, spec.AspectRatios); ratio != "" {
		body["aspectRatio"] = ratio
	}
	if spec.DefaultResolution != "" {
		body["resolution"] = spec.DefaultResolution
	}
	if slices.Contains(spec.Qualities, req.Quality) {
		body["quality"] = req.Quality
	}
	if req.NegativePrompt != "" {
		body["negativePrompt"] = req.NegativePrompt
	}
	return body
}

// imageSize reads OpenAI's "WIDTHxHEIGHT" size, defaults to 1024x1024 (also for
// "auto"), and rounds down to the multiple the model requires.
func imageSize(size string, divisor int) (int, int) {
	width, height := 1024, 1024
	if w, h, ok := strings.Cut(strings.ToLower(strings.TrimSpace(size)), "x"); ok {
		parsedW, errW := strconv.Atoi(w)
		parsedH, errH := strconv.Atoi(h)
		if errW == nil && errH == nil && parsedW > 0 && parsedH > 0 {
			width, height = parsedW, parsedH
		}
	}
	if divisor > 1 {
		width -= width % divisor
		height -= height % divisor
	}
	return width, height
}

func nearestAspectRatio(width, height int, ratios []string) string {
	target := float64(width) / float64(height)
	best, bestDiff := "", math.MaxFloat64
	for _, ratio := range ratios {
		a, b, ok := strings.Cut(ratio, ":")
		if !ok {
			continue
		}
		parsedA, errA := strconv.ParseFloat(a, 64)
		parsedB, errB := strconv.ParseFloat(b, 64)
		if errA != nil || errB != nil || parsedB == 0 {
			continue
		}
		if diff := math.Abs(parsedA/parsedB - target); diff < bestDiff {
			best, bestDiff = ratio, diff
		}
	}
	return best
}
