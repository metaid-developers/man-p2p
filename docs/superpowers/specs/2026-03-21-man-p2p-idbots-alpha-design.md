# man-p2p IDBots Alpha Design

**Date:** 2026-03-21  
**Project:** man-p2p  
**Status:** Draft for implementation planning

---

## 1. Goal

Prepare `man-p2p` for **IDBots Alpha integration** with a narrow, testable scope:

- support local-first reads from `localhost`
- support P2P realtime PIN propagation between nodes
- support minimal sync configuration and status reporting
- preserve centralized API fallback in IDBots for cold start and misses

The goal is **not** to make the whole repository production-ready. The goal is to make the **Alpha integration path** stable, observable, and repeatable.

---

## 2. Product Decision

### 2.1 Alpha Strategy

Adopt **Approach A + a small amount of C**:

- make `man-p2p` itself stable first
- then connect the minimum IDBots call surface
- validate the integration through controlled end-to-end smoke tests

This avoids coupling Alpha success to unrelated legacy tests, tools, or deferred subsystems.

### 2.2 MRC20 Policy

`MRC20` is explicitly **out of scope** for Alpha.

Alpha work may:

- keep extension points for future MRC20 work
- keep existing code present but isolated

Alpha work must not:

- require MRC20 correctness
- require MRC20 balance/indexing tests to pass
- expand MRC20 behavior or data semantics

---

## 3. Alpha Scope

### 3.1 In Scope

Alpha must include these capabilities:

1. **Realtime P2P path**
   - Node A detects a new PIN
   - Node A announces it to peers
   - Node B receives it, applies filters, fetches or stores metadata-only, and persists it locally
   - the new PIN becomes available through Node B's local HTTP API

2. **History read path**
   - IDBots reads local APIs first
   - if local data is missing or local service is unavailable, IDBots falls back to centralized APIs
   - Alpha does not require full pure-P2P historical completeness

3. **Config path**
   - `self`, `selective`, `full`
   - block address/path rules
   - content size limit
   - storage limit behavior
   - config reload endpoint

4. **Status path**
   - health endpoint
   - peer list
   - runtime status for integration diagnostics

### 3.2 Out of Scope

Alpha explicitly excludes:

- MRC20 indexing correctness
- teleport correctness
- pure-P2P historical backfill completeness
- production-grade relay and NAT success guarantees
- UI polish
- large-scale performance testing

---

## 4. System Boundaries

### 4.1 man-p2p Responsibilities

`man-p2p` owns:

- local HTTP API contract
- P2P announce/receive flow
- local persistence after receipt
- configurable local HTTP port for same-machine multi-instance validation
- config reload behavior
- peer/status reporting
- metadata-only handling for oversized content

`man-p2p` does **not** own centralized fallback behavior. That remains in IDBots.

### 4.2 IDBots Responsibilities

IDBots owns:

- local-first request routing
- centralized fallback
- subprocess lifecycle management
- UI/log visibility for data source and service state

This keeps failure attribution clear:

- if the local API is wrong, it is a `man-p2p` issue
- if fallback or process management is wrong, it is an IDBots issue

---

## 5. Alpha API Contract

The Alpha-stable API surface is limited to the endpoints IDBots already needs:

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

### 5.1 Contract Rules

- JSON APIs use the existing envelope format:
  - `{ "code": 1, "message": "ok", "data": ... }`
- success code remains `1`
- content endpoint remains outside `/api`
- empty local content is a valid Alpha state and should allow fallback in IDBots

---

## 6. Runtime Design

### 6.1 Realtime Sync

For Alpha, realtime propagation is the main P2P success path:

1. source node indexes or receives a mempool/confirmed PIN
2. source node publishes compact PIN announcement metadata
3. target node applies subscription and block rules
4. target node:
   - fetches full PIN if under content-size threshold
   - stores metadata-only if oversized
5. target node exposes the result through local HTTP APIs

### 6.2 Historical Reads

Alpha historical behavior is intentionally pragmatic:

- `man-p2p` serves what it already has locally
- IDBots falls back on cache miss or service miss

This is sufficient for Alpha because it proves the local API contract without blocking on full historical P2P discovery.

### 6.3 Status Semantics

Alpha status must be useful, not pretend to be globally accurate.

Required status data:

- peer count
- storage-limit reached flag
- storage-used bytes
- current node identity and listen addresses if available
- current sync mode

`syncProgress` must not claim global network progress unless backed by real logic. During Alpha it may remain a local placeholder or be redefined as a local-state indicator.

---

## 7. Configuration Semantics

### 7.1 Sync Modes

- `self`: only own-address data
- `selective`: explicit address/path allow rules
- `full`: no allow filter, still subject to block rules and storage limit

### 7.2 Rule Priority

Block rules always win over allow rules.

### 7.3 Oversized Content

If content exceeds `p2p_max_content_size_kb`:

- store metadata only
- do not require body bytes to be present
- let IDBots fall back for the body when needed

### 7.4 Storage Limit

When storage limit is reached:

- stop accepting new P2P data
- continue serving already stored local data
- expose the condition in status
- do not auto-delete data

---

## 8. Alpha Gate

Alpha readiness is determined by a dedicated gate, not by unrelated repository-wide green status.

### 8.1 Gate Layer 1: Contract Tests

Verify:

- endpoint existence
- envelope shape
- alias route behavior
- config reload behavior
- content behavior for present and metadata-only states

### 8.2 Gate Layer 2: P2P Unit Tests

Verify:

- publish safety when P2P is not initialized
- receive/store full PIN
- metadata-only store branch
- filter logic
- storage-limit pause behavior

### 8.3 Gate Layer 3: Same-Machine Dual-Process Integration

Verify with two isolated `man-p2p` instances:

- separate `data-dir`
- separate P2P config
- separate HTTP ports
- separate peer identities

Acceptance:

- node A and B connect
- peer counts change as expected
- node A produces a new PIN
- node B receives and stores it
- node B local API serves it
- config reload changes behavior

### 8.4 Gate Layer 4: IDBots Smoke

Verify:

- local-first request path
- fallback path on miss
- subprocess health path
- config reload path
- basic status visibility

---

## 9. Execution Phases

### Phase 1: man-p2p Alpha Gate

Stabilize only the Alpha-critical contract and tests.

### Phase 2: Same-Machine Dual-Process Validation

Prove the P2P core works without IDBots in the loop.

### Phase 3: Minimal IDBots Integration

Switch the required IDBots call sites to local-first with fallback.

### Phase 4: End-to-End Smoke

Run one controlled flow through both systems.

### Phase 5: Dual-Device Validation

Repeat the critical subset on two devices.

---

## 10. Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Legacy tests keep failing and block progress | High | Define Alpha gate separately and isolate non-Alpha tests |
| Same-machine tests pass but real devices fail | Medium | Make dual-device validation a required final phase |
| Local API shape drifts from IDBots assumptions | High | Lock contract tests to the exact IDBots call surface |
| Realtime works but historical completeness is weak | Medium | Keep fallback enabled in Alpha |
| Status fields are misleading | Medium | Prefer honest local-state semantics over fake precision |

---

## 11. Acceptance Criteria

Alpha is ready for controlled IDBots integration only when all of the following are true:

1. `man-p2p` Alpha contract tests pass
2. P2P unit tests for the Alpha flow pass
3. same-machine dual-process integration passes
4. IDBots local-first plus fallback smoke passes
5. a documented dual-device validation checklist exists

Repository-wide green status is desirable, but not an Alpha blocker unless failures affect the Alpha path.

---

## 12. Implementation Constraints

- do not expand MRC20 behavior
- do not couple Alpha success to legacy tooling directories
- do not collapse local service logic and fallback proxy logic into one layer
- prefer small, testable changes over broad cleanup
- keep future extension points available for later pure-P2P history and MRC20 phases

---

## 13. Deliverables

This design should produce:

- a `man-p2p` Alpha gate test suite
- a same-machine dual-process validation flow
- minimal IDBots local-first integration
- one end-to-end smoke path
- a dual-device validation checklist

Once this spec is approved, the next step is a dedicated implementation plan that decomposes the work task-by-task.
