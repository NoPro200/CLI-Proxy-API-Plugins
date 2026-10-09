package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// ImageModelType is the model type CLIProxyAPI routes /v1/images requests by.
const ImageModelType = "openai-image"

// Venice lists image generation and image editing models as separate catalog types.
const (
	ImageKind = "image"
	EditKind  = "inpaint"
)

// ErrModelNotFound reports that a model is missing from the requested catalog.
var ErrModelNotFound = errors.New("venice image model not found")

type imageCatalogModel struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Created   int64  `json:"created"`
	ModelSpec struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Offline     bool   `json:"offline"`
		Constraints struct {
			Steps struct {
				Default int `json:"default"`
			} `json:"steps"`
			WidthHeightDivisor   int      `json:"widthHeightDivisor"`
			AspectRatios         []string `json:"aspectRatios"`
			DefaultResolution    string   `json:"defaultResolution"`
			Qualities            []string `json:"qualities"`
			MaxInputImages       int      `json:"maxInputImages"`
			CombineImages        bool     `json:"combineImages"`
			PromptCharacterLimit int      `json:"promptCharacterLimit"`
		} `json:"constraints"`
	} `json:"model_spec"`
}

// ImageSpec holds the limits Venice publishes for an image or edit model.
type ImageSpec struct {
	Steps             int
	Divisor           int
	AspectRatios      []string
	DefaultResolution string
	Qualities         []string
	// MaxInputImages is the edit input limit; 0 means Venice publishes none.
	MaxInputImages int
	CombineImages  bool
	PromptLimit    int
}

// FetchImageSpec looks the model up in the live catalog of the given kind,
// because the limits differ per model and Venice rejects requests outside them.
func FetchImageSpec(ctx context.Context, client pluginapi.HostHTTPClient, kind, modelID string) (ImageSpec, error) {
	catalog, err := fetchImageCatalog(ctx, client, kind)
	if err != nil {
		return ImageSpec{}, err
	}
	for _, model := range catalog {
		if model.ID != modelID {
			continue
		}
		constraints := model.ModelSpec.Constraints
		return ImageSpec{
			Steps:             constraints.Steps.Default,
			Divisor:           constraints.WidthHeightDivisor,
			AspectRatios:      constraints.AspectRatios,
			DefaultResolution: constraints.DefaultResolution,
			Qualities:         constraints.Qualities,
			MaxInputImages:    constraints.MaxInputImages,
			CombineImages:     constraints.CombineImages,
			PromptLimit:       constraints.PromptCharacterLimit,
		}, nil
	}
	return ImageSpec{}, fmt.Errorf("%w: %q", ErrModelNotFound, modelID)
}

func fetchImageCatalog(ctx context.Context, client pluginapi.HostHTTPClient, kind string) ([]imageCatalogModel, error) {
	resp, err := client.Do(ctx, pluginapi.HTTPRequest{
		Method: http.MethodGet,
		URL:    catalogURL + "?type=" + kind,
		Headers: http.Header{
			"Accept":     []string{"application/json"},
			"User-Agent": []string{"Mozilla/5.0"},
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("venice %s catalog: status %d", kind, resp.StatusCode)
	}
	var payload struct {
		Data []imageCatalogModel `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("decode venice %s catalog: %w", kind, err)
	}
	// Keep only entries of the requested kind in case the filter is ignored upstream.
	out := payload.Data[:0]
	for _, model := range payload.Data {
		if model.Type == "" || model.Type == kind {
			out = append(out, model)
		}
	}
	return out, nil
}

func imageModelInfo(model imageCatalogModel, kind string) pluginapi.ModelInfo {
	name := firstNonEmpty(model.ModelSpec.Name, model.ID)
	info := pluginapi.ModelInfo{
		ID:                         model.ID,
		Object:                     "model",
		Created:                    model.Created,
		OwnedBy:                    "venice",
		Type:                       ImageModelType,
		DisplayName:                name,
		Name:                       model.ID,
		Description:                firstNonEmpty(model.ModelSpec.Description, name+" via Venice web image generation"),
		SupportedGenerationMethods: []string{"images.generations"},
		SupportedInputModalities:   []string{"text"},
		SupportedOutputModalities:  []string{"image"},
	}
	if kind == EditKind {
		// Edit models share display names with their generation siblings.
		info.DisplayName = name + " Edit"
		info.Description = firstNonEmpty(model.ModelSpec.Description, name+" via Venice web image editing")
		info.SupportedGenerationMethods = []string{"images.edits"}
		info.SupportedInputModalities = []string{"text", "image"}
	}
	return info
}
