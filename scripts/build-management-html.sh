#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEBUI_DIR="${ROOT_DIR}/third_party/Cli-Proxy-API-Management-Center"
OUTPUT_HTML="${ROOT_DIR}/internal/managementasset/bundled/management.html"
DEPLOY_HTML="/var/lib/cliproxyapi/static/management.html"

deploy_static=false

usage() {
  cat <<'EOF'
Usage: scripts/build-management-html.sh [--deploy-static]

Build the management WebUI from the local vendored subtree checkout and refresh:
  internal/managementasset/bundled/management.html

Options:
  --deploy-static   Also copy the built HTML to /var/lib/cliproxyapi/static/management.html
EOF
}

for arg in "$@"; do
  case "$arg" in
    --deploy-static)
      deploy_static=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ ! -d "${WEBUI_DIR}" ]]; then
  echo "missing webui directory: ${WEBUI_DIR}" >&2
  exit 1
fi

if [[ ! -f "${WEBUI_DIR}/package.json" ]]; then
  echo "webui subtree directory is incomplete: ${WEBUI_DIR}" >&2
  exit 1
fi

pushd "${WEBUI_DIR}" >/dev/null
npm ci
npm run build
popd >/dev/null

install -D -m 0644 "${WEBUI_DIR}/dist/index.html" "${OUTPUT_HTML}"

if [[ "${deploy_static}" == "true" ]]; then
  install -D -m 0644 "${WEBUI_DIR}/dist/index.html" "${DEPLOY_HTML}"
fi

sha256sum "${OUTPUT_HTML}"

if [[ "${deploy_static}" == "true" ]]; then
  sha256sum "${DEPLOY_HTML}"
fi
