#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

SERVER_USER="${SERVER_USER:-root}"
SERVER_HOST="${SERVER_HOST:-8.217.14.206}"
SERVER="${SERVER_USER}@${SERVER_HOST}"
REMOTE_DIR="${REMOTE_DIR:-/mnt/man_v2}"
CONTAINER_NAME="${CONTAINER_NAME:-man-indexer-v2}"
DOMAIN="${DOMAIN:-manapi.metaid.io}"
HEALTH_PATH="${HEALTH_PATH:-/debug/count}"
GOOS_TARGET="${GOOS_TARGET:-linux}"
GOARCH_TARGET="${GOARCH_TARGET:-amd64}"
DIST_DIR="${DIST_DIR:-${REPO_ROOT}/dist}"
BUILD_OUTPUT="${BUILD_OUTPUT:-${DIST_DIR}/manindexer-${GOOS_TARGET}-${GOARCH_TARGET}}"
REMOTE_TMP="/tmp/manindexer.${GOOS_TARGET}.${GOARCH_TARGET}.$$"
SKIP_PUBLIC_CHECK="${SKIP_PUBLIC_CHECK:-0}"

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  else
    shasum -a 256 "$file" | awk '{print $1}'
  fi
}

printf '[deploy] repo: %s\n' "$REPO_ROOT"
printf '[deploy] target: %s (%s)\n' "$SERVER" "$REMOTE_DIR"

mkdir -p "$DIST_DIR"
cd "$REPO_ROOT"
printf '[deploy] building manindexer (%s/%s)\n' "$GOOS_TARGET" "$GOARCH_TARGET"
CGO_ENABLED=0 GOOS="$GOOS_TARGET" GOARCH="$GOARCH_TARGET" \
  go build -trimpath -ldflags="-s -w" -o "$BUILD_OUTPUT" app.go

LOCAL_SHA="$(sha256_file "$BUILD_OUTPUT")"
printf '[deploy] local sha256: %s\n' "$LOCAL_SHA"

printf '[deploy] uploading binary to %s:%s\n' "$SERVER" "$REMOTE_TMP"
scp "$BUILD_OUTPUT" "${SERVER}:${REMOTE_TMP}"

printf '[deploy] replacing binary and restarting container\n'
ssh "$SERVER" \
  "REMOTE_DIR='${REMOTE_DIR}' CONTAINER_NAME='${CONTAINER_NAME}' REMOTE_TMP='${REMOTE_TMP}' HEALTH_PATH='${HEALTH_PATH}' LOCAL_SHA='${LOCAL_SHA}' bash -s" <<'EOSSH'
set -euo pipefail

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  else
    shasum -a 256 "$file" | awk '{print $1}'
  fi
}

cd "$REMOTE_DIR"
if [[ ! -f manindexer ]]; then
  echo "[deploy][remote] /mnt/man_v2/manindexer does not exist"
  exit 1
fi

install -m 755 "$REMOTE_TMP" manindexer.new
BACKUP="manindexer.bak.$(date +%y%m%d_%H%M%S)"
cp -a manindexer "$BACKUP"
mv -f manindexer.new manindexer
rm -f "$REMOTE_TMP"

REMOTE_SHA="$(sha256_file manindexer)"
echo "[deploy][remote] backup: $BACKUP"
echo "[deploy][remote] sha256: $REMOTE_SHA"
if [[ "$REMOTE_SHA" != "$LOCAL_SHA" ]]; then
  echo "[deploy][remote] sha256 mismatch"
  exit 1
fi

docker restart "$CONTAINER_NAME" >/dev/null
ok=0
for _ in $(seq 1 45); do
  if curl -fsS --max-time 3 "http://127.0.0.1:7777${HEALTH_PATH}" >/dev/null; then
    ok=1
    break
  fi
  sleep 2
done
if [[ "$ok" -ne 1 ]]; then
  echo "[deploy][remote] health check failed after restart"
  docker logs --tail 80 "$CONTAINER_NAME" || true
  exit 1
fi

echo "[deploy][remote] container status:"
docker ps --filter "name=${CONTAINER_NAME}" --format 'table {{.Names}}\t{{.Status}}'
echo "[deploy][remote] local health:"
curl -fsS "http://127.0.0.1:7777${HEALTH_PATH}"
echo
EOSSH

if [[ "$SKIP_PUBLIC_CHECK" == "1" ]]; then
  printf '[deploy] skipped public checks (SKIP_PUBLIC_CHECK=1)\n'
else
  printf '[deploy] public health: https://%s%s\n' "$DOMAIN" "$HEALTH_PATH"
  curl -fsS --max-time 10 "https://${DOMAIN}${HEALTH_PATH}"
  echo
fi

printf '[deploy] done\n'
