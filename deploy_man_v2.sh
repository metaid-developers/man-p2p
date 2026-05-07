#!/usr/bin/env bash
set -euo pipefail

SERVER="root@8.217.14.206"
REMOTE_DIR="/mnt/man_v2"
CONTAINER="man-indexer-v2"
LOCAL_BIN="manindexer-linux-amd64"
REMOTE_TMP="/tmp/${LOCAL_BIN}"
HEALTH_URL="http://127.0.0.1:7777/debug/count"
DARWIN_LINUX_AMD64_CC="${DARWIN_LINUX_AMD64_CC:-x86_64-unknown-linux-gnu-gcc}"
DARWIN_LINUX_AMD64_CXX="${DARWIN_LINUX_AMD64_CXX:-x86_64-unknown-linux-gnu-g++}"
DARWIN_LINUX_AMD64_CGO_LDFLAGS="${DARWIN_LINUX_AMD64_CGO_LDFLAGS:--L/usr/local/x86_64-linux/lib -lzmq}"

verify_zmq_support() {
	local file="$1"
	if ! command -v strings >/dev/null 2>&1; then
		echo "[build] strings command is required to verify zmq support"
		exit 1
	fi
	if strings "$file" | grep -q 'zmq_stub.go'; then
		echo "[build] refusing to deploy a stub-only binary: found zmq_stub.go in $file"
		exit 1
	fi
	if ! strings "$file" | grep -q 'github.com/pebbe/zmq4'; then
		echo "[build] refusing to deploy binary without zmq support: github.com/pebbe/zmq4 not found in $file"
		exit 1
	fi
}

build_binary() {
	if [[ "$(uname -s)" == "Darwin" ]]; then
		GOOS=linux GOARCH=amd64 \
		CC="$DARWIN_LINUX_AMD64_CC" \
		CXX="$DARWIN_LINUX_AMD64_CXX" \
		CGO_LDFLAGS="$DARWIN_LINUX_AMD64_CGO_LDFLAGS" \
		CGO_ENABLED=1 \
		go build -trimpath -ldflags="-s -w" -o "$LOCAL_BIN" .
		return
	fi

	GOOS=linux GOARCH=amd64 \
	CGO_ENABLED=1 \
	go build -trimpath -ldflags="-s -w" -o "$LOCAL_BIN" .
}

echo "[1/6] build binary"
build_binary
verify_zmq_support "$LOCAL_BIN"

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
