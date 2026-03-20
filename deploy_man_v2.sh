#!/usr/bin/env bash
set -euo pipefail

SERVER="root@8.217.14.206"
REMOTE_DIR="/mnt/man_v2"
CONTAINER="man-indexer-v2"
LOCAL_BIN="manindexer-linux-amd64"
REMOTE_TMP="/tmp/${LOCAL_BIN}"
HEALTH_URL="http://127.0.0.1:7777/debug/count"

echo "[1/6] build binary"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "$LOCAL_BIN" app.go

echo "[2/6] local checksum"
sha256sum "$LOCAL_BIN"

echo "[3/6] upload to server"
scp "$LOCAL_BIN" "${SERVER}:${REMOTE_TMP}"

echo "[4/6] backup + replace binary on server"
ssh "$SERVER" bash -s -- "$REMOTE_DIR" "$REMOTE_TMP" <<'EOSSH'
set -euo pipefail
REMOTE_DIR="$1"
REMOTE_TMP="$2"
cd "$REMOTE_DIR"
install -m 755 "$REMOTE_TMP" manindexer.new
cp -a manindexer "manindexer.bak.$(date +%y%m%d_%H%M%S)"
mv -f manindexer.new manindexer
rm -f "$REMOTE_TMP"
ls -lt manindexer manindexer.bak.* | head -n 5
EOSSH

echo "[5/6] restart container"
ssh "$SERVER" "docker restart ${CONTAINER} >/dev/null && sleep 3 && docker ps --filter name=${CONTAINER} --format 'table {{.Names}}\t{{.Status}}'"

echo "[6/6] health check + logs"
ssh "$SERVER" "curl -fsS --max-time 8 ${HEALTH_URL}; echo; docker logs --tail 40 ${CONTAINER}"

echo "Deploy finished."
