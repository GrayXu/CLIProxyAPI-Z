# CLIProxyAPI Local Agent Notes

## Repository Shape

- This repo is primarily a Go application with one main server entrypoint:
  - `cmd/server`
- There is one auxiliary CLI utility:
  - `cmd/fetch_antigravity_models`
- Core implementation lives under `internal/`, with the main working areas:
  - `internal/api`: HTTP server and request handling
  - `internal/config`: config loading, defaults, compatibility handling
  - `internal/store`: token persistence backends such as postgres/git/object store
  - `internal/registry`: model catalog, refresh logic, embedded fallback models
  - `internal/managementasset`: bundled management HTML, runtime sync/update logic
  - `internal/tui`: terminal management UI
  - `internal/usage`: usage persistence and logging
  - `internal/watcher`: config/watch reload path
  - `internal/wsrelay`: websocket relay path
- Public reusable API surface lives under `sdk/`.
- Higher-level regression coverage also exists in `test/`.
- Documentation for SDK consumers lives in `docs/sdk-*.md`.
- Runtime auth data belongs in `auths/`; treat it as local state, not a source tree for committed fixtures.

## Default Editing Boundaries

- For server behavior changes, prefer editing `internal/*` and keep `cmd/server` as a thin wiring layer unless the CLI surface itself changes.
- For reusable embedding/API changes, prefer `sdk/*`; if behavior changes there, check whether `docs/sdk-*.md` and `examples/*` also need updates.
- For management panel changes, edit the subtree source in `third_party/Cli-Proxy-API-Management-Center`, not the bundled HTML.
- Do not hand-edit generated or embedded artifacts unless it is an intentional hotfix and the source path cannot be used.
- The working tree may already contain unrelated local changes. Do not revert them unless explicitly asked.

## Build And Validation Defaults

- Default repo-level build validation is:

```bash
go build ./cmd/server
```

- CI currently refreshes the embedded model catalog before building `cmd/server`; do not assume a raw local build has that refresh step.
- When touching targeted Go packages, prefer focused tests first, then broader ones as needed:

```bash
go test ./internal/...
go test ./sdk/...
go test ./test/...
```

- If a change is committed as `feat`, update `README.md` in the same change to describe the user-visible behavior or capability that was added.
- If a change alters SDK-facing behavior, keep `docs/sdk-*.md` and relevant `examples/*` in sync.

## Embedded And Generated Assets

### Management WebUI Source Of Truth

- The editable management WebUI source lives in the local vendored subtree:
  - `third_party/Cli-Proxy-API-Management-Center`
- The repository-tracked bundled artifact consumed by CLIProxyAPI is:
  - `internal/managementasset/bundled/management.html`
- That bundled HTML is also embedded into the Go binary via `internal/managementasset/updater.go`.
- Do not edit `internal/managementasset/bundled/management.html` by hand unless there is an emergency hotfix.
- Normal flow: edit WebUI source in the subtree directory, then rebuild the bundled HTML artifact.

### Runtime Management Asset Path

- The runtime-served management page is not necessarily the embedded file.
- The current systemd deployment uses:
  - `MANAGEMENT_STATIC_PATH=/var/lib/cliproxyapi/static`
- In that deployment shape, `/management.html` is served from:
  - `/var/lib/cliproxyapi/static/management.html`
- `internal/managementasset/updater.go` can also materialize or refresh the runtime copy automatically; keep bundled asset, runtime static asset, and subtree source conceptually separate.

### Embedded Model Catalog

- The embedded fallback model catalog lives at:
  - `internal/registry/models/models.json`
- It is embedded into the binary by `internal/registry/model_updater.go`.
- CI refreshes this file from `router-for-me/models` before build/release jobs.
- Do not casually hand-edit `internal/registry/models/models.json`; only do so when intentionally updating the fallback snapshot or debugging catalog bootstrap behavior.

## Build Management HTML

- Build command from repo root:

```bash
scripts/build-management-html.sh
```

- What it does:
  - enters `third_party/Cli-Proxy-API-Management-Center`
  - runs `npm ci`
  - runs `npm run build`
  - copies `dist/index.html` to `internal/managementasset/bundled/management.html`

## Deploy Management HTML

- Deploy command from repo root:

```bash
scripts/build-management-html.sh --deploy-static
```

- This additionally copies the built HTML to:
  - `/var/lib/cliproxyapi/static/management.html`

## Management WebUI Operational Rules

- The upstream Management WebUI source is tracked via `git subtree`, not `git submodule`.
- Do not reintroduce `.gitmodules`, nested `.git` directories, or any new submodule wiring for `third_party/Cli-Proxy-API-Management-Center`.
- The default local remote name for upstream sync is `management-center`.
- The default upstream sync flow from repo root is:

```bash
git fetch management-center main
git subtree pull --prefix=third_party/Cli-Proxy-API-Management-Center management-center main --squash
```

- By default, keep fork-specific Management WebUI changes in this repo unless there is an explicit decision to upstream them.
- When working on management UI changes for this repo:
  1. edit files inside `third_party/Cli-Proxy-API-Management-Center`
  2. run `scripts/build-management-html.sh`
  3. if needed, run `scripts/build-management-html.sh --deploy-static`
  4. if the UI source changed, commit both the subtree source changes and `internal/managementasset/bundled/management.html` together
  5. if the goal is only to refresh the deployed HTML from already-reviewed subtree source, committing only `internal/managementasset/bundled/management.html` is acceptable

## Operations Notes

- `ops/sop.md` documents the current local deploy/update flow for Gray's environment.
- Treat `ops/sop.md` as an operational runbook, not a generic development workflow.
- Do not modify config files, auth files, installed binaries, or systemd units unless the task explicitly requires ops work.

## Current Fork-Specific UI Features To Preserve

- `quota-sticky` routing strategy in management UI
- usage stats `30d` range
- request events fields:
  - `requested_fast_mode`
  - `service_tier`
  - fast mode status display/export
- advanced model pricing fields:
  - `fast_mode_multiplier`
  - `input_over_272k`
