package auth

import (
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

// Settings holds the plugin-owned configuration stored under
// plugins.configs.<pluginID>. The host hands the plugin that block alone, so
// these keys sit at the top level beside the host's own enabled and priority.
//
// Every field supplies a default for new logins. A login that carries its own
// project selection, from the management UI or from a command line flag, wins.
type Settings struct {
	// ProjectID is either a comma-separated string, matching the
	// --geminicli-project-id flag, or a YAML list.
	ProjectID any `yaml:"project_id"`
	// ManualProjects keeps the configured list instead of discovering
	// projects, and therefore requires ProjectID.
	ManualProjects bool `yaml:"manual_projects"`
}

// ConfigFields describes Settings for management clients so they can render a
// form. Declaring the fields next to the parser that consumes them keeps the
// two from drifting apart.
func ConfigFields() []pluginapi.ConfigField {
	return []pluginapi.ConfigField{
		{
			Name:        projectIDKey,
			Type:        pluginapi.ConfigFieldTypeString,
			Description: "Default Google Cloud project ID for new logins. Accepts a comma-separated list. Leave empty to discover projects automatically.",
		},
		{
			Name:        manualProjectsKey,
			Type:        pluginapi.ConfigFieldTypeBoolean,
			Description: "Use the fixed list from project_id instead of discovering projects. Requires project_id.",
		},
	}
}

// ParseSettings reads the plugin configuration block. Malformed YAML yields the
// zero value rather than an error: the plugin is built before it can report
// one, and losing the defaults beats refusing to load at all.
func ParseSettings(configYAML []byte) Settings {
	if len(configYAML) == 0 {
		return Settings{}
	}
	var settings Settings
	if errUnmarshal := yaml.Unmarshal(configYAML, &settings); errUnmarshal != nil {
		return Settings{}
	}
	return settings
}

// defaults converts the settings into the login selection they describe.
func (s Settings) defaults() projectSelection {
	return projectSelection{
		Manual: s.ManualProjects,
		IDs:    configProjectIDs(s.ProjectID),
	}
}

// configProjectIDs accepts both the comma-separated form the command line flag
// uses and a YAML list, so a hand-edited config reads naturally either way.
func configProjectIDs(value any) []string {
	switch typed := value.(type) {
	case string:
		return splitProjectIDList(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
		return cleanStringList(out)
	}
	return nil
}
