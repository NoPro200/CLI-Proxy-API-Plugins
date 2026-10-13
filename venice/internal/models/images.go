package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

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

// ErrNoEditModel reports an image model that Venice offers no edit model for.
var ErrNoEditModel = errors.New("venice image model has no edit model")

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
		if model.ID == modelID {
			return specFrom(model), nil
		}
	}
	return ImageSpec{}, fmt.Errorf("%w: %q", ErrModelNotFound, modelID)
}

// ResolveEdit finds the Venice edit model behind a listed model, because the
// plugin lists each edit model under the image model it belongs to. Edit
// models without one are listed, and resolve, under their own ID.
func ResolveEdit(ctx context.Context, client pluginapi.HostHTTPClient, modelID string) (string, ImageSpec, error) {
	editCatalog, err := fetchImageCatalog(ctx, client, EditKind)
	if err != nil {
		return "", ImageSpec{}, err
	}
	edits := online(editCatalog)
	byID := make(map[string]imageCatalogModel, len(edits))
	for _, model := range edits {
		byID[model.ID] = model
	}
	for _, id := range []string{modelID, modelID + "-edit"} {
		if model, ok := byID[id]; ok {
			return id, specFrom(model), nil
		}
	}
	genCatalog, err := fetchImageCatalog(ctx, client, ImageKind)
	if err != nil {
		return "", ImageSpec{}, err
	}
	gens := online(genCatalog)
	if id := editPartners(gens, edits)[modelID]; id != "" {
		return id, specFrom(byID[id]), nil
	}
	for _, model := range gens {
		if model.ID == modelID {
			return "", ImageSpec{}, fmt.Errorf("%w: %q", ErrNoEditModel, modelID)
		}
	}
	return "", ImageSpec{}, fmt.Errorf("%w: %q", ErrModelNotFound, modelID)
}

// editPartners maps image model IDs to their edit model: the "-edit" sibling,
// else the only unclaimed edit model with the same display name (Venice names
// a few pairs differently, such as grok-imagine-image and grok-imagine-edit).
func editPartners(gens, edits []imageCatalogModel) map[string]string {
	partners := make(map[string]string)
	claimed := make(map[string]bool)
	editIDs := make(map[string]bool, len(edits))
	for _, model := range edits {
		editIDs[model.ID] = true
	}
	for _, model := range gens {
		if editIDs[model.ID+"-edit"] {
			partners[model.ID] = model.ID + "-edit"
			claimed[model.ID+"-edit"] = true
		}
	}
	editsByName := make(map[string][]string)
	for _, model := range edits {
		if name := baseName(model); name != "" && !claimed[model.ID] {
			editsByName[name] = append(editsByName[name], model.ID)
		}
	}
	gensByName := make(map[string]int)
	for _, model := range gens {
		gensByName[baseName(model)]++
	}
	for _, model := range gens {
		name := baseName(model)
		if ids := editsByName[name]; partners[model.ID] == "" && name != "" && len(ids) == 1 && gensByName[name] == 1 {
			partners[model.ID] = ids[0]
		}
	}
	return partners
}

// addImageModels lists each image model once, with editing when it has an
// edit model, and gives only edit models without one an entry of their own.
func addImageModels(out *[]pluginapi.ModelInfo, seen map[string]bool, gens, edits []imageCatalogModel) {
	gens, edits = online(gens), online(edits)
	partners := editPartners(gens, edits)
	paired := make(map[string]bool, len(partners))
	for _, model := range gens {
		info := imageModelInfo(model, ImageKind)
		if editID := partners[model.ID]; editID != "" {
			info.SupportedGenerationMethods = append(info.SupportedGenerationMethods, "images.edits")
			info.SupportedInputModalities = []string{"text", "image"}
			paired[editID] = true
		}
		addModel(out, seen, info)
	}
	for _, model := range edits {
		if !paired[model.ID] {
			addModel(out, seen, imageModelInfo(model, EditKind))
		}
	}
}

func specFrom(model imageCatalogModel) ImageSpec {
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
	}
}

func online(models []imageCatalogModel) []imageCatalogModel {
	out := make([]imageCatalogModel, 0, len(models))
	for _, model := range models {
		if !model.ModelSpec.Offline {
			out = append(out, model)
		}
	}
	return out
}

// baseName compares display names without suffixes such as "(SOTA)".
func baseName(model imageCatalogModel) string {
	name, _, _ := strings.Cut(model.ModelSpec.Name, "(")
	return strings.ToLower(strings.TrimSpace(name))
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
