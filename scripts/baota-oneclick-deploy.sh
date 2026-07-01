#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/www/wwwroot/nofx}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG="${1:-${PKG:-}}"

log() {
  printf '[NOFX oneclick] %s\n' "$*"
}

fail() {
  printf '[NOFX oneclick] ERROR: %s\n' "$*" >&2
  exit 1
}

find_package() {
  local dir="$1"
  find "$dir" -maxdepth 1 -type f \( -name 'nofx-baota-*.tar.gz' -o -name 'nofx-oneclick-payload-*.tar.gz' \) \
    | sort \
    | tail -1
}

if [ -z "$PKG" ]; then
  PKG="$(find_package "$SCRIPT_DIR")"
fi

[ -n "$PKG" ] || fail "package not found. Pass tar.gz path as the first argument or set PKG=/path/to/package.tar.gz"
[ -f "$PKG" ] || fail "package not found: $PKG"
command -v tar >/dev/null 2>&1 || fail "tar is not installed"

log "Using package: $PKG"
log "Using app dir: $APP_DIR"
mkdir -p "$APP_DIR"
tar -xzf "$PKG" -C "$APP_DIR"
cd "$APP_DIR"

chmod +x scripts/baota-redeploy.sh || true
export DEPLOY_HTTP_PROXY="${DEPLOY_HTTP_PROXY:-}"
export DEPLOY_MARKET_HTTP_PROXY="${DEPLOY_MARKET_HTTP_PROXY:-${MARKET_HTTP_PROXY:-${DEPLOY_HTTP_PROXY:-}}}"

bash scripts/baota-redeploy.sh "$APP_DIR"
