#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${1:-/www/wwwroot/nofx}"
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"

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

set_env_var() {
  local key="$1"
  local value="$2"
  if grep -q "^${key}=" .env 2>/dev/null; then
    sed -i "s|^${key}=.*|${key}=${value}|" .env
  else
    printf '%s=%s\n' "$key" "$value" >> .env
  fi
}

env_value() {
  local key="$1"
  grep "^${key}=" .env 2>/dev/null | tail -1 | cut -d= -f2- | tr -d '"' || true
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
  [ -n "$(env_value TZ)" ] || set_env_var TZ "Asia/Shanghai"
  [ -n "$(env_value DB_TYPE)" ] || set_env_var DB_TYPE "sqlite"
  [ -n "$(env_value DB_PATH)" ] || set_env_var DB_PATH "data/data.db"
  [ -n "$(env_value TRANSPORT_ENCRYPTION)" ] || set_env_var TRANSPORT_ENCRYPTION "false"
  ensure_csv_env_values NO_PROXY localhost 127.0.0.1 ::1 nofx nofx-frontend nofx-data-gateway
  ensure_csv_env_values no_proxy localhost 127.0.0.1 ::1 nofx nofx-frontend nofx-data-gateway

  if [ -z "$(env_value JWT_SECRET)" ]; then
    set_env_var JWT_SECRET "$(openssl rand -base64 32)"
  fi
  if [ -z "$(env_value DATA_ENCRYPTION_KEY)" ]; then
    set_env_var DATA_ENCRYPTION_KEY "$(openssl rand -base64 32)"
  fi
  if [ -z "$(env_value RSA_PRIVATE_KEY)" ]; then
    local rsa_key
    rsa_key="$(openssl genrsa 2048 2>/dev/null | awk '{printf "%s\\\\n", $0}')"
    set_env_var RSA_PRIVATE_KEY "\"${rsa_key}\""
  fi
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
  ensure_env

  log "Using app dir: ${APP_DIR}"
  log "Using compose file: ${COMPOSE_FILE}"
  log "Creating docker network if missing"
  docker network inspect nofx-network >/dev/null 2>&1 || docker network create nofx-network >/dev/null

  log "Building and starting services"
  "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" up -d --build nofx-data-gateway nofx nofx-frontend

  local backend_port frontend_port gateway_port
  backend_port="$(env_value NOFX_BACKEND_PORT)"
  frontend_port="$(env_value NOFX_FRONTEND_PORT)"
  gateway_port="$(published_host_port "$(env_value DATA_GATEWAY_PORT)" "8090")"

  wait_http "data gateway" "http://127.0.0.1:${gateway_port:-8090}/health" 90 || {
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" logs --tail=120 nofx-data-gateway
    fail "data gateway is not healthy"
  }

  wait_http "backend" "http://127.0.0.1:${backend_port:-8080}/api/health" 90 || {
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" logs --tail=160 nofx
    fail "backend is not healthy"
  }

  wait_http "frontend" "http://127.0.0.1:${frontend_port:-3000}/health" 60 || {
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" logs --tail=120 nofx-frontend
    fail "frontend is not healthy"
  }

  log "Checking backend data-gateway proxy"
  curl -fsS --max-time 10 "http://127.0.0.1:${backend_port:-8080}/api/data-gateway/health" >/tmp/nofx-data-gateway-proxy.json || {
    "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" logs --tail=120 nofx
    fail "backend data-gateway proxy failed"
  }

  log "Deployment complete"
  "${COMPOSE_CMD[@]}" -f "$COMPOSE_FILE" ps
  log "Frontend: http://SERVER_IP:${frontend_port:-3000}"
  log "Backend:  http://SERVER_IP:${backend_port:-8080}"
  log "Gateway:  http://SERVER_IP:${gateway_port:-8090}/health"
}

main "$@"
