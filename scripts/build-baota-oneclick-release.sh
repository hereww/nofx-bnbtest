#!/usr/bin/env bash
set -euo pipefail
export COPYFILE_DISABLE=1

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TS="${1:-$(date +%Y%m%d-%H%M%S)}"
RELEASE_DIR="${ROOT_DIR}/release"
STAGE_DIR="${RELEASE_DIR}/.stage-nofx-${TS}"
PAYLOAD_NAME="nofx-oneclick-payload-${TS}.tar.gz"
PAYLOAD_PATH="${RELEASE_DIR}/${PAYLOAD_NAME}"
ONECLICK_PATH="${RELEASE_DIR}/nofx-baota-oneclick-${TS}.sh"

mkdir -p "$RELEASE_DIR"
rm -rf "$STAGE_DIR"
mkdir -p "$STAGE_DIR"

copy_tree() {
  if command -v rsync >/dev/null 2>&1; then
    rsync -a \
      --exclude='.git/' \
      --exclude='.github/' \
      --exclude='.DS_Store' \
      --exclude='.tmp_*' \
      --exclude='release/' \
      --exclude='web/node_modules/' \
      --exclude='web/dist/' \
      --exclude='node_modules/' \
      --exclude='nofx' \
      --exclude='nofx_test' \
      --exclude='*.log' \
      --exclude='*.db' \
      --exclude='*.sqlite' \
      --exclude='*.sqlite3' \
      --exclude='.env' \
      --exclude='.env.*' \
      --exclude='/data/' \
      "$ROOT_DIR/" "$STAGE_DIR/"
  else
    tar -C "$ROOT_DIR" \
      --exclude='.git' \
      --exclude='.github' \
      --exclude='.DS_Store' \
      --exclude='.tmp_*' \
      --exclude='release' \
      --exclude='web/node_modules' \
      --exclude='web/dist' \
      --exclude='node_modules' \
      --exclude='nofx' \
      --exclude='nofx_test' \
      --exclude='*.log' \
      --exclude='*.db' \
      --exclude='*.sqlite' \
      --exclude='*.sqlite3' \
      --exclude='.env' \
      --exclude='.env.*' \
      --exclude='./data' \
      --exclude='./data/*' \
      -cf - . | tar -C "$STAGE_DIR" -xf -
  fi
}

copy_tree
chmod +x "$STAGE_DIR/scripts/baota-redeploy.sh" "$STAGE_DIR/scripts/baota-oneclick-deploy.sh"
tar -C "$STAGE_DIR" -czf "$PAYLOAD_PATH" .
rm -rf "$STAGE_DIR"

payload_bytes="$(wc -c <"$PAYLOAD_PATH" | tr -d '[:space:]')"
if command -v sha256sum >/dev/null 2>&1; then
  payload_sha256="$(sha256sum "$PAYLOAD_PATH" | awk '{print $1}')"
else
  payload_sha256="$(shasum -a 256 "$PAYLOAD_PATH" | awk '{print $1}')"
fi

cat >"$ONECLICK_PATH" <<SCRIPT_HEADER
#!/usr/bin/env bash
set -euo pipefail

APP_DIR="\${APP_DIR:-/www/wwwroot/nofx}"
DEPLOY_HTTP_PROXY="\${DEPLOY_HTTP_PROXY:-}"
TMP_DIR="\$(mktemp -d /tmp/nofx-oneclick.XXXXXX)"
PKG="\${TMP_DIR}/payload.tar.gz"
PAYLOAD_BYTES="${payload_bytes}"
PAYLOAD_SHA256="${payload_sha256}"

cleanup() {
  rm -rf "\$TMP_DIR"
}
trap cleanup EXIT

log() {
  printf '[NOFX oneclick] %s\n' "\$*"
}

fail() {
  printf '[NOFX oneclick] ERROR: %s\n' "\$*" >&2
  exit 1
}

command -v awk >/dev/null 2>&1 || fail "awk is not installed"
command -v tar >/dev/null 2>&1 || fail "tar is not installed"

log "Extracting embedded package"
payload_line="\$(awk '/^__NOFX_PAYLOAD_BELOW__\$/ { print NR + 1; exit 0; }' "\$0")"
[ -n "\$payload_line" ] || fail "embedded package marker not found; upload may be incomplete"
tail -n +"\$payload_line" "\$0" >"\$PKG"

actual_bytes="\$(wc -c <"\$PKG" | tr -d '[:space:]')"
if [ "\$actual_bytes" != "\$PAYLOAD_BYTES" ]; then
  fail "embedded package is incomplete: expected \${PAYLOAD_BYTES} bytes, got \${actual_bytes}. Re-upload the .sh file in binary/file mode, not by editing or copying its text."
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual_sha256="\$(sha256sum "\$PKG" | awk '{print \$1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual_sha256="\$(shasum -a 256 "\$PKG" | awk '{print \$1}')"
else
  actual_sha256=""
fi
if [ -n "\$actual_sha256" ] && [ "\$actual_sha256" != "\$PAYLOAD_SHA256" ]; then
  fail "embedded package checksum mismatch. Re-upload the .sh file in binary/file mode."
fi

if [ "\${NOFX_EXTRACT_ONLY:-0}" = "1" ]; then
  log "Extract-only check passed"
  exit 0
fi

export APP_DIR
export DEPLOY_HTTP_PROXY
export DEPLOY_MARKET_HTTP_PROXY="\${DEPLOY_MARKET_HTTP_PROXY:-\${MARKET_HTTP_PROXY:-\$DEPLOY_HTTP_PROXY}}"

log "Deploying to \${APP_DIR}"
log "Using outbound proxy: configured"
mkdir -p "\$APP_DIR"
tar -xzf "\$PKG" -C "\$APP_DIR"
cd "\$APP_DIR"
chmod +x scripts/baota-redeploy.sh scripts/baota-oneclick-deploy.sh || true
bash scripts/baota-redeploy.sh "\$APP_DIR"
exit 0

__NOFX_PAYLOAD_BELOW__
SCRIPT_HEADER
cat "$PAYLOAD_PATH" >>"$ONECLICK_PATH"

chmod +x "$ONECLICK_PATH"

cat <<EOF
Created:
  $PAYLOAD_PATH
  $ONECLICK_PATH
EOF
