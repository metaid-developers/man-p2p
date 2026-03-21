# man-p2p Alpha Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Harden `man-p2p` so IDBots Alpha can safely use local-first reads, realtime P2P PIN propagation, and centralized fallback on local miss.

**Architecture:** Keep the existing MAN indexing core and current P2P skeleton, but carve out a stable Alpha contract around local API semantics, explicit runtime modes, composable receive filters, and a focused Alpha test gate. Avoid broad refactors or MRC20 expansion.

**Tech Stack:** Go 1.25+, Gin, PebbleDB, go-libp2p, Go standard testing

---

### Task 1: Lock Alpha API miss semantics

**Files:**
- Modify: `api/btc_jsonapi.go`
- Modify: `api/webapi.go`
- Modify: `api/respond/common.go`
- Create or modify: `api/alpha_contract_test.go`

- [ ] **Step 1: Write a failing test for JSON PIN miss**

Add a test that requests `GET /api/pin/{missingPin}` and expects a non-2xx response with an error envelope.

- [ ] **Step 2: Run the test to verify it fails for the right reason**

Run: `CGO_ENABLED=0 go test ./api -run TestAlphaPinMissReturnsNon2xx -count=1 -v`
Expected: FAIL because the handler currently returns `200`.

- [ ] **Step 3: Write a failing test for content miss**

Add a test that requests `GET /content/{missingPin}` and expects a non-2xx response.

- [ ] **Step 4: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./api -run TestAlphaContentMissReturnsNon2xx -count=1 -v`
Expected: FAIL because the handler currently returns `200 fail`.

- [ ] **Step 5: Write a failing test for metadata-only content**

Store a metadata-only PIN locally and assert `GET /content/{pinId}` returns `200` with an empty body plus an explicit metadata marker header.

- [ ] **Step 6: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./api -run TestAlphaMetadataOnlyContentContract -count=1 -v`
Expected: FAIL because the current content handler does not distinguish metadata-only.

- [ ] **Step 7: Implement the minimal API changes**

Change JSON and content handlers to:
- return non-2xx on true miss
- return success only for true local hits
- expose metadata-only content with a stable marker contract

- [ ] **Step 8: Re-run the focused API tests**

Run: `CGO_ENABLED=0 go test ./api -run 'TestAlphaPinMissReturnsNon2xx|TestAlphaContentMissReturnsNon2xx|TestAlphaMetadataOnlyContentContract' -count=1 -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add api/btc_jsonapi.go api/webapi.go api/respond/common.go api/alpha_contract_test.go
git commit -m "feat: harden alpha local API miss semantics"
```

### Task 2: Add explicit runtime mode and P2P-only startup

**Files:**
- Modify: `p2p/config.go`
- Modify: `app.go`
- Modify: `man/man.go`
- Create or modify: `app_alpha_mode_test.go`

- [ ] **Step 1: Write a failing test for P2P-only mode config parsing**

Add a test proving the P2P config can express blockchain-disabled operation.

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test . -run TestP2POnlyModeConfig -count=1 -v`
Expected: FAIL because the config model does not yet expose this mode.

- [ ] **Step 3: Write a failing test for startup mode selection**

Add a test that verifies chain adapter initialization is skipped in P2P-only mode.

- [ ] **Step 4: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test . -run TestStartupSkipsChainInitInP2POnlyMode -count=1 -v`
Expected: FAIL because startup currently always initializes chain adapters.

- [ ] **Step 5: Implement the minimal runtime mode support**

Add explicit blockchain mode fields to P2P config and gate:
- chain adapter init
- `ZmqRun`
- `IndexerRun`
- `CheckNewBlock`

- [ ] **Step 6: Re-run the focused tests**

Run: `CGO_ENABLED=0 go test . -run 'TestP2POnlyModeConfig|TestStartupSkipsChainInitInP2POnlyMode' -count=1 -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add p2p/config.go app.go man/man.go app_alpha_mode_test.go
git commit -m "feat: add explicit p2p-only runtime mode"
```

### Task 3: Complete filter primitives and own-address injection

**Files:**
- Modify: `p2p/config.go`
- Modify: `p2p/subscription.go`
- Modify: `p2p/sync.go`
- Modify: `p2p/config_test.go`
- Modify: `p2p/subscription_test.go`

- [ ] **Step 1: Write a failing test for own-address config injection**

Add a config test showing own addresses are loaded from JSON and used by `self`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./p2p -run TestLoadOwnAddressesForSelfMode -count=1 -v`
Expected: FAIL because own addresses are not in config.

- [ ] **Step 3: Write a failing test for block-overrides-allow under self mode**

Add a test that an own address is still blocked if it appears in block rules.

- [ ] **Step 4: Run the test to verify it fails or is missing**

Run: `CGO_ENABLED=0 go test ./p2p -run TestSelfModeBlockOverridesOwnAddress -count=1 -v`
Expected: FAIL until the path is fully config-driven.

- [ ] **Step 5: Implement minimal filter changes**

Move own-address state into config-backed runtime state and remove reliance on ad hoc global setup in production flow.

- [ ] **Step 6: Re-run the targeted filter tests**

Run: `CGO_ENABLED=0 go test ./p2p -run 'TestLoadOwnAddressesForSelfMode|TestSelfModeBlockOverridesOwnAddress|TestBlocklistOverridesAllowlist|TestSelectivePathMatch|TestOversizedPinStillPassesFilter|TestSelfMode|TestBlockedPath' -count=1 -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add p2p/config.go p2p/subscription.go p2p/sync.go p2p/config_test.go p2p/subscription_test.go
git commit -m "feat: complete alpha filter primitives and self mode injection"
```

### Task 4: Make status and reload semantics honest

**Files:**
- Modify: `p2p/storage.go`
- Modify: `p2p/config.go`
- Modify: `api/p2p_api.go`
- Create or modify: `api/p2p_status_contract_test.go`

- [ ] **Step 1: Write a failing test for required status fields**

Add a test expecting:
- `peerCount`
- `storageUsedBytes`
- `storageLimitReached`
- `syncMode`
- `runtimeMode`
- `peerId`
- `listenAddrs`

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./api -run TestAlphaP2PStatusFields -count=1 -v`
Expected: FAIL because several fields are currently absent.

- [ ] **Step 3: Write a failing reload test for hot-reloadable fields**

Add a test confirming `POST /api/config/reload` updates in-memory filter-related config.

- [ ] **Step 4: Run the test to verify it fails or is incomplete**

Run: `CGO_ENABLED=0 go test ./api -run TestAlphaConfigReloadUpdatesRuntimeFilterState -count=1 -v`
Expected: FAIL if status or runtime view is not updated.

- [ ] **Step 5: Implement minimal status and reload reporting**

Expose honest local state only. Do not claim hot network reconfiguration that does not really happen.

- [ ] **Step 6: Re-run focused tests**

Run: `CGO_ENABLED=0 go test ./api -run 'TestAlphaP2PStatusFields|TestAlphaConfigReloadUpdatesRuntimeFilterState|TestP2PStatusEndpoint|TestConfigReloadEndpoint' -count=1 -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add p2p/storage.go p2p/config.go api/p2p_api.go api/p2p_status_contract_test.go
git commit -m "feat: expose honest alpha status and reload semantics"
```

### Task 5: Build the Alpha gate test suite

**Files:**
- Create: `api/alpha_contract_test.go` if not already created
- Create: `p2p/alpha_dual_instance_test.go`
- Modify: `Makefile`
- Optionally create: `docs/superpowers/specs/alpha-test-gate-notes.md`

- [ ] **Step 1: Write the failing same-machine dual-instance test**

Add a test that starts two isolated P2P hosts with separate data dirs and verifies node B can fetch and store a PIN announced by node A.

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./p2p -run TestAlphaDualInstanceRealtimeSync -count=1 -v`
Expected: FAIL until test harness and storage wiring are complete.

- [ ] **Step 3: Implement the minimal dual-instance harness**

Keep the test scoped to:
- two identities
- two stores
- one announcement
- one successful store on receiver

- [ ] **Step 4: Add a focused Alpha test target**

Update `Makefile` with an `alpha-test` target that runs only the Alpha-critical suites.

- [ ] **Step 5: Run the Alpha gate**

Run: `make alpha-test`
Expected: PASS for the scoped Alpha suites.

- [ ] **Step 6: Commit**

```bash
git add p2p/alpha_dual_instance_test.go Makefile api/alpha_contract_test.go
git commit -m "test: add alpha gate and dual-instance p2p validation"
```

### Task 6: Document IDBots integration contract

**Files:**
- Create: `docs/superpowers/specs/2026-03-21-idbots-man-p2p-alpha-contract.md`
- Optionally modify: `README.md`

- [ ] **Step 1: Write the contract note**

Document:
- local hit semantics
- local miss semantics
- metadata-only content semantics
- runtime mode expectations
- reload limitations

- [ ] **Step 2: Review the document against implemented tests**

Ensure the document matches actual API behavior, not intended behavior.

- [ ] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-03-21-idbots-man-p2p-alpha-contract.md README.md
git commit -m "docs: describe idbots alpha local-first contract"
```
