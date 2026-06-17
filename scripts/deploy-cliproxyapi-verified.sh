#!/usr/bin/env bash
set -Eeuo pipefail

SERVICE_NAME="${SERVICE_NAME:-cliproxyapi.service}"
INSTALL_PATH="${INSTALL_PATH:-/usr/local/bin/cliproxyapi}"
CONFIG_PATH="${CONFIG_PATH:-/etc/cliproxyapi/config.yaml}"
BACKUP_ROOT="${BACKUP_ROOT:-/var/lib/cliproxyapi/backups}"
BASE_URL="${CLIPROXYAPI_VERIFY_URL:-http://127.0.0.1:8317}"
VERIFY_MODEL="${CLIPROXYAPI_VERIFY_MODEL:-gpt-5.5}"
VERIFY_INPUT="${CLIPROXYAPI_VERIFY_INPUT:-Return exactly: ok}"
CURL_MAX_TIME="${CLIPROXYAPI_VERIFY_CURL_MAX_TIME:-45}"
HEALTH_RETRIES="${CLIPROXYAPI_VERIFY_HEALTH_RETRIES:-20}"
HEALTH_SLEEP="${CLIPROXYAPI_VERIFY_HEALTH_SLEEP:-1}"

usage() {
  cat <<'USAGE'
Usage: scripts/deploy-cliproxyapi-verified.sh

Required environment:
  CLIPROXYAPI_VERIFY_API_KEY    API key used for the real /v1/responses check.

Optional environment:
  CLIPROXYAPI_VERIFY_URL        Default: http://127.0.0.1:8317
  CLIPROXYAPI_VERIFY_MODEL      Default: gpt-5.5
  CLIPROXYAPI_VERIFY_INPUT      Default: Return exactly: ok
  CLIPROXYAPI_VERIFY_PAYLOAD    Full JSON payload. Overrides model/input defaults.
  CLIPROXYAPI_VERIFY_CURL_MAX_TIME  Default: 45
  SERVICE_NAME                  Default: cliproxyapi.service
  INSTALL_PATH                  Default: /usr/local/bin/cliproxyapi
  CONFIG_PATH                   Default: /etc/cliproxyapi/config.yaml
  BACKUP_ROOT                   Default: /var/lib/cliproxyapi/backups
USAGE
}

log() {
  printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

die() {
  log "ERROR: $*"
  rollback
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

json_safe_value() {
  case "$1" in
    *['"\'$'\n\r\t'\\]*)
      die "$2 contains characters that require CLIPROXYAPI_VERIFY_PAYLOAD"
      ;;
  esac
}

rollback() {
  set +e
  if [[ "${INSTALLED_NEW_BINARY:-false}" != "true" ]]; then
    set -e
    return
  fi
  if [[ -z "${BACKUP_BINARY:-}" || ! -f "${BACKUP_BINARY}" ]]; then
    log "rollback skipped: backup binary not found"
    set -e
    return
  fi
  log "rolling back binary from ${BACKUP_BINARY}"
  install -o root -g root -m 0755 "${BACKUP_BINARY}" "${INSTALL_PATH}"
  systemctl restart "${SERVICE_NAME}"
  systemctl status "${SERVICE_NAME}" --no-pager -l || true
  set -e
}

on_error() {
  local exit_code=$?
  rollback
  exit "${exit_code}"
}

trap on_error ERR

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

API_KEY="${CLIPROXYAPI_VERIFY_API_KEY:-}"
[[ -n "${API_KEY}" ]] || die "CLIPROXYAPI_VERIFY_API_KEY is required"

require_command go
require_command git
require_command curl
require_command install
require_command sha256sum
require_command systemctl

[[ -f "${CONFIG_PATH}" ]] || die "config file not found: ${CONFIG_PATH}"

VERSION="$(git describe --tags --dirty --always)"
COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
BUILD_OUT="/tmp/cliproxyapi-${COMMIT}"

log "building ${BUILD_OUT}"
CGO_ENABLED=0 GOOS=linux go build \
  -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" \
  -o "${BUILD_OUT}" ./cmd/server

"${BUILD_OUT}" -h >/tmp/cliproxyapi-new-version.txt 2>&1 || true
if ! grep -Fq "${COMMIT}" /tmp/cliproxyapi-new-version.txt; then
  die "new binary version output does not contain commit ${COMMIT}: $(cat /tmp/cliproxyapi-new-version.txt)"
fi

TS="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_DIR="${BACKUP_ROOT}/deploy-${TS}"
BACKUP_BINARY="${BACKUP_DIR}/cliproxyapi"
mkdir -p "${BACKUP_DIR}"

if [[ -f "${INSTALL_PATH}" ]]; then
  cp -a "${INSTALL_PATH}" "${BACKUP_BINARY}"
  "${INSTALL_PATH}" -h >"${BACKUP_DIR}/cliproxyapi.version" 2>&1 || true
  sha256sum "${INSTALL_PATH}" >"${BACKUP_DIR}/cliproxyapi.sha256"
else
  log "no existing binary at ${INSTALL_PATH}; rollback will not be available"
fi

log "installing ${BUILD_OUT} to ${INSTALL_PATH}"
install -o root -g root -m 0755 "${BUILD_OUT}" "${INSTALL_PATH}"
INSTALLED_NEW_BINARY=true

log "restarting ${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"

log "waiting for /healthz"
health_ok=false
for ((i = 1; i <= HEALTH_RETRIES; i++)); do
  if curl -fsS --max-time "${CURL_MAX_TIME}" "${BASE_URL%/}/healthz" >/tmp/cliproxyapi-healthz.txt; then
    health_ok=true
    break
  fi
  sleep "${HEALTH_SLEEP}"
done
[[ "${health_ok}" == "true" ]] || die "/healthz did not become healthy"

if [[ -n "${CLIPROXYAPI_VERIFY_PAYLOAD:-}" ]]; then
  VERIFY_PAYLOAD="${CLIPROXYAPI_VERIFY_PAYLOAD}"
else
  json_safe_value "${VERIFY_MODEL}" "CLIPROXYAPI_VERIFY_MODEL"
  json_safe_value "${VERIFY_INPUT}" "CLIPROXYAPI_VERIFY_INPUT"
  VERIFY_PAYLOAD='{"model":"'"${VERIFY_MODEL}"'","input":"'"${VERIFY_INPUT}"'","store":false,"max_output_tokens":16}'
fi

log "checking real /v1/responses with model ${VERIFY_MODEL}"
curl -fsS --max-time "${CURL_MAX_TIME}" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d "${VERIFY_PAYLOAD}" \
  "${BASE_URL%/}/v1/responses" >/tmp/cliproxyapi-responses-verify.json

python3 - <<'PY'
import json
import sys

path = "/tmp/cliproxyapi-responses-verify.json"
with open(path, "r", encoding="utf-8") as fh:
    payload = json.load(fh)

if payload.get("error") is not None:
    print("/v1/responses returned a non-null error payload", file=sys.stderr)
    sys.exit(1)
if not payload.get("id"):
    print("/v1/responses response did not contain an id", file=sys.stderr)
    sys.exit(1)
PY

systemctl is-active --quiet "${SERVICE_NAME}" || die "${SERVICE_NAME} is not active after verification"

log "deployment verified"
log "backup: ${BACKUP_DIR}"
log "version: $(cat /tmp/cliproxyapi-new-version.txt)"

trap - ERR
