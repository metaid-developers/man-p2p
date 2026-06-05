# User Data Export — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `POST /api/export/user-data` endpoint that streams a ZIP archive of all PINs for a given identity (GlobalMetaID or address) within a block height range, organized by month → path for AI consumption.

**Architecture:** New `export/` package handles query + archive generation. New `api/export_handler.go` registers the route. Uses `archive/zip` streaming via `io.Pipe` to avoid full buffering. Data already indexed in PebbleDB (`AddressDB` by MetaId prefix, `PinsDBs` by PinId).

**Tech Stack:** Go stdlib (`archive/zip`, `encoding/json`, `io`), sonic for JSON, PebbleDB via `man.PebbleStore.Database`

---

## File Structure

### New files
| File | Responsibility |
|------|----------------|
| `export/types.go` | Request/response types, metadata structs, PIN record wrapper |
| `export/query.go` | PebbleDB iteration: identity→MetaId→AddressDB prefix scan→PinsDBs fetch |
| `export/archive.go` | Streaming ZIP builder: month→path grouping, write entries, classification |
| `export/export.go` | Top-level `ExportUserData()` orchestrator |
| `export/export_test.go` | Unit + integration tests with in-memory PebbleDB |
| `api/export_handler.go` | Gin handler: validate, call export, stream response |

### Modified files
| File | Change |
|------|--------|
| `api/webapi.go` | Add `RegisterExportRoutes(r)` call after `RegisterP2PRoutes(r)` |

---

### Task 1: Core types — `export/types.go`

**Create:** `export/types.go`

- [ ] **Define request/response types**

```go
package export

import "man-p2p/pin"

type ExportRequest struct {
    Identity    string `json:"identity"`
    IdentityType string `json:"identity_type"` // "global_meta_id" | "address"
    StartHeight int64  `json:"start_height"`
    EndHeight   int64  `json:"end_height"`
}

type PinRecord struct {
    ID             string `json:"id"`
    MetaId         string `json:"metaid,omitempty"`
    GlobalMetaId   string `json:"globalMetaId,omitempty"`
    Address        string `json:"address,omitempty"`
    Path           string `json:"path"`
    Operation      string `json:"operation,omitempty"`
    GenesisHeight  int64  `json:"genesisHeight"`
    Timestamp      int64  `json:"timestamp"`
    ChainName      string `json:"chainName"`
    ContentType    string `json:"contentType"`
    Content        string `json:"content,omitempty"`
    ContentSummary string `json:"contentSummary,omitempty"`
    Status         int    `json:"status"`
}

type ExportMeta struct {
    ExportVersion  int              `json:"exportVersion"`
    ExportedAt     int64            `json:"exportedAt"`
    Identity       string           `json:"identity"`
    IdentityType   string           `json:"identityType"`
    BlockRange     BlockRange       `json:"blockRange"`
    TotalPins      int              `json:"totalPins"`
    MonthlyBreakdown []MonthSummary `json:"monthlyBreakdown"`
}

type BlockRange struct {
    Start int64 `json:"start"`
    End   int64 `json:"end"`
}

type MonthSummary struct {
    Month    string   `json:"month"`
    PinCount int      `json:"pinCount"`
    Paths    []string `json:"paths"`
}

type TimelineEntry struct {
    Month     string `json:"month"`
    PinCount  int    `json:"pinCount"`
    FirstBlock int64 `json:"firstBlock"`
    LastBlock  int64 `json:"lastBlock"`
}

type MonthIndex struct {
    Month    string       `json:"month"`
    PinCount int          `json:"pinCount"`
    Paths    []PathIndexEntry `json:"paths"`
}

type PathIndexEntry struct {
    Path  string `json:"path"`
    File  string `json:"file"`
    Count int    `json:"count"`
}
```

- [ ] **Add pathToFilename helper**

```go
func pathToFile(p string) string {
    // /info/name → info_name.json
    return strings.Replace(strings.TrimPrefix(p, "/"), "/", "_", -1) + ".json"
}
```

- [ ] **Add isTextContent helper**

```go
func isTextContent(contentType string) bool {
    ct := strings.ToLower(contentType)
    return strings.HasPrefix(ct, "text/") ||
        strings.Contains(ct, "charset") ||
        ct == "application/json" ||
        ct == "application/javascript"
}
```

- [ ] **Add pinToRecord converter**

```go
func pinToRecord(p *pin.PinInscription) PinRecord {
    content := ""
    summary := ""
    if isTextContent(p.ContentType) {
        content = p.Content
    } else {
        summary = p.ContentSummary
    }
    return PinRecord{
        ID:             p.Id,
        MetaId:         p.MetaId,
        GlobalMetaId:   p.GlobalMetaId,
        Address:        p.Address,
        Path:           p.Path,
        Operation:      p.Operation,
        GenesisHeight:  p.GenesisHeight,
        Timestamp:      p.Timestamp,
        ChainName:      p.ChainName,
        ContentType:    p.ContentType,
        Content:        content,
        ContentSummary: summary,
        Status:         p.Status,
    }
}
```

- [ ] **Run to compile**

```bash
CGO_ENABLED=0 go build ./export/
```

Expected: no errors

- [ ] **Commit**

```bash
git add export/types.go
git commit -m "feat: add export core types"
```

---

### Task 2: PebbleDB query — `export/query.go`

**Create:** `export/query.go`

- [ ] **Write the query function**

```go
package export

import (
    "fmt"
    "man-p2p/common"
    "man-p2p/pebblestore"
    "man-p2p/pin"
    "strconv"
    "strings"

    "github.com/bytedance/sonic"
    "github.com/cockroachdb/pebble"
)

const metaIdPrefixParts = 4 // "MetaId&PathMetaId&time_chain_height&PinId" split by _

// QueryUserPins returns all PinInscription for identity within block range.
// identity_type: "global_meta_id" or "address"
func QueryUserPins(db *pebblestore.Database, identity, identityType string, startHeight, endHeight int64) ([]*pin.PinInscription, error) {
    metaIds, err := resolveMetaIds(db, identity, identityType)
    if err != nil {
        return nil, err
    }
    if len(metaIds) == 0 {
        return nil, nil
    }

    seen := make(map[string]bool)
    var result []*pin.PinInscription

    for _, metaId := range metaIds {
        pins, err := queryByMetaId(db, metaId, startHeight, endHeight, seen)
        if err != nil {
            return nil, err
        }
        result = append(result, pins...)
    }
    return result, nil
}

func resolveMetaIds(db *pebblestore.Database, identity, identityType string) ([]string, error) {
    switch identityType {
    case "address":
        metaId := common.GetMetaIdByAddress(identity)
        if metaId == "" {
            return nil, nil
        }
        return []string{metaId}, nil
    case "global_meta_id":
        return resolveByGlobalMetaId(db, identity)
    default:
        return nil, fmt.Errorf("unknown identity_type: %s", identityType)
    }
}

func resolveByGlobalMetaId(db *pebblestore.Database, globalMetaId string) ([]string, error) {
    iter, err := db.MetaidInfoDB.NewIter(nil)
    if err != nil {
        return nil, err
    }
    defer iter.Close()

    var result []string
    for iter.First(); iter.Valid(); iter.Next() {
        var info pin.MetaIdInfo
        if err := sonic.Unmarshal(iter.Value(), &info); err != nil {
            continue
        }
        if info.GlobalMetaId == globalMetaId {
            result = append(result, info.MetaId)
        }
    }
    return result, nil
}

func queryByMetaId(db *pebblestore.Database, metaId string, startHeight, endHeight int64, seen map[string]bool) ([]*pin.PinInscription, error) {
    prefix := metaId + "&"
    iter, err := db.AddressDB.NewIter(&pebble.IterOptions{
        LowerBound: []byte(prefix),
        UpperBound: []byte(metaId + "&\xff\xff\xff\xff\xff\xff\xff\xff\xff\xff"),
    })
    if err != nil {
        return nil, err
    }
    defer iter.Close()

    var result []*pin.PinInscription
    for iter.First(); iter.Valid(); iter.Next() {
        key := string(iter.Key())
        if !strings.HasPrefix(key, prefix) {
            break
        }
        // key format: MetaId&PathMetaId&blockTime&chainName&height&PinId
        parts := strings.Split(key, "&")
        if len(parts) < 6 {
            continue
        }
        height, err := strconv.ParseInt(parts[4], 10, 64)
        if err != nil || height < startHeight || height > endHeight {
            continue
        }
        pinId := parts[5]
        if seen[pinId] {
            continue
        }
        seen[pinId] = true

        val, err := db.GetPinByKey(pinId)
        if err != nil {
            continue
        }
        var p pin.PinInscription
        if err := sonic.Unmarshal(val, &p); err != nil {
            continue
        }
        result = append(result, &p)
    }
    return result, nil
}
```

- [ ] **Run to compile**

```bash
CGO_ENABLED=0 go build ./export/
```

Expected: no errors

- [ ] **Commit**

```bash
git add export/query.go
git commit -m "feat: add export PebbleDB query"
```

---

### Task 3: Streaming ZIP archive — `export/archive.go`

**Create:** `export/archive.go`

- [ ] **Write the zip builder**

```go
package export

import (
    "archive/zip"
    "encoding/json"
    "fmt"
    "io"
    "man-p2p/pin"
    "sort"
    "strings"
    "time"
)

const exportVersion = 1

func WriteArchive(w io.Writer, pins []*pin.PinInscription, req *ExportRequest) error {
    zw := zip.NewWriter(w)
    defer zw.Close()

    records := make([]PinRecord, 0, len(pins))
    for _, p := range pins {
        records = append(records, pinToRecord(p))
    }

    // sort by timestamp ascending
    sort.SliceStable(records, func(i, j int) bool {
        return records[i].Timestamp < records[j].Timestamp
    })

    // group by month → path
    monthly := groupByMonth(records)

    // write metadata
    if err := writeMeta(zw, req, records, monthly); err != nil {
        return err
    }

    // write timeline
    if err := writeTimeline(zw, records); err != nil {
        return err
    }

    // write per-month directories
    for _, month := range sortedMonths(monthly) {
        groups := monthly[month]
        // write _index.json
        if err := writeMonthIndex(zw, month, groups); err != nil {
            return err
        }
        // write per-path files
        for _, path := range sortedPaths(groups) {
            recs := groups[path]
            if err := writePathFile(zw, month, path, recs); err != nil {
                return err
            }
        }
    }
    return nil
}

type monthGroup map[string]map[string][]PinRecord

func groupByMonth(records []PinRecord) monthGroup {
    result := make(monthGroup)
    for _, r := range records {
        month := time.Unix(r.Timestamp, 0).Format("2006-01")
        if result[month] == nil {
            result[month] = make(map[string][]PinRecord)
        }
        result[month][r.Path] = append(result[month][r.Path], r)
    }
    return result
}

func sortedMonths(m monthGroup) []string {
    months := make([]string, 0, len(m))
    for k := range m {
        months = append(months, k)
    }
    sort.Strings(months)
    return months
}

func sortedPaths(groups map[string][]PinRecord) []string {
    paths := make([]string, 0, len(groups))
    for k := range groups {
        paths = append(paths, k)
    }
    sort.Strings(paths)
    return paths
}

func writeMeta(zw *zip.Writer, req *ExportRequest, records []PinRecord, monthly monthGroup) error {
    f, err := zw.Create("export.json")
    if err != nil {
        return err
    }
    breakdown := make([]MonthSummary, 0)
    for _, month := range sortedMonths(monthly) {
        groups := monthly[month]
        paths := make([]string, 0, len(groups))
        total := 0
        for p, recs := range groups {
            paths = append(paths, p)
            total += len(recs)
        }
        sort.Strings(paths)
        breakdown = append(breakdown, MonthSummary{
            Month:    month,
            PinCount: total,
            Paths:    paths,
        })
    }
    meta := ExportMeta{
        ExportVersion:    exportVersion,
        ExportedAt:       time.Now().Unix(),
        Identity:         req.Identity,
        IdentityType:     req.IdentityType,
        BlockRange:       BlockRange{Start: req.StartHeight, End: req.EndHeight},
        TotalPins:        len(records),
        MonthlyBreakdown: breakdown,
    }
    return json.NewEncoder(f).Encode(meta)
}

func writeTimeline(zw *zip.Writer, records []PinRecord) error {
    f, err := zw.Create("timeline.json")
    if err != nil {
        return err
    }

    monthly := make(map[string]*TimelineEntry)
    for _, r := range records {
        month := time.Unix(r.Timestamp, 0).Format("2006-01")
        if monthly[month] == nil {
            monthly[month] = &TimelineEntry{Month: month}
        }
        monthly[month].PinCount++
        if monthly[month].FirstBlock == 0 || r.GenesisHeight < monthly[month].FirstBlock {
            monthly[month].FirstBlock = r.GenesisHeight
        }
        if r.GenesisHeight > monthly[month].LastBlock {
            monthly[month].LastBlock = r.GenesisHeight
        }
    }

    months := make([]string, 0, len(monthly))
    for k := range monthly {
        months = append(months, k)
    }
    sort.Strings(months)

    entries := make([]TimelineEntry, 0, len(months))
    for _, m := range months {
        entries = append(entries, *monthly[m])
    }

    return json.NewEncoder(f).Encode(map[string]interface{}{
        "totalPins":       len(records),
        "monthlyActivity": entries,
    })
}

func writeMonthIndex(zw *zip.Writer, month string, groups map[string][]PinRecord) error {
    f, err := zw.Create(fmt.Sprintf("%s/_index.json", month))
    if err != nil {
        return err
    }
    idx := MonthIndex{
        Month:    month,
        PinCount: 0,
        Paths:    make([]PathIndexEntry, 0),
    }
    for _, path := range sortedPaths(groups) {
        recs := groups[path]
        idx.PinCount += len(recs)
        idx.Paths = append(idx.Paths, PathIndexEntry{
            Path:  path,
            File:  pathToFile(path),
            Count: len(recs),
        })
    }
    return json.NewEncoder(f).Encode(idx)
}

func writePathFile(zw *zip.Writer, month, path string, records []PinRecord) error {
    f, err := zw.Create(fmt.Sprintf("%s/%s", month, pathToFile(path)))
    if err != nil {
        return err
    }
    return json.NewEncoder(f).Encode(records)
}
```

- [ ] **Run to compile**

```bash
CGO_ENABLED=0 go build ./export/
```

Expected: no errors

- [ ] **Commit**

```bash
git add export/archive.go
git commit -m "feat: add streaming ZIP archive builder"
```

---

### Task 4: Top-level orchestrator — `export/export.go`

**Create:** `export/export.go`

- [ ] **Write the orchestrator**

```go
package export

import (
    "fmt"
    "io"
    "man-p2p/man"
    "man-p2p/pin"
)

const maxPins = 500000

func ExportUserData(w io.Writer, req *ExportRequest) error {
    if err := validateRequest(req); err != nil {
        return err
    }

    db := man.PebbleStore.Database
    if db == nil {
        return fmt.Errorf("database not initialized")
    }

    pins, err := QueryUserPins(db, req.Identity, req.IdentityType, req.StartHeight, req.EndHeight)
    if err != nil {
        return fmt.Errorf("query failed: %w", err)
    }

    if len(pins) > maxPins {
        return fmt.Errorf("export exceeds %d pins (%d total), please narrow the block range", maxPins, len(pins))
    }

    return WriteArchive(w, pins, req)
}

func validateRequest(req *ExportRequest) error {
    if req.Identity == "" {
        return fmt.Errorf("identity is required")
    }
    if req.IdentityType != "global_meta_id" && req.IdentityType != "address" {
        return fmt.Errorf("identity_type must be 'global_meta_id' or 'address'")
    }
    if req.StartHeight <= 0 || req.EndHeight <= 0 {
        return fmt.Errorf("start_height and end_height must be positive")
    }
    if req.StartHeight > req.EndHeight {
        return fmt.Errorf("start_height must not exceed end_height")
    }
    return nil
}
```

- [ ] **Run to compile**

```bash
CGO_ENABLED=0 go build ./export/
```

Expected: no errors

- [ ] **Commit**

```bash
git add export/export.go
git commit -m "feat: add export orchestrator"
```

---

### Task 5: Gin handler + route — `api/export_handler.go`

**Create:** `api/export_handler.go`

- [ ] **Write the handler**

```go
package api

import (
    "fmt"
    "io"
    "man-p2p/api/respond"
    "man-p2p/export"
    "net/http"

    "github.com/gin-gonic/gin"
)

func RegisterExportRoutes(r *gin.Engine) {
    r.POST("/api/export/user-data", exportUserData)
}

func exportUserData(ctx *gin.Context) {
    var req export.ExportRequest
    if err := ctx.ShouldBindJSON(&req); err != nil {
        ctx.JSON(http.StatusBadRequest, respond.ApiError(400, "invalid request: "+err.Error()))
        return
    }

    pr, pw := io.Pipe()
    go func() {
        err := export.ExportUserData(pw, &req)
        pw.CloseWithError(err)
    }()

    filename := fmt.Sprintf("userdata_%s_%d_%d.zip", req.Identity, req.StartHeight, req.EndHeight)
    ctx.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
    ctx.Header("Content-Type", "application/zip")
    ctx.Header("X-Accel-Buffering", "no")
    ctx.Status(http.StatusOK)

    io.Copy(ctx.Writer, pr)
    pr.Close()
}
```

- [ ] **Run to compile**

```bash
CGO_ENABLED=0 go build ./api/
```

Expected: no errors

- [ ] **Commit**

```bash
git add api/export_handler.go
git commit -m "feat: add export API handler"
```

---

### Task 6: Wire route into webapi.go

**Modify:** `api/webapi.go`

- [ ] **Add call after RegisterP2PRoutes**

At line 132 of `api/webapi.go` (after `RegisterP2PRoutes(r)`), add:

```go
	// P2P routes
	RegisterP2PRoutes(r)
	// Export routes
	RegisterExportRoutes(r)
```

- [ ] **Verify compile**

```bash
CGO_ENABLED=0 go build .
```

Expected: no errors

- [ ] **Commit**

```bash
git add api/webapi.go
git commit -m "feat: wire export route into server"
```

---

### Task 7: Verify MAN timestamp coverage

The design relies on `PinInscription.Timestamp` being populated for every PIN. Check the indexing pipeline.

- [ ] **Search for where Timestamp is set during indexing**

```bash
grep -rn '\.Timestamp\s*=' man/*.go | head -20
```

- [ ] **Verify that Timestamp is always set in SetAllPins or BatchInsertPins**

Look for the code path. If missing, add a fallback in `pebblestore/data.go` `SetAllPins` that copies the `blockTime` param to `p.Timestamp` when `p.Timestamp == 0`.

- [ ] **Commit any fix**

```bash
git add pebblestore/data.go
git commit -m "fix: ensure PinInscription.Timestamp is populated during indexing"
```

---

### Task 8: Tests — `export/export_test.go`

**Create:** `export/export_test.go`

- [ ] **Write test for query + archive with in-memory PebbleDB**

```go
package export

import (
    "bytes"
    "archive/zip"
    "encoding/json"
    "io"
    "man-p2p/common"
    "man-p2p/man"
    "man-p2p/pebblestore"
    "man-p2p/pin"
    "os"
    "testing"
)

func setupTestDB(t *testing.T) *pebblestore.Database {
    t.Helper()
    dir, err := os.MkdirTemp("", "export-test-*")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { os.RemoveAll(dir) })
    db, err := pebblestore.NewDataBase(dir, 2)
    if err != nil {
        t.Fatal(err)
    }
    return db
}

func insertTestPin(t *testing.T, db *pebblestore.Database, p *pin.PinInscription) {
    t.Helper()
    // store in PinsDBs
    err := db.BatchInsertPins([]pin.PinInscription{*p})
    if err != nil {
        t.Fatal(err)
    }
    // add address index
    metaId := common.GetMetaIdByAddress(p.Address)
    if metaId == "" {
        metaId = p.MetaId
    }
    addrKey := p.MetaId + "&" + common.GetMetaIdByAddress(p.Path) + "&" +
        pin.GetPublicKeyStr(p.Timestamp, p.ChainName, p.GenesisHeight) + "&" + p.Id
    db.BatchSetAddressData(&[]string{addrKey})
}

func TestQueryUserPinsByAddress(t *testing.T) {
    db := setupTestDB(t)
    addr := "bc1test"
    metaId := common.GetMetaIdByAddress(addr)

    // simulate 3 pins at different heights
    for i := int64(0); i < 3; i++ {
        p := &pin.PinInscription{
            Id:            fmt.Sprintf("pin_%d", i),
            MetaId:        metaId,
            Address:       addr,
            Path:          "/info/name",
            Operation:     "create",
            Timestamp:     1700000000 + i*100000,
            GenesisHeight: 800000 + i,
            ChainName:     "bitcoin",
            ContentType:   "text/plain;charset=utf-8",
            Content:       fmt.Sprintf("name_%d", i),
            ContentSummary: "",
            Status:        0,
        }
        insertTestPin(t, db, p)
    }

    // use man.PebbleStore to access the db
    orig := man.PebbleStore
    man.PebbleStore = &man.PebbleData{Database: db}
    defer func() { man.PebbleStore = orig }()

    pins, err := QueryUserPins(db, addr, "address", 800001, 800002)
    if err != nil {
        t.Fatal(err)
    }
    if len(pins) != 2 {
        t.Fatalf("expected 2 pins, got %d", len(pins))
    }
}

func TestQueryUserPinsByGlobalMetaId(t *testing.T) {
    db := setupTestDB(t)
    gmid := "gm_test_user"

    // insert MetaIdInfo for two metaIds under same GlobalMetaId
    db.BatchSetMetaidInfo(&map[string]*pin.MetaIdInfo{
        "meta_btc": {MetaId: "meta_btc", Address: "bc1aaa", GlobalMetaId: gmid, ChainName: "bitcoin"},
        "meta_doge": {MetaId: "meta_doge", Address: "D9bbb", GlobalMetaId: gmid, ChainName: "dogecoin"},
    })

    for _, id := range []string{"meta_btc", "meta_doge"} {
        p := &pin.PinInscription{
            Id:            "pin_" + id,
            MetaId:        id,
            Address:       "addr_" + id,
            Path:          "/info/bio",
            Operation:     "create",
            Timestamp:     1700000000,
            GenesisHeight: 800000,
            ChainName:     "bitcoin",
            ContentType:   "text/plain;charset=utf-8",
            Content:       "hello",
            Status:        0,
        }
        insertTestPin(t, db, p)
    }

    orig := man.PebbleStore
    man.PebbleStore = &man.PebbleData{Database: db}
    defer func() { man.PebbleStore = orig }()

    pins, err := QueryUserPins(db, gmid, "global_meta_id", 0, 999999)
    if err != nil {
        t.Fatal(err)
    }
    if len(pins) != 2 {
        t.Fatalf("expected 2 pins (btc+doge), got %d", len(pins))
    }
}

func TestArchiveStructure(t *testing.T) {
    pins := []*pin.PinInscription{
        {
            Id: "p1", MetaId: "m1", Path: "/info/name", GenesisHeight: 800000,
            Timestamp: 1700000000, ChainName: "bitcoin",
            ContentType: "text/plain", Content: "Alice",
        },
        {
            Id: "p2", MetaId: "m1", Path: "/protocols/test", GenesisHeight: 800100,
            Timestamp: 1700100000, ChainName: "bitcoin",
            ContentType: "text/plain", Content: "data",
        },
    }

    var buf bytes.Buffer
    err := WriteArchive(&buf, pins, &ExportRequest{
        Identity: "m1", IdentityType: "address",
        StartHeight: 800000, EndHeight: 900000,
    })
    if err != nil {
        t.Fatal(err)
    }

    zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
    if err != nil {
        t.Fatal(err)
    }

    files := make(map[string]bool)
    for _, f := range zr.File {
        files[f.Name] = true
    }

    expected := []string{
        "export.json",
        "timeline.json",
        "2023-11/_index.json",
        "2023-11/info_name.json",
        "2023-11/protocols_test.json",
    }
    for _, e := range expected {
        if !files[e] {
            t.Errorf("missing expected file: %s", e)
        }
    }

    // verify export.json content
    rc, err := zr.Open("export.json")
    if err != nil {
        t.Fatal(err)
    }
    defer rc.Close()
    var meta ExportMeta
    if err := json.NewDecoder(rc).Decode(&meta); err != nil {
        t.Fatal(err)
    }
    if meta.TotalPins != 2 {
        t.Errorf("expected TotalPins=2, got %d", meta.TotalPins)
    }
}

func TestExportValidator(t *testing.T) {
    tests := []struct {
        name string
        req  ExportRequest
        want string
    }{
        {"empty identity", ExportRequest{Identity: "", IdentityType: "address", StartHeight: 1, EndHeight: 10}, "identity is required"},
        {"bad type", ExportRequest{Identity: "x", IdentityType: "foo", StartHeight: 1, EndHeight: 10}, "identity_type"},
        {"negative height", ExportRequest{Identity: "x", IdentityType: "address", StartHeight: -1, EndHeight: 10}, "positive"},
        {"start > end", ExportRequest{Identity: "x", IdentityType: "address", StartHeight: 10, EndHeight: 5}, "must not exceed"},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            err := validateRequest(&tc.req)
            if err == nil || !strings.Contains(err.Error(), tc.want) {
                t.Errorf("expected error containing %q, got %v", tc.want, err)
            }
        })
    }
}

func TestContentClassification(t *testing.T) {
    tests := []struct {
        ct    string
        isText bool
    }{
        {"text/plain;charset=utf-8", true},
        {"application/json", true},
        {"image/png", false},
        {"application/octet-stream", false},
    }
    for _, tc := range tests {
        p := &pin.PinInscription{ContentType: tc.ct, Content: "data", ContentSummary: "img"}
        rec := pinToRecord(p)
        if tc.isText && rec.Content == "" {
            t.Errorf("expected content for %s", tc.ct)
        }
        if !tc.isText && rec.ContentSummary == "" {
            t.Errorf("expected summary for %s", tc.ct)
        }
    }
}
```

- [ ] **Run tests**

```bash
CGO_ENABLED=0 go test ./export/ -v -count=1
```

Expected: All tests PASS

- [ ] **Commit**

```bash
git add export/export_test.go
git commit -m "feat: add export tests"
```

---

### Task 9: Integration test — API handler

- [ ] **Add integration test in `api/export_handler_test.go`**

```go
package api

import (
    "archive/zip"
    "bytes"
    "encoding/json"
    "io"
    "man-p2p/man"
    "man-p2p/pebblestore"
    "man-p2p/pin"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/gin-gonic/gin"
)

func setupExportTestDB(t *testing.T) *pebblestore.Database {
    t.Helper()
    dir, err := os.MkdirTemp("", "export-test-*")
    if err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { os.RemoveAll(dir) })
    db, err := pebblestore.NewDataBase(dir, 2)
    if err != nil {
        t.Fatal(err)
    }
    return db
}

func TestExportEndpoint(t *testing.T) {
    db := setupExportTestDB(t)

    metaId := "test_meta"
    addr := "bc1test"
    now := int64(1700000000)

    // insert a test PIN
    p := &pin.PinInscription{
        Id: "pin_export_test", MetaId: metaId, Address: addr,
        Path: "/info/name", Operation: "create",
        Timestamp: now, GenesisHeight: 800000,
        ChainName: "bitcoin",
        ContentType: "text/plain;charset=utf-8",
        Content: "TestUser", Status: 0,
    }
    err := db.BatchInsertPins([]pin.PinInscription{*p})
    if err != nil {
        t.Fatal(err)
    }
    addrKey := p.MetaId + "&" + "path_meta" + "&" + "01700000000_bitcoin_0000800000&" + p.Id
    db.BatchSetAddressData(&[]string{addrKey})

    orig := man.PebbleStore
    man.PebbleStore = &man.PebbleData{Database: db}
    defer func() { man.PebbleStore = orig }()

    gin.SetMode(gin.TestMode)
    r := gin.New()
    RegisterExportRoutes(r)

    body, _ := json.Marshal(map[string]interface{}{
        "identity":      addr,
        "identity_type": "address",
        "start_height":  700000,
        "end_height":    900000,
    })

    req := httptest.NewRequest("POST", "/api/export/user-data", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    w := httptest.NewRecorder()
    r.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
    }

    ct := w.Header().Get("Content-Type")
    if ct != "application/zip" {
        t.Errorf("expected application/zip, got %s", ct)
    }

    zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
    if err != nil {
        t.Fatal(err)
    }

    found := false
    for _, f := range zr.File {
        if f.Name == "export.json" {
            found = true
            rc, _ := f.Open()
            defer rc.Close()
            var meta struct {
                TotalPins int `json:"totalPins"`
            }
            json.NewDecoder(rc).Decode(&meta)
            if meta.TotalPins != 1 {
                t.Errorf("expected TotalPins=1, got %d", meta.TotalPins)
            }
        }
    }
    if !found {
        t.Error("export.json not found in archive")
    }
}
```

- [ ] **Run integration test**

```bash
CGO_ENABLED=0 go test ./api/ -run TestExportEndpoint -v -count=1
```

Expected: PASS

- [ ] **Commit**

```bash
git add api/export_handler_test.go
git commit -m "feat: add export API integration test"
```

---

### Task 10: Run full test suite

- [ ] **Run all export + api tests**

```bash
CGO_ENABLED=0 go test ./export/... ./api/... -v -count=1
```

Expected: All PASS

- [ ] **Run the existing alpha test suite to verify no regressions**

```bash
make alpha-test
```

Expected: All PASS

---

### Task 11: Final verification — build + smoke test

- [ ] **Build release binary**

```bash
make all
```

Expected: binaries under `dist/`

- [ ] **Manual smoke test (if local data available)**

```bash
CGO_ENABLED=0 go run . -config ./config.toml -server=1 -p2p-config ./p2p-config.json -data-dir ./man_p2p_data
```

In another terminal:
```bash
curl -X POST http://localhost:8088/api/export/user-data \
  -H 'Content-Type: application/json' \
  -d '{"identity":"YOUR_METAID_OR_ADDRESS","identity_type":"address","start_height":0,"end_height":9999999}' \
  -o test_export.zip
unzip -l test_export.zip
```

Expected: Valid ZIP with month directories and PIN files
