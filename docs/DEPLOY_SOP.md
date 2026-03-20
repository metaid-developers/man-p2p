# man-v2 Production Deployment SOP

## 1. Purpose
This SOP defines a repeatable release process for `man-v2` to avoid:
- accidental config mistakes
- broken API path compatibility
- non-recoverable release failures

Target environment defaults:
- Server: `8.217.14.206`
- Remote dir: `/mnt/man_v2`
- Container: `man-indexer-v2`
- Public domain: `manapi.metaid.io`

## 2. Files
- Deploy: `ops/deploy.sh`
- Verify: `ops/verify.sh`
- Rollback: `ops/rollback.sh`

## 3. Prerequisites
- Local machine:
  - `go` available
  - `ssh` and `scp` available
  - network access to `root@8.217.14.206`
- Remote server:
  - `docker` running
  - container `man-indexer-v2` exists
  - `/mnt/man_v2/manindexer` exists
  - nginx configured for `manapi.metaid.io`

## 4. One-Time Setup
Run once in repo root:

```bash
chmod +x ops/deploy.sh ops/verify.sh ops/rollback.sh
```

## 5. Standard Release Flow

### Step 1: Optional pre-check
```bash
./ops/verify.sh
```

### Step 2: Deploy
```bash
./ops/deploy.sh
```

What `deploy.sh` does:
- build Linux binary (`CGO_ENABLED=0`, `GOOS=linux`, `GOARCH=amd64`)
- upload binary to server
- backup current `/mnt/man_v2/manindexer` as `manindexer.bak.<timestamp>`
- replace binary
- restart `man-indexer-v2`
- wait for local health endpoint to recover
- verify public health endpoint

### Step 3: Post-deploy verify
```bash
./ops/verify.sh
```

Acceptance criteria:
- no `502/404` on critical endpoints
- both old and new API paths return success JSON:
  - old style: `/pin/path/list?...`
  - new style: `/api/pin/path/list?...`

## 6. Rollback Procedure
If release causes failures:

```bash
./ops/rollback.sh
```

What `rollback.sh` does:
- select latest `manindexer.bak.*` (or specific backup if provided)
- restore binary
- restart container
- wait for local health recovery
- verify public health endpoint

Rollback to a specific backup:

```bash
ROLLBACK_BACKUP=manindexer.bak.260310_035012 ./ops/rollback.sh
```

## 7. Environment Overrides
All scripts support environment variable overrides:

```bash
SERVER_USER=root \
SERVER_HOST=8.217.14.206 \
REMOTE_DIR=/mnt/man_v2 \
CONTAINER_NAME=man-indexer-v2 \
DOMAIN=manapi.metaid.io \
./ops/deploy.sh
```

Optional deploy overrides:
- `SKIP_PUBLIC_CHECK=1` skip public curl checks
- `GOARCH_TARGET=arm64` for ARM target hosts

## 8. Emergency Troubleshooting
Check remote service status:

```bash
ssh root@8.217.14.206 "docker ps --filter name=man-indexer-v2"
ssh root@8.217.14.206 "docker logs --tail 120 man-indexer-v2"
ssh root@8.217.14.206 "curl -sS http://127.0.0.1:7777/debug/count"
```

Check nginx and reload:

```bash
ssh root@8.217.14.206 "nginx -t && systemctl reload nginx"
```

## 9. Security Rules
- Do not commit real credentials to Git.
- Keep production credentials only on server-side private files/env vars.
- Commit only source, scripts, and docs.
- Validate path compatibility (`/pin/*` and `/api/pin/*`) after every release.
