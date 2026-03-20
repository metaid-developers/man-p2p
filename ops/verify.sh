#!/usr/bin/env bash
set -euo pipefail

SERVER_USER="${SERVER_USER:-root}"
SERVER_HOST="${SERVER_HOST:-8.217.14.206}"
SERVER="${SERVER_USER}@${SERVER_HOST}"
CONTAINER_NAME="${CONTAINER_NAME:-man-indexer-v2}"
DOMAIN="${DOMAIN:-manapi.metaid.io}"
HEALTH_PATH="${HEALTH_PATH:-/debug/count}"
CHECK_PATH_QUERY="${CHECK_PATH_QUERY:-/pin/path/list?cursor=0&size=1&path=%2Fprotocols%2Fmetaapp}"
CHECK_NEW_PATH_QUERY="${CHECK_NEW_PATH_QUERY:-/api/pin/path/list?cursor=0&size=1&path=%2Fprotocols%2Fmetaapp}"

preview_json() {
  local url="$1"
  local body
  body="$(curl -fsS --max-time 12 "$url")"
  printf '%s\n' "${body:0:200}"
}

printf '[verify] remote container status\n'
ssh "$SERVER" "docker ps --filter name='${CONTAINER_NAME}' --format 'table {{.Names}}\t{{.Status}}'"

printf '[verify] remote runtime details\n'
ssh "$SERVER" "docker inspect -f 'status={{.State.Status}} restart={{.RestartCount}} started={{.State.StartedAt}} net={{.HostConfig.NetworkMode}}' ${CONTAINER_NAME}"

printf '[verify] remote local health: http://127.0.0.1:7777%s\n' "$HEALTH_PATH"
ssh "$SERVER" "curl -fsS --max-time 8 'http://127.0.0.1:7777${HEALTH_PATH}'"
echo

printf '[verify] public health: https://%s%s\n' "$DOMAIN" "$HEALTH_PATH"
curl -fsS --max-time 12 "https://${DOMAIN}${HEALTH_PATH}"
echo

printf '[verify] old API compatibility: https://%s%s\n' "$DOMAIN" "$CHECK_PATH_QUERY"
preview_json "https://${DOMAIN}${CHECK_PATH_QUERY}"
echo

printf '[verify] new API path: https://%s%s\n' "$DOMAIN" "$CHECK_NEW_PATH_QUERY"
preview_json "https://${DOMAIN}${CHECK_NEW_PATH_QUERY}"
echo

printf '[verify] sort check (desc)\n'
preview_json "https://${DOMAIN}/pin/list?page=2&size=20&sortBy=timestamp&order=desc"
echo

printf '[verify] sort check (asc)\n'
preview_json "https://${DOMAIN}/pin/list?page=2&size=20&sortBy=timestamp&order=asc"
echo

printf '[verify] done\n'
