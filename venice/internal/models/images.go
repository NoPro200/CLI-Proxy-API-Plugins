package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// ImageModelType is the model type CLIProxyAPI routes /v1/images requests by.
const ImageModelType = "openai-image"

type imageCatalogModel struct {
	ID        string `json:"id"`
	Created   int64  `json:"created"`
	ModelSpec struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Offline     bool   `json:"offline"`
		Constraints struct {
			Steps struct {
				Default int `json:"default"`
			} `json:"steps"`
			WidthHeightDivisor int      `json:"widthHeightDivisor"`
			AspectRatios       []string `json:"aspectRatios"`
			DefaultResolution  string   `json:"defaultResolution"`
			Qualities          []string `json:"qualities"`
		} `json:"constraints"`
	} `json:"model_spec"`
}

// ImageSpec holds the generation limits Venice publishes for an image model.
type ImageSpec struct {
	Steps             int
	Divisor           int
	AspectRatios      []string
	DefaultResolution string
	Qualities         []string
}

// FetchImageSpec looks the model up in the live image catalog, because the
// limits differ per model and Venice rejects requests outside them.
func FetchImageSpec(ctx context.Context, client pluginapi.HostHTTPClient, modelID string) (ImageSpec, error) {
	catalog, err := fetchImageCatalog(ctx, client)
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
		}, nil
	}
	return ImageSpec{}, fmt.Errorf("venice image model %q not found", modelID)
}

func fetchImageCatalog(ctx context.Context, client pluginapi.HostHTTPClient) ([]imageCatalogModel, error) {
	resp, err := client.Do(ctx, pluginapi.HTTPRequest{
		Method: http.MethodGet,
		URL:    catalogURL + "?type=image",
		Headers: http.Header{
			"Accept":     []string{"application/json"},
			"User-Agent": []string{"Mozilla/5.0"},
		},
	})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("venice image catalog: status %d", resp.StatusCode)
	}
	var payload struct {
		Data []imageCatalogModel `json:"data"`
	}
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		return nil, fmt.Errorf("decode venice image catalog: %w", err)
	}
	return payload.Data, nil
}

func imageModelInfo(model imageCatalogModel) pluginapi.ModelInfo {
	name := firstNonEmpty(model.ModelSpec.Name, model.ID)
	return pluginapi.ModelInfo{
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
}
