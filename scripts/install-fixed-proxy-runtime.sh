#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/www/wwwroot/nofx}"
CONFIG_SOURCE="${1:-/tmp/nofx-sing-box.json}"
EXPECTED_IP="${NOFX_PROXY_EXPECTED_IP:-}"
PROXY_PORT="${NOFX_PROXY_PORT:-18080}"
PROXY_URL="http://nofx-proxy:18080"

log() {
  printf '[NOFX proxy] %s\n' "$*"
}

fail() {
  printf '[NOFX proxy] ERROR: %s\n' "$*" >&2
  exit 1
}

set_env_var() {
  local key="$1"
  local value="$2"
  local tmp
  tmp="$(mktemp)"
  grep -v "^${key}=" .env >"$tmp" || true
  printf '%s=%s\n' "$key" "$value" >>"$tmp"
  mv "$tmp" .env
}

env_value() {
  local key="$1"
  grep "^${key}=" .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '"' || true
}

ensure_no_proxy() {
  local key="$1"
  local current value
  current="$(env_value "$key")"
  for value in localhost 127.0.0.1 ::1 nofx nofx-frontend nofx-data-gateway nofx-onchain-indexer nofx-proxy; do
    case ",${current}," in
      *",${value},"*) ;;
      *) current="${current:+${current},}${value}" ;;
    esac
  done
  set_env_var "$key" "$current"
}

detect_compose() {
  if docker compose version >/dev/null 2>&1; then
    COMPOSE_CMD=(docker compose)
  elif command -v docker-compose >/dev/null 2>&1; then
    COMPOSE_CMD=(docker-compose)
  else
    fail "Docker Compose is not installed"
  fi
}

clear_compose_proxy_environment() {
  unset HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY
  unset http_proxy https_proxy all_proxy no_proxy
}

wait_backend() {
  local i
  for i in $(seq 1 60); do
    if curl -fsS --max-time 5 http://127.0.0.1:8080/api/health >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

main() {
  [ -d "$APP_DIR" ] || fail "app directory does not exist: ${APP_DIR}"
  [ -s "$CONFIG_SOURCE" ] || fail "sing-box config does not exist: ${CONFIG_SOURCE}"
  cd "$APP_DIR"
  [ -f .env ] || fail ".env does not exist"
  [ -f docker-compose.yml ] || fail "docker-compose.yml does not exist"
  [ -f docker-compose.proxy.yml ] || fail "docker-compose.proxy.yml does not exist"
  detect_compose

  local stamp backup running_before running_after actual_ip binance_status
  stamp="$(date +%Y%m%d%H%M%S)"
  backup="/www/wwwroot/nofx-backups/proxy-before-${stamp}"
  install -m 700 -d "$backup" data/proxy
  cp -a .env "$backup/.env"
  [ ! -f data/proxy/sing-box.json ] || cp -a data/proxy/sing-box.json "$backup/sing-box.json"
  install -m 600 "$CONFIG_SOURCE" data/proxy/sing-box.json

  for key in HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy all_proxy MARKET_HTTP_PROXY ONCHAIN_ARCHIVE_HTTP_PROXY; do
    set_env_var "$key" "$PROXY_URL"
  done
  set_env_var NOFX_FIXED_PROXY_URL "$PROXY_URL"
  set_env_var NOFX_PROXY_PORT "$PROXY_PORT"
  [ -z "$EXPECTED_IP" ] || set_env_var NOFX_PROXY_EXPECTED_IP "$EXPECTED_IP"
  ensure_no_proxy NO_PROXY
  ensure_no_proxy no_proxy
  clear_compose_proxy_environment

  "${COMPOSE_CMD[@]}" -f docker-compose.yml -f docker-compose.proxy.yml config >/tmp/nofx-proxy-compose-config.yml
  running_before="$(sqlite3 data/data.db 'SELECT COUNT(*) FROM traders WHERE is_running=1;' 2>/dev/null || printf unknown)"

  log "Starting fixed egress proxy"
  "${COMPOSE_CMD[@]}" -f docker-compose.yml -f docker-compose.proxy.yml up -d nofx-proxy

  actual_ip=""
  for _ in $(seq 1 20); do
    actual_ip="$(env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
      curl -4 -fsS --max-time 10 --proxy "http://127.0.0.1:${PROXY_PORT}" https://ifconfig.me/ip 2>/dev/null || true)"
    [ -n "$actual_ip" ] && break
    sleep 2
  done
  [ -n "$actual_ip" ] || fail "fixed proxy did not become ready"
  if [ -n "$EXPECTED_IP" ] && [ "$actual_ip" != "$EXPECTED_IP" ]; then
    fail "fixed proxy exit mismatch: expected ${EXPECTED_IP}, got ${actual_ip}"
  fi

  binance_status="$(env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
    curl -4 -sS --max-time 20 --proxy "http://127.0.0.1:${PROXY_PORT}" \
    -o /tmp/nofx-binance-proxy-smoke.json -w '%{http_code}' \
    https://fapi.binance.com/fapi/v1/time || true)"
  [ "$binance_status" = "200" ] || fail "fixed proxy cannot reach Binance Futures API (HTTP ${binance_status:-000})"

  log "Recreating backend services with fixed proxy environment"
  "${COMPOSE_CMD[@]}" -f docker-compose.yml -f docker-compose.proxy.yml up -d --force-recreate \
    nofx-data-gateway nofx-onchain-indexer nofx
  wait_backend || fail "backend did not become healthy"

  running_after="$(sqlite3 data/data.db 'SELECT COUNT(*) FROM traders WHERE is_running=1;' 2>/dev/null || printf unknown)"
  log "Backup: ${backup}"
  log "Fixed exit: ${actual_ip}"
  log "Binance Futures: HTTP ${binance_status}"
  log "Running traders before/after: ${running_before}/${running_after}"
  "${COMPOSE_CMD[@]}" -f docker-compose.yml -f docker-compose.proxy.yml ps
}

main "$@"
