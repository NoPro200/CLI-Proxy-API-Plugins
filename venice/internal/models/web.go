package models

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	authpkg "github.com/trungking/cpa-plugin-venice/internal/auth"
)

// webIDs holds what the Venice web app's model list says. It stays nil until a
// web model list arrived and keeps the last one when a later fetch fails.
// shortcut: one index for all accounts; web lists differ per plan, so split it per auth once free and paid accounts are mixed.
var webIDs atomic.Pointer[webIndex]

type webIndex struct {
	ids       map[string]map[string]string // kind -> API model id -> web id
	reasoning map[string]ReasoningMode     // web id of a text model -> its reasoning switches
}

// ReasoningMode is how the Venice web app runs reasoning for a text model.
type ReasoningMode struct {
	Supported        bool // the model reasons
	Toggle           bool // users can switch reasoning on and off
	ForceOff         bool // reasoning is always off
	ForwardEncrypted bool // the web app sends no /think or /no_think for it
}

type webModel struct {
	ID                        string   `json:"id"`
	APIModelID                string   `json:"apiModelId"`
	AliasIDs                  []string `json:"aliasIds"`
	Active                    *bool    `json:"active"`
	SupportsReasoning         bool     `json:"supportsReasoning"`
	SupportsReasonToggle      bool     `json:"supportsReasonToggle"`
	ForceDisableReasoning     bool     `json:"forceDisableReasoning"`
	ForwardEncryptedReasoning bool     `json:"forwardEncryptedReasoning"`
}

type webModelGroup struct {
	Models []webModel `json:"models"`
}

// WebID returns the id the Venice web app uses for an API model id of the
// given kind, and whether the web app offers that model at all. Until a web
// model list arrived, every model passes unchanged.
func WebID(kind, apiID string) (string, bool) {
	index := webIDs.Load()
	if index == nil {
		return apiID, true
	}
	ids, known := index.ids[kind]
	if !known {
		return apiID, true
	}
	if id, ok := ids[apiID]; ok {
		return id, true
	}
	return apiID, false
}

// Reasoning returns the reasoning switches of a text model by its web id. ok is
// false until a web model list arrived or when the list lacks the model.
func Reasoning(webID string) (ReasoningMode, bool) {
	index := webIDs.Load()
	if index == nil {
		return ReasoningMode{}, false
	}
	mode, ok := index.reasoning[webID]
	return mode, ok
}

func refreshWebIDs(ctx context.Context, client pluginapi.HostHTTPClient, storageJSON []byte) {
	storage, errParse := authpkg.ParseStorage(storageJSON)
	if errParse != nil || storage == nil {
		return
	}
	body, errFetch := authpkg.FetchWebModels(ctx, client, storage)
	if errFetch != nil {
		return
	}
	if index := indexWebModels(body); index != nil && len(index.ids) > 0 {
		webIDs.Store(index)
	}
}

func indexWebModels(body []byte) *webIndex {
	var catalog struct {
		Text    webModelGroup `json:"text"`
		Image   webModelGroup `json:"image"`
		Inpaint webModelGroup `json:"inpaint"`
	}
	if json.Unmarshal(body, &catalog) != nil {
		return nil
	}
	index := &webIndex{ids: make(map[string]map[string]string), reasoning: make(map[string]ReasoningMode)}
	for kind, group := range map[string]webModelGroup{TextKind: catalog.Text, ImageKind: catalog.Image, EditKind: catalog.Inpaint} {
		usable := make([]webModel, 0, len(group.Models))
		for _, model := range group.Models {
			if model.ID != "" && (model.Active == nil || *model.Active) {
				usable = append(usable, model)
			}
		}
		ids := make(map[string]string)
		// Later loops win: a model's own id beats another model's apiModelId, which beats an alias.
		for _, model := range usable {
			for _, alias := range model.AliasIDs {
				ids[alias] = model.ID
			}
		}
		for _, model := range usable {
			if model.APIModelID != "" {
				ids[model.APIModelID] = model.ID
			}
		}
		for _, model := range usable {
			ids[model.ID] = model.ID
			if kind == TextKind {
				index.reasoning[model.ID] = ReasoningMode{
					Supported:        model.SupportsReasoning,
					Toggle:           model.SupportsReasonToggle,
					ForceOff:         model.ForceDisableReasoning,
					ForwardEncrypted: model.ForwardEncryptedReasoning,
				}
			}
		}
		// An empty kind stays unknown, so a changed web list cannot hide every model of it.
		if len(ids) > 0 {
			index.ids[kind] = ids
		}
	}
	return index
}
