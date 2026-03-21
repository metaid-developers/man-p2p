# IDBots man-p2p Alpha Contract

**Date:** 2026-03-21  
**Project:** man-p2p + IDBots  
**Status:** Active Alpha integration contract

---

## 1. Purpose

This document defines the local-first contract that IDBots should use when talking to `man-p2p` during Alpha.

The goal is simple:

- try local `man-p2p` first
- if local data is missing or local service is unavailable, fall back to centralized services

Alpha does not require complete local history.

---

## 2. Local Endpoints

IDBots Alpha may rely on these local endpoints:

- `GET /health`
- `POST /api/config/reload`
- `GET /api/p2p/status`
- `GET /api/p2p/peers`
- `GET /api/pin/{pinId}`
- `GET /api/pin/path/list?...`
- `GET /api/address/pin/list/{address}?...`
- `GET /api/v1/users/info/metaid/{metaId}`
- `GET /api/v1/users/info/address/{address}`
- `GET /content/{pinId}`

JSON success payloads keep the existing envelope:

```json
{
  "code": 1,
  "message": "ok",
  "data": {}
}
```

---

## 3. Hit And Miss Semantics

### 3.1 JSON API Hit

Treat the response as a local hit only when:

- HTTP status is `2xx`
- envelope `code` is `1`

### 3.2 JSON API Miss

Treat the response as a local miss when:

- HTTP status is non-`2xx`

For Alpha, `GET /api/pin/{pinId}` must use this rule. A local miss must not be treated as success.

### 3.3 Content Hit

Treat `GET /content/{pinId}` as a local content hit only when:

- HTTP status is `200`
- response body is non-empty
- header `X-Man-Content-Status` is not `metadata-only`

### 3.4 Content Metadata-Only

Treat the response as a local metadata-only miss when:

- HTTP status is `200`
- header `X-Man-Content-Status` equals `metadata-only`
- response body is empty

In this case, IDBots should keep the local metadata result if it already has it, but fall back to centralized content for the body bytes.

### 3.5 Content Missing Entirely

Treat the response as a local content miss when:

- HTTP status is non-`2xx`

---

## 4. Fallback Rules

IDBots should fall back to centralized services when:

- the local request times out
- the local request fails at transport level
- the local response is non-`2xx`
- content response is `metadata-only`

IDBots should not fall back when:

- local JSON returned `2xx` and `code=1`
- local content returned `200` with real body bytes

---

## 5. Runtime Status Interpretation

`GET /api/p2p/status` is local runtime truth only.

Important fields:

- `syncMode`
  - `self`, `selective`, or `full`
- `runtimeMode`
  - `chain-enabled` means blockchain ingestion is active
  - `p2p-only` means the node is only serving local data and P2P-received data
- `peerCount`
- `peerId`
- `listenAddrs`
- `storageLimitReached`
- `storageUsedBytes`

`syncProgress` is not a completeness guarantee during Alpha.

---

## 6. Config Reload Semantics

`POST /api/config/reload` hot reloads Alpha-critical filtering state:

- sync preset
- own addresses
- allow/block filters
- max content size
- storage limit
- chain-source mode flag as reported status

Alpha does not guarantee full live reconfiguration of network topology such as relay or bootstrap behavior without restart.

---

## 7. Current Alpha Limitations

IDBots should assume:

- realtime P2P propagation is the main decentralized success path
- local storage may be partial
- historical completeness still depends on fallback unless local data is already present
- metadata-only content is expected behavior for oversized PINs

---

## 8. Verification Commands

Current Alpha gate command:

```bash
make alpha-test
```

This command validates the local API miss contract, P2P status contract, P2P receive path, and the P2P-only runtime mode.
