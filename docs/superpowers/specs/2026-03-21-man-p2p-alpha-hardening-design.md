# man-p2p Alpha Hardening Design

**Date:** 2026-03-21  
**Project:** man-p2p  
**Status:** Approved for implementation

---

## 1. Goal

Prepare `man-p2p` for an IDBots Alpha where:

- IDBots reads local `man-p2p` APIs first
- newly seen PINs can propagate between P2P peers in near real time
- local misses can safely fall back to centralized services
- nodes may run with blockchain RPC enabled or as P2P-only receivers

This design supersedes the earlier unfinished Alpha draft in this repository.

---

## 2. Product Scope

### 2.1 Alpha Success Definition

Alpha is considered ready when all of the following are true:

1. IDBots can call `localhost` first for the required MAN-compatible APIs
2. a node with fresh data can announce a new PIN to another node
3. the receiving node can filter, fetch, and persist the PIN locally
4. when local data is missing, IDBots can reliably detect the miss and fall back

### 2.2 Explicit Non-Goals For This Alpha

The following are not required for this Alpha:

- pure-P2P historical completeness
- DHT-backed provider search for arbitrary historical PIN lookup
- automatic 7-day history catch-up across peers
- MRC20 correctness or balance semantics
- production-grade NAT or relay guarantees

These remain later phases.

---

## 3. Core Product Decision

`self`, `selective`, and `full` are **user-facing presets**, not separate sync engines.

The real system primitive is a composable local receive filter:

- address allow list
- address block list
- path allow list
- path block list
- max content size
- optional own-address list used by the `self` preset

Preset semantics:

- `self`: allow only own addresses, then apply block rules and size rule
- `selective`: allow explicit addresses and paths, then apply block rules and size rule
- `full`: no allowlist restriction, but still apply block rules and size rule

Block rules always win.

---

## 4. Runtime Modes

### 4.1 Chain-Enabled Mode

When blockchain RPC/ZMQ is configured and enabled:

- MAN keeps its inherited blockchain indexing flow
- newly indexed mempool and confirmed PINs are broadcast into P2P
- local Pebble remains the authoritative local store

### 4.2 P2P-Only Mode

When blockchain indexing is disabled:

- `man-p2p` still starts Pebble, HTTP API, libp2p host, gossip, and PIN fetch handlers
- it does not initialize chain adapters
- it does not run blockchain indexing loops
- it only receives data from peers and serves what it has locally

This mode is required for the Alpha because many IDBots users will not have chain RPC access.

---

## 5. Data Flow

### 5.1 Upstream Flow

Source node:

1. sees a PIN via blockchain path or already has it locally
2. persists it to Pebble
3. publishes compact announcement metadata through gossip

Announcement fields must be sufficient for filtering:

- pin id
- path
- address
- metaid
- chain
- timestamp
- confirmed flag
- height
- content size
- sender peer id

### 5.2 Downstream Flow

Target node:

1. receives announcement
2. applies local filter rules
3. rejects if blocked or storage-limited
4. if under size threshold, fetches full PIN from announcing peer and stores it
5. if over size threshold, stores metadata-only record

The Alpha pull path remains peer-directed: fetch from the announcing peer. Historical peer search is deferred.

---

## 6. Local API Contract

The local API must be optimized for **correct fallback behavior**, not only backward-looking MAN compatibility.

Required Alpha endpoints:

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

### 6.1 Miss Semantics

This is the most important Alpha API rule.

- local JSON API miss must not look like a successful hit
- local content miss must not return `200 "fail"`
- metadata-only content must remain distinguishable from missing content

Required behavior:

- JSON miss: return a non-2xx status with an error envelope
- content missing entirely: return non-2xx
- metadata-only content: return `200` with empty body and explicit metadata header, or an equivalent contract that IDBots can reliably interpret as a body miss

This ensures IDBots can fall back safely and deterministically.

### 6.2 Envelope Semantics

For successful JSON responses:

- keep the existing `{ "code": 1, "message": "ok", "data": ... }` envelope

For miss/error responses:

- non-2xx HTTP status is authoritative for fallback
- the JSON body may still use the existing error envelope shape

---

## 7. Configuration Semantics

### 7.1 Configuration Source

Alpha configuration is read from the P2P JSON config file and held in memory.

It must include:

- sync preset
- allow/block address filters
- allow/block path filters
- max content size
- bootstrap nodes
- relay enabled flag
- storage limit
- own address list
- blockchain mode toggle

### 7.2 Reload Semantics

`POST /api/config/reload` only promises hot reload for:

- allow/block filters
- preset mode
- own addresses
- max content size
- storage limit

Network topology config such as relay and bootstrap nodes may be accepted on reload, but does not need to take full effect without restart in this Alpha. Status must be honest about this.

---

## 8. Observability

`GET /api/p2p/status` must report local runtime truth, not fake global progress.

Required fields:

- peer count
- storage used bytes
- storage-limit reached flag
- sync mode
- p2p-only vs chain-enabled mode
- local peer id
- local listen addresses if available

`syncProgress` is optional and must not claim global completeness.

---

## 9. Alpha Test Gate

Alpha is gated by a focused suite, not by unrelated legacy repository status.

### 9.1 Contract Tests

Validate:

- health endpoint
- P2P status and peers endpoints
- config reload endpoint
- JSON miss behavior
- content miss behavior
- metadata-only content behavior
- user-info alias routes

### 9.2 P2P Unit Tests

Validate:

- publish safety before initialization
- receive full PIN path
- metadata-only path
- filter precedence
- storage-limit pause

### 9.3 Same-Machine Dual-Instance Test

Validate with two independent processes:

- separate data dirs
- separate identities
- separate HTTP ports
- shared bootstrap path
- node A stores and announces a new PIN
- node B receives and stores it
- node B serves it over local API

### 9.4 IDBots Handoff Rules

Document for integration:

- which local endpoints are considered authoritative
- what response shape means local hit
- what response shape means local miss
- when centralized fallback should trigger

---

## 10. Implementation Priorities

Order of execution:

1. fix API miss semantics
2. add explicit P2P-only startup path
3. make filter config complete and runtime-injected
4. make status and reload semantics honest
5. add Alpha-specific tests and runnable gate targets
6. document IDBots integration expectations

---

## 11. Future Work After Alpha

After Alpha is stable, the next phases can add:

- peer-assisted historical sync by time window or height window
- richer peer query protocol beyond direct announcement pulls
- stronger restartless network config reload
- MRC20-specific enablement and validation
- more complete standalone deployment packaging
