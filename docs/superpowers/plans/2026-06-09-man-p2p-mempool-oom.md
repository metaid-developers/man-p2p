# MAN P2P Mempool OOM Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop `man-p2p` mempool read paths from causing OOM, API wedging, and Pebble value lifetime crashes under production-sized data.

**Architecture:** Fix the unsafe Pebble read boundary first, then make mempool APIs defensive, then replace full materialization with bounded pagination and a durable mempool sort index. Add local-only diagnostics, HTTP timeouts, slow-request logging, and repeatable memory-pressure checks so production incidents can be diagnosed without relying on restarts.

**Tech Stack:** Go, Gin, PebbleDB, sonic JSON, `net/http/pprof`, shell scripts, `CGO_ENABLED=0` validation.

---

## Background

Ops reported a production incident on `8.217.14.206` where `man-p2p` showed OOM, `127.0.0.1:7777` health checks wedged, and logs included a crash stack through `runtime.memmove`, `runtime.slicebytetostring`, `github.com/bytedance/sonic.Unmarshal`, `pebblestore.(*Database).GetMempoolPageList`, and `api.mempool`.

Code review confirmed these relevant facts:

- `pebblestore.Database.GetMempoolPageList(page, size)` currently scans the whole `PinsMempoolDb`, loads every full PIN from the sharded PIN DBs, unmarshals every PIN, appends all results, sorts all results, then slices the requested page.
- `api.webapi.mempool()` and `api.btc_jsonapi.mempoolList()` both call `GetMempoolPageList`.
- `pebblestore.Database.GetPinByKey`, `GetPinInscriptionByKey`, and `GetMempool` return Pebble `DB.Get()` values after closing their `closer`, even though Pebble only guarantees those bytes before close.
- `pin.PinInscription` contains `ContentBody []byte` and `OriginalContentBody []byte`; mempool pagination currently keeps full bodies in the in-memory result slice.
- The Gin server is started with `r.Run(...)`, so no explicit `ReadTimeout`, `WriteTimeout`, or `IdleTimeout` is configured.

## Execution Rules

- Do not work directly in the existing dirty `main` checkout. Create a separate worktree or branch before implementation.
- Do not revert unrelated local changes such as existing changes under `ops/`, `docs/DEPLOY_SOP.md`, `.claude/`, or `.deepseek/`.
- Use `CGO_ENABLED=0` for normal validation.
- Commit every completed verifiable task with a message matching `<type>: <short description>`.
- After each commit, use the `metabot-post-buzz` skill with identity `lisa-hahn` to post a development-journal entry describing the change.
- Keep changes surgical. Do not refactor unrelated route handlers, deployment scripts, or MRC20 logic while doing these tasks.

## File Structure

### New Files

| File | Responsibility |
|------|----------------|
| `pebblestore/value_helpers.go` | Centralized cloning helpers for Pebble `DB.Get()` and iterator bytes. |
| `pebblestore/mempool_pagination_test.go` | Tests for mempool page bounds, bad records, content trimming, and large-data pagination behavior. |
| `api/mempool_params_test.go` | Handler-level tests for `/mempool/:page` and `/api/mempool/list` validation. |
| `api/server.go` | HTTP server wrapper with explicit timeout settings. |
| `api/middleware.go` | Slow request and response-size middleware. |
| `api/debug_server.go` | Optional localhost-only pprof server. |
| `tools/mempool_pressure/run.sh` | Repeatable local or production pressure probe script. |
| `docs/MAN_P2P_MEMORY_RUNBOOK.md` | Operator runbook for pprof, slow requests, and memory-pressure checks. |

### Modified Files

| File | Change |
|------|--------|
| `pebblestore/store.go` | Use cloned `DB.Get()` values in direct PIN read helpers. |
| `pebblestore/data.go` | Validate mempool pagination inputs, skip corrupt data safely, bound in-memory candidates, and later use mempool sort index. |
| `pebblestore/pincount.go` | Count mempool pins through the canonical mempool id iterator after the mempool keyspace changes. |
| `man/pin_ordering_test.go` | Keep existing mempool ordering regression aligned with the new implementation. |
| `api/webapi.go` | Use server wrapper, register middleware, validate HTML mempool page, start optional pprof, and expose memory diagnostics in `/debug/count`. |
| `api/btc_jsonapi.go` | Validate JSON mempool pagination size and page; use request context. |
| `man/man.go` | Delete mempool sort-index entries when confirmed pins leave mempool. |
| `man/mempool.go` | Write mempool sort-index entries and update scale metrics. |
| `man/p2p_ingest.go` | Write mempool sort-index entries for P2P-ingested unconfirmed pins. |

---

## Task 0: Isolated Worktree And Baseline

**Files:**
- Read: `AGENTS.md`
- Read: `localdocs/README.md` from the source checkout if it is absent in the worktree.
- Read: `docs/superpowers/plans/2026-06-09-man-p2p-mempool-oom.md`

- [ ] **Step 1: Confirm the current checkout is dirty before branching**

Run:

```bash
git status --short --branch
```

Expected: output may include unrelated existing changes such as `ops/` files, `docs/DEPLOY_SOP.md`, `.claude/`, or `.deepseek/`. Do not revert them.

- [ ] **Step 2: Create an isolated worktree**

Run from `/Users/tusm/Documents/MetaID_Projects/man-p2p`:

```bash
mkdir -p /Users/tusm/Documents/MetaID_Projects/worktrees
git worktree add -b codex/fix-man-p2p-mempool-oom /Users/tusm/Documents/MetaID_Projects/worktrees/man-p2p-mempool-oom main
cd /Users/tusm/Documents/MetaID_Projects/worktrees/man-p2p-mempool-oom
```

Expected: new worktree on branch `codex/fix-man-p2p-mempool-oom`, including the tracked plan document from local `main`.

- [ ] **Step 3: Hydrate ignored local context and smoke-test config**

`localdocs/`, `config.toml`, and `p2p-config.json` are ignored by git. Copy them from the source checkout only for local execution:

```bash
SOURCE=/Users/tusm/Documents/MetaID_Projects/man-p2p
if [ -d "$SOURCE/localdocs" ] && [ ! -d localdocs ]; then cp -R "$SOURCE/localdocs" ./localdocs; fi
if [ ! -f config.toml ] && [ -f config.example.toml ]; then cp config.example.toml config.toml; fi
if [ ! -f p2p-config.json ] && [ -f p2p-config.example.json ]; then cp p2p-config.example.json p2p-config.json; fi
```

Expected: `localdocs/README.md` exists when it exists in the source checkout; `config.toml` and `p2p-config.json` exist for the final local smoke.

- [ ] **Step 4: Read project instructions inside the worktree**

Run:

```bash
sed -n '1,260p' AGENTS.md
if [ -f localdocs/README.md ]; then sed -n '1,220p' localdocs/README.md; fi
```

Expected: confirms `CGO_ENABLED=0` validation, small commits, and buzz posting rules.

- [ ] **Step 5: Record baseline test state**

Run:

```bash
CGO_ENABLED=0 go test ./pebblestore ./man ./api -count=1
CGO_ENABLED=0 go test ./p2p/... -v -count=1
```

Expected: either PASS or known baseline failures. If failures occur before edits, save the exact failing package and test name in the task notes and continue only if failures are unrelated to this plan.

---

## Task 1: Clone Pebble Get Values Before Closing

**Files:**
- Create: `pebblestore/value_helpers.go`
- Modify: `pebblestore/store.go:496-516`
- Modify: `pebblestore/data.go:257-274`
- Test: `pebblestore/store_test.go`

- [ ] **Step 1: Add failing tests for cloned direct reads**

Append these tests to `pebblestore/store_test.go`:

```go
func TestGetPinByKeyReturnsIndependentBytes(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewDataBase(dir, 1)
	if err != nil {
		t.Fatalf("NewDataBase err: %v", err)
	}
	defer idx.Close()

	key := "clone-pin"
	db := idx.getShard(key)
	if err := db.Set([]byte(key), []byte(`{"id":"clone-pin","contentBody":"aGVsbG8="}`), nil); err != nil {
		t.Fatalf("set pin: %v", err)
	}

	got, err := idx.GetPinByKey(key)
	if err != nil {
		t.Fatalf("GetPinByKey: %v", err)
	}
	if string(got) != `{"id":"clone-pin","contentBody":"aGVsbG8="}` {
		t.Fatalf("unexpected first read: %s", string(got))
	}

	if err := db.Set([]byte(key), []byte(`{"id":"clone-pin","contentBody":"Ynll"}`), nil); err != nil {
		t.Fatalf("overwrite pin: %v", err)
	}
	if string(got) != `{"id":"clone-pin","contentBody":"aGVsbG8="}` {
		t.Fatalf("GetPinByKey returned reused Pebble memory: %s", string(got))
	}
}

func TestGetMempoolReturnsIndependentBytes(t *testing.T) {
	dir := t.TempDir()
	idx, err := NewDataBase(dir, 1)
	if err != nil {
		t.Fatalf("NewDataBase err: %v", err)
	}
	defer idx.Close()

	key := "mempool-pin"
	if err := idx.PinsMempoolDb.Set([]byte(key), []byte(`{"id":"mempool-pin"}`), nil); err != nil {
		t.Fatalf("set mempool: %v", err)
	}
	got, err := idx.GetMempool(key)
	if err != nil {
		t.Fatalf("GetMempool: %v", err)
	}
	if string(got) != `{"id":"mempool-pin"}` {
		t.Fatalf("unexpected mempool read: %s", string(got))
	}

	if err := idx.PinsMempoolDb.Set([]byte(key), []byte(`{"id":"changed"}`), nil); err != nil {
		t.Fatalf("overwrite mempool: %v", err)
	}
	if string(got) != `{"id":"mempool-pin"}` {
		t.Fatalf("GetMempool returned reused Pebble memory: %s", string(got))
	}
}
```

- [ ] **Step 2: Run the focused failing tests**

Run:

```bash
CGO_ENABLED=0 go test ./pebblestore -run 'TestGetPinByKeyReturnsIndependentBytes|TestGetMempoolReturnsIndependentBytes' -count=1
```

Expected before implementation: these tests may fail or pass nondeterministically because Pebble use-after-close is lifetime-dependent. Keep them as contract tests for independent returned slices, then verify the implementation by code review and package tests after cloning is added.

- [ ] **Step 3: Create cloned read helpers**

Create `pebblestore/value_helpers.go`:

```go
package pebblestore

import (
	"io"

	"github.com/cockroachdb/pebble"
)

func cloneBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func closePebbleValue(closer io.Closer) {
	if closer != nil {
		_ = closer.Close()
	}
}

func getClonedValue(db *pebble.DB, key []byte) ([]byte, error) {
	value, closer, err := db.Get(key)
	if err != nil {
		return nil, err
	}
	defer closePebbleValue(closer)
	return cloneBytes(value), nil
}
```

- [ ] **Step 4: Update direct DB.Get consumers that return or parse after close**

In `pebblestore/store.go`, replace `GetPinByKey` and `GetPinInscriptionByKey` with:

```go
func (idx *Database) GetPinByKey(key string) ([]byte, error) {
	db := idx.getShard(key)
	return getClonedValue(db, []byte(key))
}

func (idx *Database) GetPinInscriptionByKey(key string) (pinNode pin.PinInscription, err error) {
	val, err := idx.GetPinByKey(key)
	if err != nil {
		return
	}
	if len(val) == 0 {
		return
	}
	err = sonic.Unmarshal(val, &pinNode)
	return
}
```

In `pebblestore/data.go`, replace `GetMempool` with:

```go
func (db *Database) GetMempool(key string) ([]byte, error) {
	result, err := getClonedValue(db.PinsMempoolDb, []byte(key))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("GetMempool error: %v", err)
	}
	return result, nil
}
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
CGO_ENABLED=0 go test ./pebblestore -run 'TestGetPinByKeyReturnsIndependentBytes|TestGetMempoolReturnsIndependentBytes' -count=1
```

Expected: PASS.

- [ ] **Step 6: Run related package tests**

Run:

```bash
CGO_ENABLED=0 go test ./pebblestore ./man -count=1
```

Expected: PASS or only pre-existing baseline failures from Task 0.

- [ ] **Step 7: Commit and post buzz**

Run:

```bash
git add pebblestore/value_helpers.go pebblestore/store.go pebblestore/data.go pebblestore/store_test.go
git commit -m "fix: clone pebble get values before closing"
```

Then use `metabot-post-buzz` with `--from lisa-hahn` and content describing the cloned Pebble value fix, tests run, and any baseline failures.

---

## Task 2: Validate Mempool Pagination Inputs

**Files:**
- Modify: `pebblestore/data.go:215-255`
- Modify: `api/webapi.go:257-289`
- Modify: `api/btc_jsonapi.go:109-143`
- Create: `api/mempool_params_test.go`
- Test: `man/pin_ordering_test.go`

- [ ] **Step 1: Add handler-level validation tests**

Create `api/mempool_params_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupMempoolParamRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/mempool/:page", mempool)
	btcJsonApi(r)
	return r
}

func TestMempoolHTMLRejectsPageZero(t *testing.T) {
	r := setupMempoolParamRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/mempool/0", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for legacy HTML handler, got %d", w.Code)
	}
	if body := w.Body.String(); body != "fail" {
		t.Fatalf("expected fail body for page 0, got %q", body)
	}
}

func TestMempoolJSONRejectsPageZero(t *testing.T) {
	r := setupMempoolParamRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mempool/list?page=0&size=100", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 response envelope, got %d", w.Code)
	}
	if body := w.Body.String(); body == "" {
		t.Fatal("expected JSON error response for page 0")
	}
}

func TestMempoolJSONCapsOversizedPageSize(t *testing.T) {
	size := normalizeMempoolPageSize(9999)
	if size != maxMempoolPageSize {
		t.Fatalf("expected oversized page size capped to %d, got %d", maxMempoolPageSize, size)
	}
}

func TestMempoolJSONRejectsDeepPage(t *testing.T) {
	r := setupMempoolParamRouter()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/mempool/list?page=1000000&size=100", nil)

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 response envelope, got %d", w.Code)
	}
	if body := w.Body.String(); body == "" {
		t.Fatal("expected JSON error response for deep page")
	}
}
```

- [ ] **Step 2: Add shared pagination constants and normalizers**

In `api/btc_jsonapi.go`, near the top-level declarations, add:

```go
const (
	defaultMempoolPageSize int64 = 100
	maxMempoolPageSize     int64 = 500
	maxMempoolOffset       int64 = 50000
)

func normalizeMempoolPageSize(size int64) int64 {
	if size <= 0 {
		return defaultMempoolPageSize
	}
	if size > maxMempoolPageSize {
		return maxMempoolPageSize
	}
	return size
}

func validateMempoolPage(page int64, size int64) bool {
	if page < 1 {
		return false
	}
	size = normalizeMempoolPageSize(size)
	return (page-1)*size <= maxMempoolOffset
}
```

- [ ] **Step 3: Validate HTML mempool page**

In `api/webapi.go`, update `mempool(ctx *gin.Context)` so the page check is:

```go
page, err := strconv.ParseInt(ctx.Param("page"), 10, 64)
if err != nil || !validateMempoolPage(page, defaultMempoolPageSize) {
	ctx.String(http.StatusOK, "fail")
	return
}
list, err := man.PebbleStore.Database.GetMempoolPageList(ctx.Request.Context(), page-1, defaultMempoolPageSize)
```

If the package currently lacks `net/http` in `api/webapi.go`, add the import. The file already imports `net/http`, so do not add a duplicate.

- [ ] **Step 4: Validate JSON mempool page and size**

In `api/btc_jsonapi.go`, update `mempoolList(ctx *gin.Context)` so the parsing block is:

```go
page, err := strconv.ParseInt(ctx.Query("page"), 10, 64)
if err != nil || page < 1 {
	ctx.JSON(http.StatusOK, respond.ErrParameterError)
	return
}
size, err := strconv.ParseInt(ctx.Query("size"), 10, 64)
if err != nil {
	ctx.JSON(http.StatusOK, respond.ErrParameterError)
	return
}
size = normalizeMempoolPageSize(size)
if !validateMempoolPage(page, size) {
	ctx.JSON(http.StatusOK, respond.ErrParameterError)
	return
}
list, err := man.PebbleStore.Database.GetMempoolPageList(ctx.Request.Context(), page-1, size)
```

- [ ] **Step 5: Add defensive bounds in the storage method signature**

Change `pebblestore.Database.GetMempoolPageList` signature to:

```go
func (db *Database) GetMempoolPageList(ctx context.Context, page int64, size int64) ([]*pin.PinInscription, error)
```

At the start of the function, add:

```go
const (
	maxMempoolStoragePageSize int64 = 500
	maxMempoolStorageOffset   int64 = 50000
)

if ctx == nil {
	ctx = context.Background()
}
if page < 0 {
	page = 0
}
if size <= 0 {
	size = 100
}
if size > maxMempoolStoragePageSize {
	size = maxMempoolStoragePageSize
}
if page*size > maxMempoolStorageOffset {
	return []*pin.PinInscription{}, nil
}
```

Update existing callers in tests and code to pass `context.Background()` or a request context.

- [ ] **Step 6: Run focused tests**

Run:

```bash
CGO_ENABLED=0 go test ./api -run 'TestMempoolHTMLRejectsPageZero|TestMempoolJSONRejectsPageZero|TestMempoolJSONCapsOversizedPageSize|TestMempoolJSONRejectsDeepPage' -count=1
CGO_ENABLED=0 go test ./man -run TestGetMempoolPageListFallsBackToTimestampWhenSeenTimeMissing -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit and post buzz**

Run:

```bash
git add api/mempool_params_test.go api/webapi.go api/btc_jsonapi.go pebblestore/data.go man/pin_ordering_test.go
git commit -m "fix: validate mempool pagination inputs"
```

Then post a Lisa Hahn development-journal buzz describing the pagination boundary fix and tests run.

---

## Task 3: Bound Mempool Pagination Memory

**Files:**
- Modify: `pebblestore/data.go:215-255`
- Create: `pebblestore/mempool_pagination_test.go`
- Modify: `man/pin_ordering_test.go`

- [ ] **Step 1: Add tests for content trimming, bad data skip, and page correctness**

Create `pebblestore/mempool_pagination_test.go`:

```go
package pebblestore

import (
	"context"
	"fmt"
	"testing"

	"man-p2p/pin"

	"github.com/bytedance/sonic"
)

func newMempoolTestDB(t *testing.T) *Database {
	t.Helper()
	db, err := NewDataBase(t.TempDir(), 1)
	if err != nil {
		t.Fatalf("NewDataBase: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func insertMempoolPin(t *testing.T, db *Database, id string, seenTime int64, body string) {
	t.Helper()
	p := pin.PinInscription{
		Id:            id,
		ChainName:     "mvc",
		GenesisHeight: -1,
		Timestamp:     seenTime,
		SeenTime:      seenTime,
		ContentBody:   []byte(body),
	}
	if err := db.SetAllPins(-1, []*pin.PinInscription{&p}, 1); err != nil {
		t.Fatalf("SetAllPins %s: %v", id, err)
	}
	if err := db.SetMempool(&p); err != nil {
		t.Fatalf("SetMempool %s: %v", id, err)
	}
}

func TestGetMempoolPageListTrimsContentBody(t *testing.T) {
	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-a", 100, "large-body")

	list, err := db.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 pin, got %d", len(list))
	}
	if len(list[0].ContentBody) != 0 {
		t.Fatalf("expected ContentBody trimmed, got %d bytes", len(list[0].ContentBody))
	}
	if list[0].ContentSummary != "large-body" {
		t.Fatalf("expected ContentSummary preserved, got %q", list[0].ContentSummary)
	}
}

func TestGetMempoolPageListSkipsCorruptPinData(t *testing.T) {
	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-good", 200, "ok")

	badKey := "pin-bad"
	shard := db.getShard(badKey)
	if err := shard.Set([]byte(badKey), []byte("{bad-json"), nil); err != nil {
		t.Fatalf("set bad pin: %v", err)
	}
	if err := db.PinsMempoolDb.Set([]byte(badKey), nil, nil); err != nil {
		t.Fatalf("set bad mempool key: %v", err)
	}

	list, err := db.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 1 || list[0].Id != "pin-good" {
		t.Fatalf("expected only good pin, got %#v", list)
	}
}

func TestGetMempoolPageListKeepsOnlyRequestedWindow(t *testing.T) {
	db := newMempoolTestDB(t)
	for i := 0; i < 1000; i++ {
		insertMempoolPin(t, db, fmt.Sprintf("pin-%04d", i), int64(i), "body")
	}

	list, err := db.GetMempoolPageList(context.Background(), 9, 25)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 25 {
		t.Fatalf("expected 25 pins, got %d", len(list))
	}
	if list[0].Id != "pin-0774" {
		raw, _ := sonic.MarshalString(list[0])
		t.Fatalf("unexpected first pin on page 10: %s", raw)
	}
}
```

- [ ] **Step 2: Run tests before implementation**

Run:

```bash
CGO_ENABLED=0 go test ./pebblestore -run 'TestGetMempoolPageListTrimsContentBody|TestGetMempoolPageListSkipsCorruptPinData|TestGetMempoolPageListKeepsOnlyRequestedWindow' -count=1
```

Expected before implementation: `TestGetMempoolPageListTrimsContentBody` fails because `ContentBody` is retained. The page-window test protects ordering/page behavior, but it does not prove memory boundedness by itself; verify the bounded heap code path in review and cover the durable index behavior in Task 4.

- [ ] **Step 3: Add bounded candidate selection types**

In `pebblestore/data.go`, add imports for `container/heap`, `context`, and `log` if missing. Add these types near `GetMempoolPageList`:

```go
type mempoolCandidate struct {
	sortTime int64
	id       string
	pinNode  *pin.PinInscription
}

type mempoolMinHeap []mempoolCandidate

func (h mempoolMinHeap) Len() int { return len(h) }

func (h mempoolMinHeap) Less(i, j int) bool {
	if h[i].sortTime == h[j].sortTime {
		return h[i].id < h[j].id
	}
	return h[i].sortTime < h[j].sortTime
}

func (h mempoolMinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *mempoolMinHeap) Push(x interface{}) {
	*h = append(*h, x.(mempoolCandidate))
}

func (h *mempoolMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func trimMempoolPinForList(pinNode *pin.PinInscription) {
	if pinNode == nil {
		return
	}
	if pinNode.ContentSummary == "" && len(pinNode.ContentBody) > 0 {
		pinNode.ContentSummary = string(pinNode.ContentBody)
	}
	pinNode.ContentBody = nil
	pinNode.OriginalContentBody = nil
}
```

- [ ] **Step 4: Replace full slice materialization with bounded heap**

Update `GetMempoolPageList` so it retains at most `limit := int((page + 1) * size)` candidates. Because Task 2 caps the storage offset at `50000`, legacy fallback retains a bounded candidate window instead of the whole mempool:

```go
limit := int((page + 1) * size)
if limit <= 0 {
	return []*pin.PinInscription{}, nil
}

bounded := &mempoolMinHeap{}
heap.Init(bounded)

for iter.First(); iter.Valid(); iter.Next() {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	key := string(iter.Key())
	data, err := db.GetPinByKey(key)
	if err != nil {
		continue
	}
	var pinNode pin.PinInscription
	if err := sonic.Unmarshal(data, &pinNode); err != nil {
		log.Printf("[WARN] mempool pagination skipped corrupt pin id=%s: %v", key, err)
		continue
	}
	trimMempoolPinForList(&pinNode)
	candidate := mempoolCandidate{
		sortTime: pin.EffectiveSortTime(&pinNode),
		id:       pinNode.Id,
		pinNode:  &pinNode,
	}
	if bounded.Len() < limit {
		heap.Push(bounded, candidate)
		continue
	}
	worst := (*bounded)[0]
	if candidate.sortTime > worst.sortTime || (candidate.sortTime == worst.sortTime && candidate.id > worst.id) {
		heap.Pop(bounded)
		heap.Push(bounded, candidate)
	}
}

result := make([]*pin.PinInscription, 0, bounded.Len())
for bounded.Len() > 0 {
	item := heap.Pop(bounded).(mempoolCandidate)
	result = append(result, item.pinNode)
}
sort.SliceStable(result, func(i, j int) bool {
	a := pin.EffectiveSortTime(result[i])
	b := pin.EffectiveSortTime(result[j])
	if a == b {
		return result[i].Id > result[j].Id
	}
	return a > b
})

start := int(page * size)
if start >= len(result) {
	return []*pin.PinInscription{}, nil
}
end := start + int(size)
if end > len(result) {
	end = len(result)
}
return result[start:end], nil
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
CGO_ENABLED=0 go test ./pebblestore -run 'TestGetMempoolPageList|TestGetMempoolPageListTrimsContentBody|TestGetMempoolPageListSkipsCorruptPinData|TestGetMempoolPageListKeepsOnlyRequestedWindow' -count=1
CGO_ENABLED=0 go test ./man -run TestGetMempoolPageListFallsBackToTimestampWhenSeenTimeMissing -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit and post buzz**

Run:

```bash
git add pebblestore/data.go pebblestore/mempool_pagination_test.go man/pin_ordering_test.go
git commit -m "fix: bound mempool pagination memory"
```

Then post a Lisa Hahn development-journal buzz describing bounded pagination, corrupt-data skipping, and tests run.

---

## Task 4: Add A Durable Mempool Sort Index

**Files:**
- Modify: `pebblestore/store.go:32-156`
- Modify: `pebblestore/data.go:208-292`
- Modify: `pebblestore/pincount.go:122-164`
- Modify: `man/mempool.go:13-28`
- Modify: `man/p2p_ingest.go:32-39`
- Modify: `man/man.go:397-460`
- Test: `pebblestore/mempool_pagination_test.go`

- [ ] **Step 1: Add index behavior tests**

Append these tests to `pebblestore/mempool_pagination_test.go`:

```go
func TestMempoolSortIndexPaginatesWithoutLegacyScanOrder(t *testing.T) {
	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-old", 10, "old")
	insertMempoolPin(t, db, "pin-new", 30, "new")
	insertMempoolPin(t, db, "pin-mid", 20, "mid")

	list, err := db.GetMempoolPageList(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 pins, got %d", len(list))
	}
	if list[0].Id != "pin-new" || list[1].Id != "pin-mid" {
		t.Fatalf("expected sort index order pin-new,pin-mid got %s,%s", list[0].Id, list[1].Id)
	}
}

func TestDeleteMempoolRemovesSortIndex(t *testing.T) {
	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-delete", 10, "body")

	if err := db.DeleteMempool("pin-delete"); err != nil {
		t.Fatalf("DeleteMempool: %v", err)
	}
	list, err := db.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected deleted mempool pin absent, got %d", len(list))
	}
}

func TestSetMempoolReplacesPriorSortIndex(t *testing.T) {
	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-dup", 10, "old")

	updated := pin.PinInscription{
		Id:            "pin-dup",
		ChainName:     "mvc",
		GenesisHeight: -1,
		Timestamp:     50,
		SeenTime:      50,
		ContentBody:   []byte("new"),
	}
	if err := db.SetAllPins(-1, []*pin.PinInscription{&updated}, 1); err != nil {
		t.Fatalf("SetAllPins updated: %v", err)
	}
	if err := db.SetMempool(&updated); err != nil {
		t.Fatalf("SetMempool updated: %v", err)
	}

	list, err := db.GetMempoolPageList(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 1 || list[0].Id != "pin-dup" {
		t.Fatalf("expected exactly one updated pin, got %#v", list)
	}
}

func TestMempoolSortIndexSkipsStaleEntriesWithoutShiftingPage(t *testing.T) {
	db := newMempoolTestDB(t)
	insertMempoolPin(t, db, "pin-a", 20, "a")
	insertMempoolPin(t, db, "pin-b", 10, "b")

	staleSortKey := []byte("sort:00000000000000000030&mvc&pin-missing")
	if err := db.PinsMempoolDb.Set(staleSortKey, []byte("pin-missing"), nil); err != nil {
		t.Fatalf("set stale sort key: %v", err)
	}

	list, err := db.GetMempoolPageList(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("GetMempoolPageList: %v", err)
	}
	if len(list) != 2 || list[0].Id != "pin-a" || list[1].Id != "pin-b" {
		t.Fatalf("expected stale key skipped without shifting page, got %#v", list)
	}
}
```

- [ ] **Step 2: Add mempool index key helpers**

In `pebblestore/data.go`, add:

```go
const (
	mempoolPinKeyPrefix  = "pin:"
	mempoolSortKeyPrefix = "sort:"
	mempoolByIDKeyPrefix = "byid:"
)

func mempoolPinKey(pinID string) []byte {
	return []byte(mempoolPinKeyPrefix + pinID)
}

func mempoolByIDKey(pinID string) []byte {
	return []byte(mempoolByIDKeyPrefix + pinID)
}

func mempoolSortKey(pinNode *pin.PinInscription) []byte {
	sortTime := pin.EffectiveSortTime(pinNode)
	if sortTime == 0 {
		sortTime = pin.LegacyMempoolSortTime
	}
	return []byte(fmt.Sprintf("%s%020d&%s&%s", mempoolSortKeyPrefix, sortTime, pinNode.ChainName, pinNode.Id))
}

func isLegacyMempoolKey(key []byte) bool {
	return !strings.HasPrefix(string(key), mempoolPinKeyPrefix) &&
		!strings.HasPrefix(string(key), mempoolSortKeyPrefix) &&
		!strings.HasPrefix(string(key), mempoolByIDKeyPrefix)
}

func mempoolPinIDFromKey(key []byte) (string, bool) {
	keyText := string(key)
	if strings.HasPrefix(keyText, mempoolPinKeyPrefix) {
		return strings.TrimPrefix(keyText, mempoolPinKeyPrefix), true
	}
	if isLegacyMempoolKey(key) {
		return keyText, true
	}
	return "", false
}

func (db *Database) forEachMempoolPinID(ctx context.Context, fn func(pinID string) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	iter, err := db.PinsMempoolDb.NewIter(nil)
	if err != nil {
		return err
	}
	defer iter.Close()
	seen := make(map[string]struct{})
	for iter.First(); iter.Valid(); iter.Next() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		pinID, ok := mempoolPinIDFromKey(cloneBytes(iter.Key()))
		if !ok || pinID == "" {
			continue
		}
		if _, exists := seen[pinID]; exists {
			continue
		}
		seen[pinID] = struct{}{}
		if err := fn(pinID); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 3: Update mempool write path**

Replace `SetMempool` with:

```go
func (db *Database) SetMempool(pinNode *pin.PinInscription) error {
	if pinNode == nil || pinNode.Id == "" {
		return nil
	}
	sortKey := mempoolSortKey(pinNode)
	oldSortKey, err := getClonedValue(db.PinsMempoolDb, mempoolByIDKey(pinNode.Id))
	if err != nil && err != pebble.ErrNotFound {
		return err
	}
	batch := db.PinsMempoolDb.NewBatch()
	defer batch.Close()
	if len(oldSortKey) > 0 && string(oldSortKey) != string(sortKey) {
		if err := batch.Delete(oldSortKey, nil); err != nil {
			return err
		}
	}
	if err := batch.Set(mempoolPinKey(pinNode.Id), nil, nil); err != nil {
		return err
	}
	if err := batch.Set(sortKey, []byte(pinNode.Id), nil); err != nil {
		return err
	}
	if err := batch.Set(mempoolByIDKey(pinNode.Id), sortKey, nil); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}
```

- [ ] **Step 4: Update mempool delete path**

Replace `DeleteMempool` with:

```go
func (db *Database) DeleteMempool(key string) error {
	batch := db.PinsMempoolDb.NewBatch()
	defer batch.Close()
	if sortKey, err := getClonedValue(db.PinsMempoolDb, mempoolByIDKey(key)); err == nil && len(sortKey) > 0 {
		if err := batch.Delete(sortKey, nil); err != nil {
			return err
		}
	}
	if err := batch.Delete(mempoolByIDKey(key), nil); err != nil {
		return err
	}
	if err := batch.Delete(mempoolPinKey(key), nil); err != nil {
		return err
	}
	if err := batch.Delete([]byte(key), nil); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}
```

- [ ] **Step 5: Update batch delete to call DeleteMempool**

Replace `BatchDeleteMempool` implementation with:

```go
func (db *Database) BatchDeleteMempool(keys []string) error {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if err := db.DeleteMempool(key); err != nil {
			return err
		}
	}
	return nil
}
```

Update `man.DeleteMempoolData` to call `PebbleStore.Database.BatchDeleteMempool(pinIdList)` instead of `pebblestore.DeleteBatchByKeyList(PebbleStore.Database.PinsMempoolDb, &pinIdList)`.

- [ ] **Step 6: Update mempool count scan to use canonical pin ids**

In `pebblestore/pincount.go`, replace the mempool scan block that appends every raw `PinsMempoolDb` key with a scan through `forEachMempoolPinID`. Add the `context` import.

Use this code shape inside `StatPinSortTable`:

```go
var mempoolKeys []string
flushMempoolKeys := func() {
	if len(mempoolKeys) == 0 {
		return
	}
	pins, _ := db.GetPinListByIdList(mempoolKeys, 100, false)
	for _, p := range pins {
		if p != nil {
			mempoolStat.PinCount += 1
			if p.Path != "" {
				mempoolStat.PathPinCount[p.Path] += 1
				if p.MetaId != "" {
					mempoolStat.AddressPathPinCount[p.MetaId+"_"+p.Path] += 1
				}
			}
		}
	}
	mempoolKeys = mempoolKeys[:0]
}

if err := db.forEachMempoolPinID(context.Background(), func(pinID string) error {
	mempoolKeys = append(mempoolKeys, pinID)
	if len(mempoolKeys) >= 100 {
		flushMempoolKeys()
	}
	return nil
}); err != nil {
	return err
}
flushMempoolKeys()
```

Expected: mempool count ignores `sort:` and `byid:` keys, supports legacy raw pin-id keys, and dedupes mixed legacy/new data.

- [ ] **Step 7: Implement indexed pagination with bounded scan fallback**

Update `GetMempoolPageList` to:

1. Try iterating `sort:` keys in reverse order with lower bound `[]byte(mempoolSortKeyPrefix)` and upper bound `[]byte("sort;")`.
2. For each sort entry, read the pin id from `iter.Value()`, then read the full pin through `GetPinByKey`.
3. Validate and trim the backing pin before it counts toward the page window.
4. Skip duplicate pin ids and stale/corrupt sort entries without incrementing the visible row counter.
5. Opportunistically delete stale sort entries after closing the iterator.
6. Stop after collecting the requested page.
7. If no `sort:` keys exist, use the bounded scan implementation from Task 3 and backfill index entries for successfully read legacy keys.

Use this code shape:

```go
func (db *Database) getMempoolPageListFromIndex(ctx context.Context, page int64, size int64) ([]*pin.PinInscription, bool, error) {
	lower := []byte(mempoolSortKeyPrefix)
	upper := []byte("sort;")
	iter, err := db.PinsMempoolDb.NewIter(&pebble.IterOptions{LowerBound: lower, UpperBound: upper})
	if err != nil {
		return nil, false, err
	}

	start := page * size
	end := start + size
	var visible int64
	sawIndex := false
	seenIDs := make(map[string]struct{})
	var staleSortKeys [][]byte
	result := make([]*pin.PinInscription, 0, size)
	defer func() {
		if err := iter.Close(); err != nil {
			log.Printf("[WARN] mempool index iterator close failed: %v", err)
		}
		for _, key := range staleSortKeys {
			if err := db.PinsMempoolDb.Delete(key, nil); err != nil {
				log.Printf("[WARN] mempool index stale cleanup failed key=%s: %v", string(key), err)
			}
		}
	}()

	for iter.Last(); iter.Valid(); iter.Prev() {
		select {
		case <-ctx.Done():
			return nil, true, ctx.Err()
		default:
		}
		sawIndex = true
		sortKey := cloneBytes(iter.Key())
		pinID := string(cloneBytes(iter.Value()))
		if pinID == "" {
			staleSortKeys = append(staleSortKeys, sortKey)
			continue
		}
		if _, exists := seenIDs[pinID]; exists {
			staleSortKeys = append(staleSortKeys, sortKey)
			continue
		}
		data, err := db.GetPinByKey(pinID)
		if err != nil {
			staleSortKeys = append(staleSortKeys, sortKey)
			continue
		}
		var pinNode pin.PinInscription
		if err := sonic.Unmarshal(data, &pinNode); err != nil {
			log.Printf("[WARN] mempool index skipped corrupt pin id=%s: %v", pinID, err)
			staleSortKeys = append(staleSortKeys, sortKey)
			continue
		}
		trimMempoolPinForList(&pinNode)
		seenIDs[pinID] = struct{}{}
		if visible < start {
			visible++
			continue
		}
		if visible >= end {
			break
		}
		result = append(result, &pinNode)
		visible++
	}
	return result, sawIndex, nil
}
```

- [ ] **Step 8: Run focused tests**

Run:

```bash
CGO_ENABLED=0 go test ./pebblestore -run 'TestMempoolSortIndex|TestDeleteMempoolRemovesSortIndex|TestSetMempoolReplacesPriorSortIndex|TestMempoolSortIndexSkipsStaleEntriesWithoutShiftingPage|TestGetMempoolPageList' -count=1
CGO_ENABLED=0 go test ./man -run 'TestGetMempoolPageListFallsBackToTimestampWhenSeenTimeMissing|TestSyncExistingMempoolProcessesPins' -count=1
```

Expected: PASS.

- [ ] **Step 9: Commit and post buzz**

Run:

```bash
git add pebblestore/data.go pebblestore/store.go pebblestore/pincount.go pebblestore/mempool_pagination_test.go man/mempool.go man/p2p_ingest.go man/man.go man/pin_ordering_test.go
git commit -m "fix: add indexed mempool pagination"
```

Then post a Lisa Hahn development-journal buzz describing the sort index, legacy fallback, delete cleanup, and tests run.

---

## Task 5: HTTP Server Timeouts And Slow Request Logging

**Files:**
- Create: `api/server.go`
- Create: `api/middleware.go`
- Modify: `api/webapi.go:83-160`
- Test: `api/mempool_params_test.go`

- [ ] **Step 1: Add server timeout wrapper**

Create `api/server.go`:

```go
package api

import (
	"net/http"
	"time"
)

func runHTTPServer(addr string, handler http.Handler, certFile string, keyFile string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	if certFile != "" && keyFile != "" {
		return srv.ListenAndServeTLS(certFile, keyFile)
	}
	return srv.ListenAndServe()
}
```

- [ ] **Step 2: Add slow request middleware**

Create `api/middleware.go`:

```go
package api

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

const slowRequestThreshold = 2 * time.Second

type responseSizeWriter struct {
	gin.ResponseWriter
	size int
}

func (w *responseSizeWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.size += n
	return n, err
}

func SlowRequestLogger() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		start := time.Now()
		writer := &responseSizeWriter{ResponseWriter: ctx.Writer}
		ctx.Writer = writer
		ctx.Next()
		elapsed := time.Since(start)
		if elapsed >= slowRequestThreshold {
			log.Printf("[WARN] slow request path=%s query=%s status=%d duration=%s bytes=%d",
				ctx.Request.URL.Path,
				ctx.Request.URL.RawQuery,
				ctx.Writer.Status(),
				elapsed,
				writer.size,
			)
		}
	}
}
```

- [ ] **Step 3: Register middleware and server wrapper**

In `api/webapi.go`, after `r := gin.Default()`, add:

```go
r.Use(SlowRequestLogger())
```

Replace the `RunTLS` / `Run` block with:

```go
if err := runHTTPServer(common.Config.Web.Port, r, common.Config.Web.PemFile, common.Config.Web.KeyFile); err != nil {
	log.Printf("Server Start failed: %v", err)
}
```

- [ ] **Step 4: Run API tests**

Run:

```bash
CGO_ENABLED=0 go test ./api -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit and post buzz**

Run:

```bash
git add api/server.go api/middleware.go api/webapi.go api/mempool_params_test.go
git commit -m "fix: add api timeouts and slow request logging"
```

Then post a Lisa Hahn development-journal buzz describing timeout values, slow request fields, and tests run.

---

## Task 6: Localhost-Only Pprof Diagnostics

**Files:**
- Create: `api/debug_server.go`
- Modify: `api/webapi.go:83-160`
- Create: `docs/MAN_P2P_MEMORY_RUNBOOK.md`

- [ ] **Step 1: Add optional pprof server**

Create `api/debug_server.go`:

```go
package api

import (
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
)

func startLocalDebugServerFromEnv() {
	addr := strings.TrimSpace(os.Getenv("MAN_P2P_PPROF_ADDR"))
	if addr == "" {
		return
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Printf("[WARN] pprof disabled: invalid MAN_P2P_PPROF_ADDR=%q: %v", addr, err)
		return
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		log.Printf("[WARN] pprof disabled: MAN_P2P_PPROF_ADDR must be localhost, got %q", addr)
		return
	}
	go func() {
		log.Printf("[INFO] pprof listening on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("[WARN] pprof server stopped: %v", err)
		}
	}()
}
```

- [ ] **Step 2: Start pprof before the public API server**

In `api/webapi.go`, inside `Start(f embed.FS)` after Gin mode setup, call:

```go
startLocalDebugServerFromEnv()
```

- [ ] **Step 3: Add operator runbook**

Create `docs/MAN_P2P_MEMORY_RUNBOOK.md`:

````markdown
# MAN P2P Memory Runbook

## Enable Local Pprof

Set this only on the host running `man-p2p.service`:

```bash
MAN_P2P_PPROF_ADDR=127.0.0.1:6060
```

The address must be localhost. The process refuses non-localhost pprof binds.

## Capture Profiles

```bash
mkdir -p /root/deploy-backups/man-p2p-profiles-$(date +%Y%m%d-%H%M%S)
cd /root/deploy-backups/man-p2p-profiles-*
curl -sS --max-time 30 http://127.0.0.1:6060/debug/pprof/heap > heap.pb.gz
curl -sS --max-time 30 http://127.0.0.1:6060/debug/pprof/goroutine > goroutine.pb.gz
curl -sS --max-time 30 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30' > cpu.pb.gz
curl -sS --max-time 30 http://127.0.0.1:6060/debug/pprof/mutex > mutex.pb.gz
curl -sS --max-time 30 http://127.0.0.1:6060/debug/pprof/block > block.pb.gz
```

## Interpret Cgroup Memory

Read both `memory.current` and `memory.stat`:

```bash
systemctl show man-p2p.service -p MainPID -p MemoryCurrent -p MemoryHigh -p MemoryMax
cat /sys/fs/cgroup/system.slice/man-p2p.service/memory.stat | egrep '^(anon|file|kernel|slab) '
```

High `file` with low `anon` points at Pebble/file-cache pressure. High `anon` or high heap profile points at Go heap pressure.
````

- [ ] **Step 4: Run package tests**

Run:

```bash
CGO_ENABLED=0 go test ./api -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit and post buzz**

Run:

```bash
git add api/debug_server.go api/webapi.go docs/MAN_P2P_MEMORY_RUNBOOK.md
git commit -m "feat: add localhost pprof diagnostics"
```

Then post a Lisa Hahn development-journal buzz describing the pprof guard, runbook, and tests run.

---

## Task 7: Mempool Scale Metrics And Pressure Probe

**Files:**
- Modify: `pebblestore/data.go`
- Modify: `api/webapi.go`
- Create: `api/debug_mempool.go` if the gated handler is kept out of `api/webapi.go`
- Create: `tools/mempool_pressure/run.sh`
- Modify: `docs/MAN_P2P_MEMORY_RUNBOOK.md`

- [ ] **Step 1: Add cheap mempool index stats type and method**

In `pebblestore/data.go`, add index-only stats. This method must not call `GetPinByKey` or unmarshal full PIN bodies:

```go
type MempoolStats struct {
	Total          int64            `json:"total"`
	ByChain        map[string]int64  `json:"byChain"`
	OldestSeenTime int64            `json:"oldestSeenTime"`
	NewestSeenTime int64            `json:"newestSeenTime"`
	Indexed        bool             `json:"indexed"`
	LegacyKeys     int64            `json:"legacyKeys"`
}

func (db *Database) GetMempoolIndexStats(ctx context.Context) (MempoolStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	stats := MempoolStats{ByChain: make(map[string]int64)}
	iter, err := db.PinsMempoolDb.NewIter(&pebble.IterOptions{
		LowerBound: []byte(mempoolSortKeyPrefix),
		UpperBound: []byte("sort;"),
	})
	if err != nil {
		return stats, err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}
		stats.Indexed = true
		sortTime, chainName, ok := parseMempoolSortKey(cloneBytes(iter.Key()))
		if !ok {
			continue
		}
		stats.Total++
		stats.ByChain[chainName]++
		if stats.OldestSeenTime == 0 || sortTime < stats.OldestSeenTime {
			stats.OldestSeenTime = sortTime
		}
		if sortTime > stats.NewestSeenTime {
			stats.NewestSeenTime = sortTime
		}
	}
	if stats.Indexed {
		return stats, nil
	}
	if err := db.forEachMempoolPinID(ctx, func(pinID string) error {
		stats.Total++
		stats.LegacyKeys++
		return nil
	}); err != nil {
		return stats, err
	}
	return stats, nil
}

func parseMempoolSortKey(key []byte) (int64, string, bool) {
	text := strings.TrimPrefix(string(key), mempoolSortKeyPrefix)
	parts := strings.SplitN(text, "&", 3)
	if len(parts) != 3 {
		return 0, "", false
	}
	sortTime, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return sortTime, parts[1], true
}
```

- [ ] **Step 2: Keep `/debug/count` cheap and add runtime memory fields**

In `api/webapi.go`, update `debugCount` to include goroutine count and heap counters only. Do not scan `PinsMempoolDb` in this handler:

```go
var mem runtime.MemStats
runtime.ReadMemStats(&mem)
ctx.JSON(200, gin.H{
	"pin":        count.Pin,
	"block":      count.Block,
	"metaId":     count.MetaId,
	"app":        count.App,
	"goroutines": runtime.NumGoroutine(),
	"heapAlloc":  mem.HeapAlloc,
	"heapInuse":  mem.HeapInuse,
})
```

Add `runtime` to imports if needed.

- [ ] **Step 3: Add env-gated localhost mempool stats endpoint**

In `api/webapi.go`, register:

```go
r.GET("/debug/mempool", debugMempool)
```

Implement the handler in `api/webapi.go` or a small `api/debug_mempool.go` file:

```go
func debugMempool(ctx *gin.Context) {
	if os.Getenv("MAN_P2P_ENABLE_MEMPOOL_STATS") != "1" || !isLocalDebugRequest(ctx.Request) {
		ctx.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	stats, err := man.PebbleStore.Database.GetMempoolIndexStats(ctx.Request.Context())
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, stats)
}

func isLocalDebugRequest(req *http.Request) bool {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}
```

Add `net`, `os`, and `net/http` imports only where needed. `api/webapi.go` already imports `net/http`.

- [ ] **Step 4: Add pressure probe script**

Create `tools/mempool_pressure/run.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:7777}"
SERVICE="${SERVICE:-man-p2p.service}"
INTERVAL_SECONDS="${INTERVAL_SECONDS:-60}"
ITERATIONS="${ITERATIONS:-60}"
OUT_DIR="${OUT_DIR:-./mempool-pressure-$(date +%Y%m%d-%H%M%S)}"

mkdir -p "$OUT_DIR"
echo "timestamp,endpoint,status,time_total" > "$OUT_DIR/requests.csv"
echo "timestamp,rss_kb,memory_current,goroutines,heap_alloc,heap_inuse" > "$OUT_DIR/memory.csv"

probe_endpoint() {
	local endpoint="$1"
	local now status total
	now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	read -r status total < <(curl -sS -o /dev/null -w '%{http_code} %{time_total}' --max-time 30 "$BASE_URL$endpoint" || echo "000 30")
	echo "$now,$endpoint,$status,$total" >> "$OUT_DIR/requests.csv"
}

sample_memory() {
	local now pid rss mem count_json gor alloc inuse
	now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	pid="$(systemctl show "$SERVICE" -p MainPID --value 2>/dev/null || true)"
	rss="0"
	if [[ -n "$pid" && "$pid" != "0" && -r "/proc/$pid/status" ]]; then
		rss="$(awk '/VmRSS/ {print $2}' "/proc/$pid/status")"
	fi
	mem="$(systemctl show "$SERVICE" -p MemoryCurrent --value 2>/dev/null || echo 0)"
	count_json="$(curl -sS --max-time 5 "$BASE_URL/debug/count" || true)"
	gor="$(printf '%s\n' "$count_json" | sed -n 's/.*"goroutines":\([0-9]*\).*/\1/p')"
	alloc="$(printf '%s\n' "$count_json" | sed -n 's/.*"heapAlloc":\([0-9]*\).*/\1/p')"
	inuse="$(printf '%s\n' "$count_json" | sed -n 's/.*"heapInuse":\([0-9]*\).*/\1/p')"
	echo "$now,$rss,$mem,${gor:-0},${alloc:-0},${inuse:-0}" >> "$OUT_DIR/memory.csv"
}

for ((i=1; i<=ITERATIONS; i++)); do
	sample_memory
	probe_endpoint "/health"
	probe_endpoint "/api/p2p/status"
	probe_endpoint "/debug/count"
	probe_endpoint "/mempool/1"
	probe_endpoint "/api/mempool/list?page=1&size=100"
	probe_endpoint "/api/pin/path/list?path=/protocols/skill-service&size=10"
	sleep "$INTERVAL_SECONDS"
done
```

Run:

```bash
chmod +x tools/mempool_pressure/run.sh
```

- [ ] **Step 5: Document pressure probe**

Append to `docs/MAN_P2P_MEMORY_RUNBOOK.md`:

````markdown
## Pressure Probe

Run a 60-minute probe:

```bash
BASE_URL=http://127.0.0.1:7777 SERVICE=man-p2p.service ITERATIONS=60 INTERVAL_SECONDS=60 tools/mempool_pressure/run.sh
```

The script writes `requests.csv` and `memory.csv`. Compare pre-fix and post-fix runs by checking `/health` timeouts, `/api/mempool/list` latency, RSS, `MemoryCurrent`, goroutine count, and runtime heap counters. Use pprof from the earlier runbook section only when deeper heap attribution is needed.
````

- [ ] **Step 6: Run tests and shell syntax check**

Run:

```bash
bash -n tools/mempool_pressure/run.sh
CGO_ENABLED=0 go test ./pebblestore ./api -count=1
```

Expected: shell syntax check passes; Go tests pass or only show Task 0 baseline failures.

- [ ] **Step 7: Commit and post buzz**

Run:

```bash
git add pebblestore/data.go api/webapi.go tools/mempool_pressure/run.sh docs/MAN_P2P_MEMORY_RUNBOOK.md
if [ -f api/debug_mempool.go ]; then git add api/debug_mempool.go; fi
git commit -m "feat: add mempool memory pressure checks"
```

Then post a Lisa Hahn development-journal buzz describing mempool stats, pressure script, runbook updates, and tests run.

---

## Final Verification

- [ ] **Run focused packages**

```bash
CGO_ENABLED=0 go test ./pebblestore ./man ./api -count=1
```

Expected: PASS.

- [ ] **Run P2P suite**

```bash
CGO_ENABLED=0 go test ./p2p/... -v -count=1
```

Expected: PASS.

- [ ] **Run project test target**

```bash
make test
```

Expected: PASS. If this target includes only `./p2p/...`, record that explicitly in the final implementation summary.

- [ ] **Build Linux production binary**

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o dist/man-p2p-linux-amd64 .
```

Expected: binary builds successfully.

- [ ] **Run local smoke against a temporary data dir**

```bash
CGO_ENABLED=0 go run . -config ./config.toml -server=1 -p2p-config ./p2p-config.json -data-dir ./man_p2p_data_test
```

In another shell:

```bash
curl -sS --max-time 5 http://127.0.0.1:7777/health
curl -sS --max-time 10 'http://127.0.0.1:7777/api/mempool/list?page=1&size=100'
curl -sS --max-time 10 http://127.0.0.1:7777/debug/count
# Only if the service was started with MAN_P2P_ENABLE_MEMPOOL_STATS=1:
curl -sS --max-time 10 http://127.0.0.1:7777/debug/mempool
```

Expected: `/health` returns status `ok`; mempool endpoint returns a bounded response; `/debug/count` includes runtime memory fields after Task 7. `/debug/mempool` returns index-level mempool stats only when the service is started with `MAN_P2P_ENABLE_MEMPOOL_STATS=1` and the request is localhost.

## Subagent Execution Notes

When executing this plan with `superpowers:subagent-driven-development`:

1. Dispatch one implementer subagent per Task 1 through Task 7.
2. Give each implementer only the task text, the background section, and the execution rules.
3. After each implementer reports done, dispatch a spec reviewer subagent that checks only whether the task matches this plan.
4. After spec review passes, dispatch a code-quality reviewer subagent.
5. Do not proceed to the next task until the current task has tests passing, review complete, commit created, and Lisa Hahn buzz posted.
6. Stop only if a task is blocked by a real ambiguity, failing baseline that invalidates the task, or production-specific data that cannot be simulated locally.
