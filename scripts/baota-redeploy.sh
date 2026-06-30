#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-/www/wwwroot/nofx}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
PROXY_COMPOSE_FILE="${PROXY_COMPOSE_FILE:-docker-compose.proxy.yml}"
PROXY_ENABLED=0

log() {
  printf '[NOFX deploy] %s\n' "$*"
}

fail() {
  printf '[NOFX deploy] ERROR: %s\n' "$*" >&2
  exit 1
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

configure_compose_files() {
  COMPOSE_ARGS=(-f "$COMPOSE_FILE")
  if [ -s data/proxy/sing-box.json ]; then
    [ -f "$PROXY_COMPOSE_FILE" ] || fail "fixed proxy config exists but compose override is missing: ${PROXY_COMPOSE_FILE}"
    COMPOSE_ARGS+=(-f "$PROXY_COMPOSE_FILE")
    PROXY_ENABLED=1
  fi
}

clear_compose_proxy_environment() {
  unset HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY
  unset http_proxy https_proxy all_proxy no_proxy
}

set_env_var() {
  local key="$1"
  local value="$2"
  local tmp
  tmp="$(mktemp)"
  [ -f .env ] && grep -v "^${key}=" .env >"$tmp" || true
  printf '%s=%s\n' "$key" "$value" >>"$tmp"
  mv "$tmp" .env
}

unset_env_var() {
  local key="$1"
  local tmp
  [ -f .env ] || return 0
  tmp="$(mktemp)"
  grep -v "^${key}=" .env >"$tmp" || true
  mv "$tmp" .env
}

env_value() {
  local key="$1"
  grep "^${key}=" .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '"' || true
}

inherit_legacy_runtime() {
  local legacy_dir="${NOFX_LEGACY_DIR:-/www/wwwroot/nofx-current}"
  [ "${NOFX_INHERIT_LEGACY:-1}" = "1" ] || return 0
  [ "$APP_DIR" != "$legacy_dir" ] || return 0
  [ -d "$legacy_dir" ] || return 0

  if [ ! -s .env ] && [ -s "$legacy_dir/.env" ]; then
    log "Inheriting existing .env from ${legacy_dir}"
    cp "$legacy_dir/.env" .env
    chmod 600 .env || true
  fi

  mkdir -p data
  if [ ! -s data/data.db ] && [ -s "$legacy_dir/data/data.db" ]; then
    log "Inheriting existing primary database from ${legacy_dir}/data/data.db"
    cp -a "$legacy_dir/data/data.db" data/data.db
  fi

  if [ ! -s data/data-gateway.db ] && [ -s "$legacy_dir/data/data-gateway.db" ]; then
    log "Inheriting existing data gateway database from ${legacy_dir}/data/data-gateway.db"
    cp -a "$legacy_dir/data/data-gateway.db" data/data-gateway.db
  fi
}

ensure_csv_env_values() {
  local key="$1"
  shift
  local current value
  current="$(env_value "$key")"
  for value in "$@"; do
    case ",${current}," in
      *",${value},"*) ;;
      *) current="${current:+${current},}${value}" ;;
    esac
  done
  set_env_var "$key" "$current"
}

published_host_port() {
  local value="$1"
  local fallback="$2"
  value="${value:-$fallback}"
  case "$value" in
    *:*) printf '%s\n' "${value##*:}" ;;
    *) printf '%s\n' "$value" ;;
  esac
}

ensure_env() {
  if [ ! -f .env ]; then
    if [ -f .env.example ]; then
      cp .env.example .env
    else
      touch .env
    fi
    chmod 600 .env || true
  fi

  [ -n "$(env_value NOFX_BACKEND_PORT)" ] || set_env_var NOFX_BACKEND_PORT "8080"
  [ -n "$(env_value NOFX_FRONTEND_PORT)" ] || set_env_var NOFX_FRONTEND_PORT "3000"
  [ -n "$(env_value DATA_GATEWAY_PORT)" ] || set_env_var DATA_GATEWAY_PORT "127.0.0.1:8090"
  [ -n "$(env_value DATA_GATEWAY_DB_PATH)" ] || set_env_var DATA_GATEWAY_DB_PATH "data/data-gateway.db"
  [ -n "$(env_value DATA_GATEWAY_REFRESH_INTERVAL)" ] || set_env_var DATA_GATEWAY_REFRESH_INTERVAL "1m"
  [ -n "$(env_value DATA_GATEWAY_URL)" ] || set_env_var DATA_GATEWAY_URL "http://nofx-data-gateway:8090"
  [ -n "$(env_value ONCHAIN_INDEXER_URL)" ] || set_env_var ONCHAIN_INDEXER_URL "http://nofx-onchain-indexer:8091"
  [ -n "$(env_value ONCHAIN_INDEXER_DB_PATH)" ] || set_env_var ONCHAIN_INDEXER_DB_PATH "data/onchain-indexer.db"
  current_batch_blocks="$(env_value ONCHAIN_INDEXER_BATCH_BLOCKS)"
  if [ -z "$current_batch_blocks" ] || [ "$current_batch_blocks" -gt 100 ] 2>/dev/null; then
    set_env_var ONCHAIN_INDEXER_BATCH_BLOCKS "100"
  fi
  current_free_rpcs="$(env_value ONCHAIN_FREE_RPC_URLS)"
  if [ -z "$current_free_rpcs" ] || \
    [ "$current_free_rpcs" = "https://bsc-rpc.publicnode.com,https://1rpc.io/bnb" ] || \
    [ "$current_free_rpcs" = "https://1rpc.io/bnb,https://bsc-rpc.publicnode.com" ] || \
    [ "$current_free_rpcs" = "https://bsc-rpc.publicnode.com,https://bsc-dataseed.binance.org" ]; then
    set_env_var ONCHAIN_FREE_RPC_URLS "https://bsc-rpc.publicnode.com,https://binance.llamarpc.com,https://bsc-dataseed.binance.org,https://bsc-dataseed1.binance.org,https://bsc-dataseed2.binance.org"
  fi
  current_free_log_source="$(env_value ONCHAIN_FREE_LOG_SOURCE)"
  if [ -z "$current_free_log_source" ] || [ "$current_free_log_source" = "geckoterminal" ] || [ "$current_free_log_source" = "rpc" ] || [ "$current_free_log_source" = "etherscan,rpc" ]; then
    set_env_var ONCHAIN_FREE_LOG_SOURCE "etherscan,gecko"
  fi
  [ -n "$(env_value ONCHAIN_GECKO_TRADES_LIMIT)" ] || set_env_var ONCHAIN_GECKO_TRADES_LIMIT "300"
  [ -n "$(env_value ONCHAIN_ETHERSCAN_BASE_URL)" ] || set_env_var ONCHAIN_ETHERSCAN_BASE_URL "https://api.etherscan.io/v2/api"
  [ -n "$(env_value ONCHAIN_FREE_LOGS_RPS)" ] || set_env_var ONCHAIN_FREE_LOGS_RPS "2"
  [ -n "$(env_value ONCHAIN_FREE_LOGS_DAILY_BUDGET)" ] || set_env_var ONCHAIN_FREE_LOGS_DAILY_BUDGET "90000"
  [ -n "$(env_value ONCHAIN_LOG_PAGE_SIZE)" ] || set_env_var ONCHAIN_LOG_PAGE_SIZE "1000"
  [ -n "$(env_value ONCHAIN_EARLY_WINDOW_BLOCKS)" ] || set_env_var ONCHAIN_EARLY_WINDOW_BLOCKS "100000"
  [ -n "$(env_value TZ)" ] || set_env_var TZ "Asia/Shanghai"
  if [ "$(env_value DB_TYPE | tr '[:upper:]' '[:lower:]')" != "sqlite" ] || [ "$(env_value DB_PATH)" != "data/data.db" ]; then
    log "Using SQLite runtime database: data/data.db"
  fi
  set_env_var DB_TYPE "sqlite"
  set_env_var DB_PATH "data/data.db"
  [ -n "$(env_value TRANSPORT_ENCRYPTION)" ] || set_env_var TRANSPORT_ENCRYPTION "false"
  unset_env_var ONCHAINHTTPPROXY
  local deploy_proxy=""
  if [ -n "${ONCHAIN_HTTP_PROXY:-}" ]; then
    deploy_proxy="$ONCHAIN_HTTP_PROXY"
  elif [ -n "${DEPLOY_ONCHAIN_HTTP_PROXY:-}" ]; then
    deploy_proxy="$DEPLOY_ONCHAIN_HTTP_PROXY"
  else
    deploy_proxy="$(env_value ONCHAIN_HTTP_PROXY)"
  fi
  if [ -n "$deploy_proxy" ]; then
    set_env_var HTTP_PROXY "$deploy_proxy"
    set_env_var HTTPS_PROXY "$deploy_proxy"
    set_env_var ALL_PROXY "$deploy_proxy"
    set_env_var http_proxy "$deploy_proxy"
    set_env_var https_proxy "$deploy_proxy"
    set_env_var all_proxy "$deploy_proxy"
  fi
  if [ -n "${MARKET_HTTP_PROXY:-}" ]; then
    set_env_var MARKET_HTTP_PROXY "$MARKET_HTTP_PROXY"
  elif [ -n "${DEPLOY_MARKET_HTTP_PROXY:-}" ]; then
    set_env_var MARKET_HTTP_PROXY "$DEPLOY_MARKET_HTTP_PROXY"
  elif [ -n "$deploy_proxy" ] && [ -z "$(env_value MARKET_HTTP_PROXY)" ]; then
    set_env_var MARKET_HTTP_PROXY "$deploy_proxy"
  fi
  if [ -s data/proxy/sing-box.json ]; then
    local fixed_proxy
    fixed_proxy="$(env_value NOFX_FIXED_PROXY_URL)"
    fixed_proxy="${fixed_proxy:-http://nofx-proxy:18080}"
    log "Using Docker-local fixed egress proxy"
    set_env_var NOFX_FIXED_PROXY_URL "$fixed_proxy"
    set_env_var HTTP_PROXY "$fixed_proxy"
    set_env_var HTTPS_PROXY "$fixed_proxy"
    set_env_var ALL_PROXY "$fixed_proxy"
    set_env_var http_proxy "$fixed_proxy"
    set_env_var https_proxy "$fixed_proxy"
    set_env_var all_proxy "$fixed_proxy"
    set_env_var MARKET_HTTP_PROXY "$fixed_proxy"
    set_env_var ONCHAIN_ARCHIVE_HTTP_PROXY "$fixed_proxy"
  fi
  unset_env_var ONCHAIN_HTTP_PROXY
  if [ -n "${ONCHAIN_BSC_ARCHIVE_RPC_URL:-}" ]; then
    set_env_var ONCHAIN_BSC_ARCHIVE_RPC_URL "$ONCHAIN_BSC_ARCHIVE_RPC_URL"
  elif [ -n "${DEPLOY_ONCHAIN_BSC_ARCHIVE_RPC_URL:-}" ]; then
    set_env_var ONCHAIN_BSC_ARCHIVE_RPC_URL "$DEPLOY_ONCHAIN_BSC_ARCHIVE_RPC_URL"
  fi
  ensure_csv_env_values NO_PROXY localhost 127.0.0.1 ::1 nofx nofx-frontend nofx-data-gateway nofx-onchain-indexer nofx-proxy
  ensure_csv_env_values no_proxy localhost 127.0.0.1 ::1 nofx nofx-frontend nofx-data-gateway nofx-onchain-indexer nofx-proxy

  if [ -z "$(env_value JWT_SECRET)" ]; then
    set_env_var JWT_SECRET "$(openssl rand -base64 32)"
  fi
  if [ -z "$(env_value DATA_ENCRYPTION_KEY)" ]; then
    set_env_var DATA_ENCRYPTION_KEY "$(openssl rand -base64 32)"
  fi
  if ! grep -q "^RSA_PRIVATE_KEY=" .env 2>/dev/null; then
    log "Generating RSA_PRIVATE_KEY"
    local rsa_key
    rsa_key="$(generate_rsa_private_key_env)"
    set_env_var RSA_PRIVATE_KEY "\"${rsa_key}\""
  fi
}

rsa_key_is_valid() {
  local key="${1:-}"
  [ -n "$key" ] || return 1
  printf '%b' "$key" | openssl rsa -check -noout >/dev/null 2>&1
}

generate_rsa_private_key_env() {
  openssl genrsa 2048 2>/dev/null | awk '{printf "%s\\n", $0}'
}

remove_conflicting_containers() {
  local name
  for name in nofx-data-gateway nofx-onchain-indexer nofx-trading nofx-frontend nofx-proxy nofx-build-proxy; do
    if docker ps -a --format '{{.Names}}' | grep -qx "$name"; then
      log "Removing existing container: ${name}"
      docker rm -f "$name" >/dev/null 2>&1 || true
    fi
  done
}

cleanup_build_proxy() {
  docker rm -f nofx-build-proxy >/dev/null 2>&1 || true
}

build_services_through_fixed_proxy() {
  local bridge_gateway build_proxy
  bridge_gateway="$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}')"
  [ -n "$bridge_gateway" ] || fail "cannot determine Docker bridge gateway"
  build_proxy="http://${bridge_gateway}:18081"

  cleanup_build_proxy
  docker run -d --name nofx-build-proxy --restart=no \
    --network nofx-network \
    -p "${bridge_gateway}:18081:18080" \
    -v "$APP_DIR/data/proxy/sing-box.json:/etc/sing-box/config.json:ro" \
    ghcr.io/sagernet/sing-box:latest run -c /etc/sing-box/config.json >/dev/null
  trap cleanup_build_proxy EXIT

  local i
  for i in $(seq 1 20); do
    if curl -fsS --max-time 5 --proxy "$build_proxy" https://ifconfig.me/ip >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done
  curl -fsS --max-time 10 --proxy "$build_proxy" https://ifconfig.me/ip >/dev/null || fail "temporary build proxy is not ready"

  export HTTP_PROXY="$build_proxy" HTTPS_PROXY="$build_proxy" ALL_PROXY="$build_proxy"
  export http_proxy="$build_proxy" https_proxy="$build_proxy" all_proxy="$build_proxy"
  "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" build \
    --build-arg GO_VERSION=docker.io/library/golang:1.25-alpine \
    --build-arg ALPINE_VERSION=docker.io/library/alpine:3.20 \
    nofx-data-gateway nofx-onchain-indexer nofx
  "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" build \
    --build-arg NODE_VERSION=docker.io/library/node:20-alpine \
    --build-arg NGINX_VERSION=docker.io/library/nginx:alpine \
    nofx-frontend
  clear_compose_proxy_environment
  cleanup_build_proxy
  trap - EXIT
}

wait_fixed_proxy() {
  local port expected_ip actual_ip status i
  port="$(env_value NOFX_PROXY_PORT)"
  port="${port:-18080}"
  expected_ip="$(env_value NOFX_PROXY_EXPECTED_IP)"

  actual_ip=""
  for i in $(seq 1 15); do
    actual_ip="$(env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
      curl -4 -fsS --max-time 10 --proxy "http://127.0.0.1:${port}" https://ifconfig.me/ip 2>/dev/null || true)"
    [ -n "$actual_ip" ] && break
    sleep 2
  done
  [ -n "$actual_ip" ] || fail "fixed proxy did not become ready"
  if [ -n "$expected_ip" ] && [ "$actual_ip" != "$expected_ip" ]; then
    fail "fixed proxy exit mismatch: expected ${expected_ip}, got ${actual_ip}"
  fi

  status="$(env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
    curl -4 -sS --max-time 20 --proxy "http://127.0.0.1:${port}" \
    -o /tmp/nofx-binance-proxy-smoke.json -w '%{http_code}' \
    https://fapi.binance.com/fapi/v1/time || true)"
  [ "$status" = "200" ] || fail "fixed proxy cannot reach Binance Futures API (HTTP ${status:-000})"
  log "Fixed proxy is ready: exit=${actual_ip}, Binance Futures HTTP 200"
}

wait_http() {
  local name="$1"
  local url="$2"
  local max="${3:-60}"
  local i
  for i in $(seq 1 "$max"); do
    if curl -fsS --max-time 5 "$url" >/tmp/nofx-health-body 2>/tmp/nofx-health-error; then
      log "${name} is ready: ${url}"
      return 0
    fi
    sleep 2
  done
  log "${name} health body:"
  cat /tmp/nofx-health-body 2>/dev/null || true
  log "${name} curl error:"
  cat /tmp/nofx-health-error 2>/dev/null || true
  return 1
}

main() {
  [ -d "$APP_DIR" ] || fail "APP_DIR does not exist: ${APP_DIR}"
  cd "$APP_DIR"
  [ -f "$COMPOSE_FILE" ] || fail "compose file not found: ${APP_DIR}/${COMPOSE_FILE}"

  command -v docker >/dev/null 2>&1 || fail "Docker is not installed"
  docker info >/dev/null 2>&1 || fail "Docker daemon is not running"
  command -v curl >/dev/null 2>&1 || fail "curl is not installed"
  command -v openssl >/dev/null 2>&1 || fail "openssl is not installed"
  detect_compose

  install -m 700 -d data
  inherit_legacy_runtime
  ensure_env
  clear_compose_proxy_environment
  configure_compose_files

  log "Using app dir: ${APP_DIR}"
  log "Using compose file: ${COMPOSE_FILE}"
  log "Creating docker network if missing"
  docker network inspect nofx-network >/dev/null 2>&1 || docker network create nofx-network >/dev/null

  remove_conflicting_containers

  if [ "$PROXY_ENABLED" = "1" ]; then
    log "Starting fixed egress proxy"
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" up -d nofx-proxy
    wait_fixed_proxy
    log "Building services through fixed egress proxy"
    build_services_through_fixed_proxy
  fi

  log "Building and starting services"
  if [ "$PROXY_ENABLED" = "1" ]; then
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" up -d nofx-data-gateway nofx-onchain-indexer nofx nofx-frontend
  else
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" up -d --build nofx-data-gateway nofx-onchain-indexer nofx nofx-frontend
  fi

  local backend_port frontend_port gateway_port
  backend_port="$(env_value NOFX_BACKEND_PORT)"
  frontend_port="$(env_value NOFX_FRONTEND_PORT)"
  gateway_port="$(published_host_port "$(env_value DATA_GATEWAY_PORT)" "8090")"

  wait_http "data gateway" "http://127.0.0.1:${gateway_port:-8090}/health" 90 || {
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" logs --tail=120 nofx-data-gateway
    fail "data gateway is not healthy"
  }

  "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" exec -T nofx-onchain-indexer wget -q -O - http://127.0.0.1:8091/health >/tmp/nofx-onchain-indexer-health.json || {
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" logs --tail=160 nofx-onchain-indexer
    fail "onchain indexer is not healthy"
  }

  wait_http "backend" "http://127.0.0.1:${backend_port:-8080}/api/health" 90 || {
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" logs --tail=160 nofx
    fail "backend is not healthy"
  }

  wait_http "frontend" "http://127.0.0.1:${frontend_port:-3000}/health" 60 || {
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" logs --tail=120 nofx-frontend
    fail "frontend is not healthy"
  }

  log "Checking backend data-gateway proxy"
  curl -fsS --max-time 10 "http://127.0.0.1:${backend_port:-8080}/api/data-gateway/health" >/tmp/nofx-data-gateway-proxy.json || {
    "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" logs --tail=120 nofx
    fail "backend data-gateway proxy failed"
  }

  log "Deployment complete"
  "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" ps
  if [ -n "${ONCHAIN_SMOKE_ADDRESS:-}" ]; then
    log "Checking on-chain recent analysis smoke test"
    curl -fsS --max-time 45 "http://127.0.0.1:${backend_port:-8080}/api/onchain/token-analysis?chain=${ONCHAIN_SMOKE_CHAIN:-bsc}&address=${ONCHAIN_SMOKE_ADDRESS}&depth=recent" >/tmp/nofx-onchain-smoke.json || {
      "${COMPOSE_CMD[@]}" "${COMPOSE_ARGS[@]}" logs --tail=120 nofx
      fail "on-chain smoke test failed"
    }
    log "on-chain smoke test passed"
  fi
  log "Frontend: http://SERVER_IP:${frontend_port:-3000}"
  log "Backend:  http://SERVER_IP:${backend_port:-8080}"
  log "Gateway:  http://SERVER_IP:${gateway_port:-8090}/health"
}

main "$@"
