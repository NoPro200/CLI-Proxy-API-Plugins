# Venice Provider Plugin

This plugin adds Venice web-chat upstream support to CLIProxyAPI through the native plugin ABI. It signs in with the browser session cookie of a Venice account instead of a Venice API key.

It is a fork of [trungking/cpa-plugin-venice](https://github.com/trungking/cpa-plugin-venice), imported at commit `f90447b`, and is released from this repository under the plugin ID `nopro200-venice`.

## Capabilities

- Parses `type: "venice"` auth files.
- Supports command-line account import with `--venice-login` and `--venice-cookie`.
- Uses the durable Clerk `__client` cookie from `clerk.venice.ai` to mint fresh Venice bearer tokens.
- Executes OpenAI `chat.completions` requests against `https://outerface.venice.ai/api/inference/chat`. The host translates other client protocols.
- Converts Venice newline-delimited response chunks into OpenAI-compatible non-streaming responses.
- Passes Venice streaming chunks through the host stream path.
- Lists each account's live Venice model catalog with display names. Offline models and E2EE models, which need client-side encryption, are left out.
- Reports plan, credits and rate limits through the CLIProxyAPI quota API, which the management center shows on its quota and auth file pages (releases after v1.25.6).
- Exposes Venice account status, quota metadata and a realtime request monitor through CLIProxyAPI plugin management routes.

Requires CLIProxyAPI v7.3.4 or newer: the plugin is built with SDK v7.3.20 and declares RPC schema version 6.

## Models

Venice offers models whose IDs other providers also serve, for example `claude-opus-4-8`. CLIProxyAPI then routes such a request to any credential that serves the ID, Venice included. With `routing.strategy: fill-first`, the `priority` field of an auth file decides the order (higher first). Give the Venice auth file a lower `priority` than the other credentials to use Venice only when they are unavailable, or hide models from Venice under `oauth.excluded-models.venice` (`oauth-excluded-models` in the older flat config layout), which accepts wildcards such as `claude-*`.

## Installation

Add this repository's registry to the host `config.yaml` and install `nopro200-venice` from the plugin store:

```yaml
plugins:
  enabled: true
  store-sources:
    - "https://raw.githubusercontent.com/NoPro200/CLI-Proxy-API-Plugins/main/registry.json"
  configs:
    nopro200-venice:
      enabled: true
      priority: 1
```

The plugin reads no further options. For a manual install, place `nopro200-venice.so` (`.dylib` on macOS, `.dll` on Windows) in `plugins/<goos>/<goarch>/`. The host derives the plugin ID from the file name, and the plugin's resource links expect `nopro200-venice`.

## Command-Line Flags

- `--venice-login`: opens Venice and prompts for a `__client` cookie or Cookie header.
- `--venice-cookie`: creates auth from a pasted `__client=...`, Cookie header, Cookie Editor JSON, or copied cURL containing `__client`.

Use the host `--no-browser` flag to skip opening the browser automatically. Run the commands in the host's working directory so that the host finds its `config.yaml` and plugin directory. The command saves the auth file and exits without starting a server.

```bash
cli-proxy-api --venice-cookie '__client=...'
cli-proxy-api --venice-login --no-browser
```

## Auth Storage

The plugin stores provider-owned auth JSON:

```json
{
  "type": "venice",
  "email": "user@example.com",
  "cookie": "__client=...",
  "authorization": "Bearer ...",
  "authorization_expires_at": "2026-06-27T18:30:00Z",
  "user_id": "user_...",
  "account_plan": "pro",
  "quota_checked_at": "2026-06-28T00:00:00Z",
  "quota": {
    "balance": {
      "creditsRemaining": 42
    }
  }
}
```

Only `cookie` with a valid `__client` is required. The authorization fields are cached and refreshed automatically.
Quota fields are collected from Venice's user session response when present, with auth tokens and cookies filtered out.

## Account Management

Management routes require the CLIProxyAPI management key:

- `GET /v0/management/plugins/venice/accounts` (HTML; `?refresh=1` refreshes every Venice credential)
- `GET /v0/management/plugins/venice/accounts.json`
- `GET /v0/management/plugins/venice/realtime.json`
- `GET`, `POST /v0/management/cpa-plugin-venice-login`

Browser pages, also listed in the management panel:

- `/v0/resource/plugins/nopro200-venice/accounts`
- `/v0/resource/plugins/nopro200-venice/realtime`
- `/v0/resource/plugins/nopro200-venice/login`

CLIProxyAPI serves `/v0/resource/...` pages without the management key, so these pages carry no account data. They ask for the management key once per browser tab, keep it in `sessionStorage`, and load all data through the management routes. A rejected key is discarded at once, so it counts as a single failed attempt toward the host's IP ban (5 failures, 30 minutes).

The account data includes email, status, request counts, token expiry, plan, quota, and the time the quota was checked. Cookies and bearer tokens are never returned.

The host refreshes each Venice account every 10 minutes, which keeps the quota current. Requests renew the short-lived bearer token on their own.

## Build

From this directory, with Go 1.26 or newer and a C toolchain for cgo:

```bash
go test ./...
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o dist/nopro200-venice.so ./cmd/venice
```

Releases come from the repository workflow: a `v*` tag builds every plugin in this repository with the tag's version and publishes them in one GitHub release.

## License

This project is licensed under the [MIT License](LICENSE).
