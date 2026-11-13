package models

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	authpkg "github.com/trungking/cpa-plugin-venice/internal/auth"
)

// webIDs maps API model ids to the ids the Venice web app uses, per kind. It
// stays nil until a web model list arrived and keeps the last one when a later
// fetch fails.
// shortcut: one index for all accounts; web lists differ per plan, so split it per auth once free and paid accounts are mixed.
var webIDs atomic.Pointer[map[string]map[string]string]

type webModel struct {
	ID         string   `json:"id"`
	APIModelID string   `json:"apiModelId"`
	AliasIDs   []string `json:"aliasIds"`
	Active     *bool    `json:"active"`
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
	ids, known := (*index)[kind]
	if !known {
		return apiID, true
	}
	if id, ok := ids[apiID]; ok {
		return id, true
	}
	return apiID, false
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
	if index := indexWebModels(body); len(index) > 0 {
		webIDs.Store(&index)
	}
}

func indexWebModels(body []byte) map[string]map[string]string {
	var catalog struct {
		Text    webModelGroup `json:"text"`
		Image   webModelGroup `json:"image"`
		Inpaint webModelGroup `json:"inpaint"`
	}
	if json.Unmarshal(body, &catalog) != nil {
		return nil
	}
	index := make(map[string]map[string]string)
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
		}
		// An empty kind stays unknown, so a changed web list cannot hide every model of it.
		if len(ids) > 0 {
			index[kind] = ids
		}
	}
	return index
}
