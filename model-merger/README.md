# Model Merger Plugin

This plugin adds virtual models to CLIProxyAPI. A virtual model forwards each request to an ordered list of real models and moves on to the next one when a model fails (fallback). Virtual models are managed on their own page in the management center.

Plugin ID: `nopro200-model-merger`. Display name: **Model Merger (NoPro200)**.

Example: `mixed-image-gen` tries `gpt-image-2.5-sunburst` first and falls back to `qwen-image-3-pro`.

## Capabilities

- Serves virtual image models on `/v1/images/generations` (`type_of_model: "openai-image"`). Chat models are planned; the file format already carries the type.
- Tries the targets strictly in order. A failed fallback never returns to an earlier target.
- Falls back on configurable HTTP statuses, on transport errors and on timeouts. `no_fallback_on_status` wins over `fallback_on_status`.
- Optional per-model switch to fall back when a target's content filter blocks the prompt (`fallback_on_moderation`, off by default).
- Sends exactly the client's request to each target; only the `model` field changes. The plugin never rewrites, softens or hides a prompt. Each target applies its own rules.
- When every target fails, the client gets the status and error body of the last target that was tried.
- Optional per-model cooldown that skips a recently failed target for a while.
- Lists each enabled virtual model in `/v1/models` under its alias.
- Logs every fallback decision to the CLIProxyAPI log.

Requires CLIProxyAPI v7.3.4 or newer: the plugin is built with SDK v7.3.20 and declares RPC schema version 6.

## Installation

Add this repository's registry to the host `config.yaml`, install `nopro200-model-merger` from the plugin store and give the plugin an access key:

```yaml
api-keys:
  - "sk-your-client-key"
  - "sk-model-merger"          # used only by the plugin

plugins:
  enabled: true
  store-sources:
    - "https://raw.githubusercontent.com/NoPro200/CLI-Proxy-API-Plugins/main/registry.json"
  configs:
    nopro200-model-merger:
      enabled: true
      access-key: "sk-model-merger"   # required, one of api-keys
      port: 8317                      # optional, default 8317
      # loopback-url: "https://127.0.0.1:8317"  # optional, overrides port (e.g. with TLS)
```

### Why a loopback call

The plugin reaches the targets through the host's own `POST http://127.0.0.1:<port>/v1/images/generations`, authenticated with `access-key`. It does not use a proxy for this call.

SDK v7.3.20 offers an internal model call (`host.model.execute`), but it runs requests with image models disallowed: models such as `gpt-image-2.5-sunburst` are refused with `503 model ... is only supported on /v1/images/generations and /v1/images/edits`. The public images route is the only path that serves every image model, so the plugin uses it.

The access key is visible to anyone who can read the plugin config through the management API. Use a dedicated key.

## Auth Storage

Each virtual model is one auth file of type `model-merger`. The management page writes these files; they can also be written by hand:

```json
{
  "type": "model-merger",
  "alias": "mixed-image-gen",
  "type_of_model": "openai-image",
  "targets": [
    { "model": "gpt-image-2.5-sunburst" },
    { "model": "qwen-image-3-pro", "timeout": 120 }
  ],
  "fallback_on_status": [408, 429, 500, 502, 503, 504],
  "no_fallback_on_status": [],
  "cooldown_seconds": 0,
  "enabled": true,
  "fallback_on_moderation": false,
  "moderation_patterns": ["safety system", "content policy", "moderation"]
}
```

| Field | Meaning |
| --- | --- |
| `alias` | Model name clients use. Must not be the name of a real model or of another virtual model. |
| `type_of_model` | `openai-image` (default). Other values are rejected for now. |
| `targets` | Ordered real models. `timeout` is in seconds; omitted or `0` waits as long as the client does. At least one target, no duplicates, no virtual models. |
| `fallback_on_status` | Statuses that move on to the next target. Omitted: `408, 429, 500, 502, 503, 504`. `[]`: none. |
| `no_fallback_on_status` | Statuses that never move on; wins over `fallback_on_status`. |
| `cooldown_seconds` | Skip a target for this long after it failed with a fallback error (not after a content filter block). In memory only. When every target is cooling down, all are tried in order. |
| `enabled` | `false` hides the model from `/v1/models` and refuses requests. Default `true`. |
| `fallback_on_moderation` | See below. Default `false`. |
| `moderation_patterns` | Case-insensitive substrings of `error.message` that mark a content filter block. Omitted: the three defaults above. |

## Content Filter Blocks

A target's reply counts as a content filter block when it is an HTTP 400 and

- `error.code` is `moderation_blocked`, or
- `error.type` is `image_generation_user_error`, or
- `error.message` contains one of `moderation_patterns`.

CLIProxyAPI often wraps an upstream error body as the message string; the plugin unpacks such nested JSON before it checks.

- `fallback_on_moderation: false`: the block is an ordinary error without fallback. The client gets the 400 unchanged.
- `fallback_on_moderation: true`: the plugin sends the same request to the next target, which applies its own rules.

A block is decided by this switch alone, even when `400` is listed in `fallback_on_status`.

## Model Management

The management page is served at:

```text
/v0/resource/plugins/nopro200-model-merger/models
```

It asks for the CLIProxyAPI management key once per browser tab and keeps it in `sessionStorage`. A rejected key is dropped right away, so a wrong key costs one failed attempt instead of feeding the host's IP ban. The page creates, edits and deletes virtual models, reorders targets, sets the status lists and turns models and the content filter switch on and off.

JSON routes (management key required):

```text
GET    /v0/management/plugins/model-merger/models.json   list, defaults and known model IDs
POST   /v0/management/plugins/model-merger/models.json   create (body: the fields above)
PUT    /v0/management/plugins/model-merger/models.json   update (body: fields plus "name" of the file)
DELETE /v0/management/plugins/model-merger/models.json?name=<file>
```

The same routes also answer without `.json`; a browser request without `Accept: application/json` then gets the page. Responses never contain the management key or the access key.

The plugin host has no callback to delete auth files. `DELETE` forwards the caller's management key once to the host's `DELETE /v0/management/auth-files` over the loopback address, and only for files of this plugin.

## Limits

- Images only; `/v1/images/edits` and streaming are refused for virtual models.
- The check that an alias is not a real model's name uses `/v1/models`, which leaves out models whose credentials are cooling down. It is best effort.
- The host may retry a failed request (`request-retry`); a retry runs the whole chain again from the first target.
- With the host's non-streaming keep-alive enabled, a target's error arrives as `200` plus an error body. The plugin treats it as a failure with status 502 for the status lists and still checks it for content filter blocks.

## Build

```bash
go test ./...
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared \
  -o dist/nopro200-model-merger.so ./cmd/model-merger
```

Release archives are built by the repository workflow from a `v*` tag.

## License

MIT
