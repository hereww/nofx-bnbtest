#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/www/wwwroot/nofx}"
cd "$APP_DIR"

if docker compose version >/dev/null 2>&1; then
  COMPOSE_CMD=(docker compose)
else
  COMPOSE_CMD=(docker-compose)
fi

unset HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY
unset http_proxy https_proxy all_proxy no_proxy

running_before="$(sqlite3 data/data.db 'SELECT COUNT(*) FROM traders WHERE is_running=1;')"

docker rm -f nofx-build-proxy >/dev/null 2>&1 || true
docker run -d --name nofx-build-proxy --restart=no \
  --network nofx-network \
  -p 172.17.0.1:18081:18080 \
  -v "$APP_DIR/data/proxy/sing-box.json:/etc/sing-box/config.json:ro" \
  ghcr.io/sagernet/sing-box:latest run -c /etc/sing-box/config.json >/dev/null
trap 'docker rm -f nofx-build-proxy >/dev/null 2>&1 || true' EXIT

for _ in $(seq 1 20); do
  if curl -fsS --max-time 5 --proxy http://172.17.0.1:18081 https://ifconfig.me/ip >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
curl -fsS --max-time 10 --proxy http://172.17.0.1:18081 https://ifconfig.me/ip >/dev/null

export HTTP_PROXY=http://172.17.0.1:18081
export HTTPS_PROXY=http://172.17.0.1:18081
export ALL_PROXY=http://172.17.0.1:18081
export http_proxy=http://172.17.0.1:18081
export https_proxy=http://172.17.0.1:18081
export all_proxy=http://172.17.0.1:18081
"${COMPOSE_CMD[@]}" -f docker-compose.yml -f docker-compose.proxy.yml build \
  --build-arg GO_VERSION=docker.io/library/golang:1.25-alpine \
  --build-arg ALPINE_VERSION=docker.io/library/alpine:3.20 \
  nofx

unset HTTP_PROXY HTTPS_PROXY ALL_PROXY
unset http_proxy https_proxy all_proxy
docker rm -f nofx-build-proxy >/dev/null 2>&1 || true
"${COMPOSE_CMD[@]}" -f docker-compose.yml -f docker-compose.proxy.yml up -d --force-recreate nofx

for _ in $(seq 1 60); do
  if curl -fsS --max-time 5 http://127.0.0.1:8080/api/health >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
curl -fsS --max-time 5 http://127.0.0.1:8080/api/health

running_after="$(sqlite3 data/data.db 'SELECT COUNT(*) FROM traders WHERE is_running=1;')"
printf '\nrunning_before=%s\nrunning_after=%s\n' "$running_before" "$running_after"
