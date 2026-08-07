# Gemini CLI Provider Plugin

This plugin adds Gemini CLI upstream provider support to CLIProxyAPI through the native plugin ABI. It does not restore the host-side `/v1internal:generateContent` or `/v1internal:streamGenerateContent` inbound routes; those endpoints are used only as upstream Cloud Code API targets.

## Capabilities

- Parses `gemini` and `gemini-cli` auth storage files.
- Expands one physical auth file into multiple virtual auths when `project_ids` contains more than one Google Cloud project.
- Discovers Google Cloud projects automatically during login, or uses a fixed, user-supplied project list when manual project mode is enabled.
- Supports host Web OAuth through the host `/v0/management/oauth-callback` flow.
- Supports command-line login through `--geminicli-login`.
- After a successful OAuth login, automatically enables the Gemini Code Assist `EXPERIMENTAL` release channel for saved project IDs so preview models are available.
- Executes generate, stream, and token count requests through the host HTTP client when the host invokes the executor.
- Translates OpenAI, Responses, Claude, Gemini, and Codex payloads to the Gemini CLI provider envelope.
- Applies Gemini CLI thinking config under `request.generationConfig.thinkingConfig`.

## Configuration

The plugin reads its own block under `plugins.configs` in the host `config.yaml`. Both fields are optional and are declared to management clients, so the management UI renders a form for them instead of reporting that the plugin has no visual config fields.

```yaml
plugins:
  configs:
    nopro200-gemini-cli:
      enabled: true
      project_id: project-a,project-b
      manual_projects: true
```

- `project_id`: default Google Cloud project for new logins. Accepts a comma-separated string or a YAML list. Leave it empty to discover projects automatically.
- `manual_projects`: uses the fixed `project_id` list instead of discovering projects. It requires `project_id`; starting a login without one fails with the same error as the command-line flag.

These values are defaults only. A login that carries its own project selection, whether from a command-line flag or from Web OAuth login metadata, takes precedence, so the configuration merely saves you from repeating the flags on every login. The one asymmetry: `manual_projects` can be switched on by either source but not off, because there is no negative form of `--geminicli-manual-projects`. To return to automatic discovery, clear the setting in the configuration.

Invalid YAML in the block is ignored and the plugin loads with no defaults rather than refusing to start.

## Command-Line Flags

- `--geminicli-login`: starts an interactive Gemini CLI login.
- `--geminicli-project-id`: sets the preferred project for the saved auth. Accepts a comma-separated list; the first entry becomes the primary project.
- `--geminicli-manual-projects`: turns off project discovery and uses exactly the projects passed through `--geminicli-project-id`.

Use the host `--no-browser` flag to skip opening the browser automatically during command-line login. After opening the login URL, the command can accept a pasted callback URL when the local HTTP callback is unavailable. OAuth login saves a single physical auth file; multiple projects are kept in `project_ids` and expanded as virtual auths when the file is loaded. After login finalization, the plugin best-effort configures each project for the `EXPERIMENTAL` release channel via the Cloud AI Companion API (`releaseChannelSettings` + `settingBindings`). Login still succeeds if that configuration call fails. OAuth login timeout and polling interval are fixed in code. Proxy handling comes from the host configuration; the plugin has no plugin-specific proxy option.

## Project Selection

Login resolves the Google Cloud projects in one of two modes.

Automatic (default, unchanged behavior): the plugin lists the account projects through Cloud Resource Manager and falls back to Code Assist auto-discovery when that list is empty. `--geminicli-project-id` only picks the primary project.

Manual: the plugin performs no discovery and stores exactly the projects you supply. Enable it with `--geminicli-manual-projects` and pass the IDs as a comma-separated list:

```bash
cli-proxy-api --geminicli-login --geminicli-manual-projects --geminicli-project-id=project-a,project-b,project-c
```

When the toggle is set without `--geminicli-project-id`, the command asks for the list on stdin and fails when nothing is entered. The first ID becomes the primary project and is onboarded through Code Assist; the remaining IDs are saved in `project_ids` and expanded into virtual auths. A Code Assist onboarding response that names a different project does not override the configured list.

For the host Web OAuth flow the same toggle is passed as login metadata: `manual_projects: true` plus a comma-separated `project_id`. Starting a manual login without project IDs fails before the browser opens.

## Auth Storage

The plugin stores provider-owned auth JSON with `type: "gemini-cli"`. A single physical storage file can include:

```json
{
  "type": "gemini-cli",
  "email": "user@example.com",
  "project_id": "primary-project",
  "project_ids": ["primary-project", "secondary-project"],
  "access_token": "access-token",
  "refresh_token": "refresh-token"
}
```

The first auth is the physical auth. Additional projects are exposed as virtual auths with `metadata.virtual=true`, `metadata.parent_auth_id`, `attributes.project_id`, and `attributes.runtime_only=true`.

## Upstream Endpoints

The executor targets the Cloud Code upstream endpoints:

- `POST https://cloudcode-pa.googleapis.com/v1internal:generateContent`
- `POST https://cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse`
- `POST https://cloudcode-pa.googleapis.com/v1internal:countTokens`

The plugin injects `Authorization`, `User-Agent`, and `X-Goog-Api-Client` headers before dispatching through the host HTTP client.

## License

This project is licensed under the [MIT License](LICENSE).

Copyright (c) 2026.6-present Router-For.ME
