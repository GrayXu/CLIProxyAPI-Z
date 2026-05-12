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

## Upstream Merge, Review, And Deployment Workflow

- For large upstream sync work, use a separate worktree/branch instead of the main working tree. The main worktree may contain Gray's unrelated local changes and must not be dirtied or reset.
- Default remotes:
  - `origin`: Gray's fork, target branch `main`
  - `upstream`: `router-for-me/CLIProxyAPI`, branch `main`
  - `management-center`: `router-for-me/Cli-Proxy-API-Management-Center`, branch `main`
- Start by fetching all relevant remotes:

```bash
git fetch origin main
git fetch upstream main
git fetch management-center main
```

- Merge the Go/backend upstream first, then merge the Management WebUI subtree separately:

```bash
git merge upstream/main
git subtree pull --prefix=third_party/Cli-Proxy-API-Management-Center management-center main --squash
```

- During conflict resolution, preserve this fork's local behavior unless Gray explicitly decides otherwise. In particular, keep:
  - `quota-sticky` routing strategy in the Management WebUI
  - usage stats `30d` range
  - request event fields `requested_fast_mode` and `service_tier`
  - fast mode status display/export
  - advanced pricing fields `fast_mode_multiplier` and `input_over_272k`
  - legacy `/v0/management/usage` behavior
  - admin/viewer management route split and public issue API key flow
- After any Management WebUI source change or subtree merge, run `scripts/build-management-html.sh` and commit the WebUI source, `third_party/Cli-Proxy-API-Management-Center/dist/index.html`, and `internal/managementasset/bundled/management.html` together. Avoid ending with a standalone `npm run build`; the final generated artifacts should come from the repo script.
- Maintain the root `merge.log` as an append-only local merge record. After each upstream or Management WebUI sync, add a top entry covering source commits, conflict decisions, generated assets, validation commands, and push/deploy status.
- Run focused tests for touched packages first, then the broad validation set:

```bash
go test ./internal/...
go test ./sdk/...
go test ./test/...
go build -o /tmp/cliproxyapi-server-merge-verify ./cmd/server
scripts/build-management-html.sh
```

- Before merging into `origin/main`, ask a subagent to review the resolved branch against `origin/main`. The review must explicitly check:
  - conflict residue and accidental reversions
  - fork-specific WebUI/usage features listed above
  - backend behavior introduced by upstream, especially auth, websocket, usage queue, API key usage, and management routes
  - generated Management WebUI artifacts versus source
  - missing tests or deployment risks
- Fix blocking/high review findings before merging. Medium findings that affect fork-specific behavior should normally be fixed in the same merge branch.
- If Gray asks to merge directly rather than create a PR, create an explicit merge commit on top of `origin/main` and push that to `origin/main`; do not fast-forward the remote silently:

```bash
git switch -c merge/direct-origin-main-YYYYMMDD origin/main
git merge --no-ff <resolved-merge-branch>
git push origin HEAD:main
```

## Local Deployment Workflow

- The current systemd service is `cliproxyapi.service`.
- The production binary path is `/usr/local/bin/cliproxyapi`.
- The service currently runs with:

```text
ExecStart=/usr/local/bin/cliproxyapi -config /etc/cliproxyapi/config.yaml
MANAGEMENT_STATIC_PATH=/var/lib/cliproxyapi/static
```

- For full backend deployment, build with release-style ldflags so `/usr/local/bin/cliproxyapi --version` prints useful metadata:

```bash
VERSION=$(git describe --tags --dirty --always)
COMMIT=$(git rev-parse --short HEAD)
BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" \
  -o /tmp/cliproxyapi-${COMMIT} ./cmd/server
```

- Before replacing the production binary or runtime Management HTML, create a timestamped backup under `/var/lib/cliproxyapi/backups/`, for example:

```bash
TS=$(date -u +%Y%m%dT%H%M%SZ)
BACKUP_DIR=/var/lib/cliproxyapi/backups/deploy-${TS}
mkdir -p "$BACKUP_DIR"
cp -a /usr/local/bin/cliproxyapi "$BACKUP_DIR/cliproxyapi"
cp -a /var/lib/cliproxyapi/static/management.html "$BACKUP_DIR/management.html"
```

- Replace the binary with `install -o root -g root -m 0755`, replace runtime Management HTML with `install -o root -g root -m 0644`, then restart and verify:

```bash
install -o root -g root -m 0755 /tmp/cliproxyapi-${COMMIT} /usr/local/bin/cliproxyapi
install -o root -g root -m 0644 internal/managementasset/bundled/management.html /var/lib/cliproxyapi/static/management.html
systemctl restart cliproxyapi.service
systemctl status cliproxyapi.service --no-pager -l
journalctl -u cliproxyapi.service -n 80 --no-pager
```

- For WebUI-only fixes after the backend binary is already deployed, rebuild with `scripts/build-management-html.sh`, commit and push the source/generated artifacts, backup `/var/lib/cliproxyapi/static/management.html`, then replace only that static file. A service restart is not required for a static HTML-only update.
- Do not edit `/etc/cliproxyapi/config.yaml`, auth files, systemd unit files, or other runtime state during deployment unless Gray explicitly asks.

## Operations Notes

- If `ops/sop.md` exists, it documents the current local deploy/update flow for Gray's environment.
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
