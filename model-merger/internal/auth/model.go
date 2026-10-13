package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	// ProviderKey is the provider of every virtual model auth. The host only asks
	// this plugin for models and execution of auths with this provider.
	ProviderKey = "model-merger"
	// StorageType is the "type" of a virtual model auth file.
	StorageType = "model-merger"
	// PluginID must match the library file name (nopro200-model-merger.<ext>),
	// because the host derives the resource URLs from it.
	PluginID = "nopro200-model-merger"

	// ImageModelType is the model type CLIProxyAPI routes /v1/images requests by.
	ImageModelType = "openai-image"
)

// DefaultFallbackStatus lists the statuses that switch to the next target when
// a file does not set fallback_on_status: timeouts, rate limits and server errors.
var DefaultFallbackStatus = []int{408, 429, 500, 502, 503, 504}

// DefaultModerationPatterns are matched against error messages to recognise a
// content filter block that carries neither a code nor a type.
var DefaultModerationPatterns = []string{"safety system", "content policy", "moderation"}

// supportedModelTypes lists the type_of_model values the executor can serve.
var supportedModelTypes = []string{ImageModelType}

// Target is one real model a virtual model forwards to.
type Target struct {
	Model string `json:"model"`
	// Timeout in seconds for this target; 0 waits as long as the client does.
	Timeout int `json:"timeout,omitempty"`
}

// VirtualModel is the content of one model-merger auth file.
type VirtualModel struct {
	Type                 string   `json:"type"`
	Alias                string   `json:"alias"`
	TypeOfModel          string   `json:"type_of_model"`
	Targets              []Target `json:"targets"`
	FallbackOnStatus     []int    `json:"fallback_on_status"`
	NoFallbackOnStatus   []int    `json:"no_fallback_on_status"`
	CooldownSeconds      int      `json:"cooldown_seconds"`
	Enabled              bool     `json:"enabled"`
	FallbackOnModeration bool     `json:"fallback_on_moderation"`
	ModerationPatterns   []string `json:"moderation_patterns"`
}

// fileModel mirrors VirtualModel with pointers where a missing field has a
// default that differs from Go's zero value.
type fileModel struct {
	Type                 string   `json:"type"`
	Alias                string   `json:"alias"`
	TypeOfModel          string   `json:"type_of_model"`
	Targets              []Target `json:"targets"`
	FallbackOnStatus     []int    `json:"fallback_on_status"`
	NoFallbackOnStatus   []int    `json:"no_fallback_on_status"`
	CooldownSeconds      int      `json:"cooldown_seconds"`
	Enabled              *bool    `json:"enabled"`
	FallbackOnModeration bool     `json:"fallback_on_moderation"`
	ModerationPatterns   []string `json:"moderation_patterns"`
}

// ErrNotModelMerger reports JSON that belongs to another provider.
var ErrNotModelMerger = errors.New("not a model-merger auth")

// ParseVirtualModel decodes and normalises an auth file. It returns
// ErrNotModelMerger for files of other providers and does not validate.
func ParseVirtualModel(raw []byte) (VirtualModel, error) {
	var file fileModel
	if errUnmarshal := json.Unmarshal(raw, &file); errUnmarshal != nil {
		return VirtualModel{}, fmt.Errorf("decode model-merger auth: %w", errUnmarshal)
	}
	if !strings.EqualFold(strings.TrimSpace(file.Type), StorageType) {
		return VirtualModel{}, ErrNotModelMerger
	}
	model := VirtualModel{
		Type:                 StorageType,
		Alias:                strings.TrimSpace(file.Alias),
		TypeOfModel:          strings.TrimSpace(file.TypeOfModel),
		Targets:              make([]Target, 0, len(file.Targets)),
		FallbackOnStatus:     file.FallbackOnStatus,
		NoFallbackOnStatus:   file.NoFallbackOnStatus,
		CooldownSeconds:      file.CooldownSeconds,
		Enabled:              file.Enabled == nil || *file.Enabled,
		FallbackOnModeration: file.FallbackOnModeration,
		ModerationPatterns:   file.ModerationPatterns,
	}
	if model.TypeOfModel == "" {
		model.TypeOfModel = ImageModelType
	}
	for _, target := range file.Targets {
		model.Targets = append(model.Targets, Target{Model: strings.TrimSpace(target.Model), Timeout: target.Timeout})
	}
	// A missing list takes the default; an explicit empty list means none.
	if model.FallbackOnStatus == nil {
		model.FallbackOnStatus = slices.Clone(DefaultFallbackStatus)
	}
	if model.NoFallbackOnStatus == nil {
		model.NoFallbackOnStatus = []int{}
	}
	if model.ModerationPatterns == nil {
		model.ModerationPatterns = slices.Clone(DefaultModerationPatterns)
	}
	patterns := model.ModerationPatterns[:0]
	for _, pattern := range model.ModerationPatterns {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			patterns = append(patterns, pattern)
		}
	}
	model.ModerationPatterns = patterns
	return model, nil
}

// Validate checks a virtual model on its own and against the aliases of all
// other virtual models, so a target can never lead back into a virtual model.
func (m VirtualModel) Validate(otherAliases []string) error {
	var problems []string
	if m.Alias == "" {
		problems = append(problems, "alias is required")
	} else if strings.ContainsAny(m.Alias, " \t\r\n") {
		problems = append(problems, "alias must not contain whitespace")
	}
	if !slices.Contains(supportedModelTypes, m.TypeOfModel) {
		problems = append(problems, fmt.Sprintf("type_of_model %q is not supported yet (supported: %s)", m.TypeOfModel, strings.Join(supportedModelTypes, ", ")))
	}
	if len(m.Targets) == 0 {
		problems = append(problems, "at least one target is required")
	}
	virtual := make(map[string]bool, len(otherAliases)+1)
	for _, alias := range otherAliases {
		virtual[strings.ToLower(strings.TrimSpace(alias))] = true
	}
	virtual[strings.ToLower(m.Alias)] = true
	seen := make(map[string]bool, len(m.Targets))
	for i, target := range m.Targets {
		key := strings.ToLower(target.Model)
		switch {
		case target.Model == "":
			problems = append(problems, fmt.Sprintf("target %d has no model", i+1))
		case virtual[key]:
			problems = append(problems, fmt.Sprintf("target %q is a virtual model; targets must be real models", target.Model))
		case seen[key]:
			problems = append(problems, fmt.Sprintf("target %q is listed twice", target.Model))
		}
		seen[key] = true
		if target.Timeout < 0 {
			problems = append(problems, fmt.Sprintf("target %q has a negative timeout", target.Model))
		}
	}
	for _, status := range append(slices.Clone(m.FallbackOnStatus), m.NoFallbackOnStatus...) {
		if status < 100 || status > 599 {
			problems = append(problems, fmt.Sprintf("status %d is not an HTTP status code", status))
		}
	}
	if m.CooldownSeconds < 0 {
		problems = append(problems, "cooldown_seconds must not be negative")
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: slices.Compact(problems)}
	}
	return nil
}

// ValidationError lists every problem of a virtual model at once.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid virtual model: " + strings.Join(e.Problems, "; ")
}

// JSON returns the canonical auth file content.
func (m VirtualModel) JSON() []byte {
	m.Type = StorageType
	raw, _ := json.MarshalIndent(m, "", "  ")
	return raw
}

// FileName is the auth file name for an alias.
func FileName(alias string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(alias)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), ".-")
	if name == "" {
		name = "model"
	}
	return "model-merger-" + name + ".json"
}
