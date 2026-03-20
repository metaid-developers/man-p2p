#!/usr/bin/env bash
set -euo pipefail

SERVER_USER="${SERVER_USER:-root}"
SERVER_HOST="${SERVER_HOST:-8.217.14.206}"
SERVER="${SERVER_USER}@${SERVER_HOST}"
REMOTE_DIR="${REMOTE_DIR:-/mnt/man_v2}"
CONTAINER_NAME="${CONTAINER_NAME:-man-indexer-v2}"
DOMAIN="${DOMAIN:-manapi.metaid.io}"
HEALTH_PATH="${HEALTH_PATH:-/debug/count}"
ROLLBACK_BACKUP="${ROLLBACK_BACKUP:-}"

printf '[rollback] target: %s (%s)\n' "$SERVER" "$REMOTE_DIR"

ssh "$SERVER" \
  "REMOTE_DIR='${REMOTE_DIR}' CONTAINER_NAME='${CONTAINER_NAME}' HEALTH_PATH='${HEALTH_PATH}' ROLLBACK_BACKUP='${ROLLBACK_BACKUP}' bash -s" <<'EOSSH'
set -euo pipefail

cd "$REMOTE_DIR"

if [[ -n "${ROLLBACK_BACKUP}" ]]; then
  BACKUP_FILE="${ROLLBACK_BACKUP}"
else
  BACKUP_FILE="$(ls -1t manindexer.bak.* 2>/dev/null | head -n 1 || true)"
fi

if [[ -z "${BACKUP_FILE}" || ! -f "${BACKUP_FILE}" ]]; then
  echo "[rollback][remote] no backup file found"
  exit 1
fi

FAILED_COPY="manindexer.failed.$(date +%y%m%d_%H%M%S)"
cp -a manindexer "${FAILED_COPY}"
cp -a "${BACKUP_FILE}" manindexer
chmod 755 manindexer

echo "[rollback][remote] restored: ${BACKUP_FILE}"
echo "[rollback][remote] failed-copy: ${FAILED_COPY}"

docker restart "${CONTAINER_NAME}" >/dev/null

ok=0
for _ in $(seq 1 45); do
  if curl -fsS --max-time 3 "http://127.0.0.1:7777${HEALTH_PATH}" >/dev/null; then
    ok=1
    break
  fi
  sleep 2
done
if [[ "${ok}" -ne 1 ]]; then
  echo "[rollback][remote] health check failed after rollback"
  docker logs --tail 80 "${CONTAINER_NAME}" || true
  exit 1
fi

docker ps --filter "name=${CONTAINER_NAME}" --format 'table {{.Names}}\t{{.Status}}'
curl -fsS "http://127.0.0.1:7777${HEALTH_PATH}"
echo
EOSSH

printf '[rollback] public health: https://%s%s\n' "$DOMAIN" "$HEALTH_PATH"
curl -fsS --max-time 12 "https://${DOMAIN}${HEALTH_PATH}"
echo

printf '[rollback] done\n'
