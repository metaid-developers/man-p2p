# User Data Export — Design Spec

Export a ZIP archive containing all PIN data for a given identity (GlobalMetaID or address) within a block height range, organized for AI/LLM consumption.

**Date**: 2026-06-06  
**Status**: Draft

---

## 1. API

```
POST /api/export/user-data
Content-Type: application/json

{
  "identity": "gm_xxx | bc1...",
  "identity_type": "global_meta_id | address",
  "start_height": 800000,
  "end_height": 900000
}
```

**Response**:
```
200 OK
Content-Type: application/zip
Content-Disposition: attachment; filename="userdata_<identity>_<start>_<end>.zip"
```

**Errors**:
- `400` — invalid parameters (start > end, missing identity, bad identity_type)
- `404` — identity not found on any chain
- `500` — internal error during query or archive generation

The response is streamed — no full buffering of the ZIP in memory.

---

## 2. Archive Structure

```
userdata_<identity>_<startHeight>_<endHeight>.zip
├── export.json           # Top-level metadata
├── 2024-01/              # Month directory (YYYY-MM)
│   ├── _index.json       # Path list for this month
│   ├── info_name.json    # Path → filename: strip leading /, replace / with _
│   ├── info_bio.json
│   ├── protocols_metaprotocol.json
│   ├── ft_mrc20.json
│   └── ...
├── 2024-02/
│   └── ...
└── timeline.json         # Activity summary by month
```

### export.json (metadata)

```json
{
  "exportVersion": 1,
  "exportedAt": 1719000000,
  "identity": "gm_xxx | bc1...",
  "identityType": "global_meta_id | address",
  "blockRange": { "start": 800000, "end": 900000 },
  "totalPins": 1523,
  "monthlyBreakdown": [
    { "month": "2024-01", "pinCount": 45, "paths": ["info/name", "protocols/metaprotocol"] },
    { "month": "2024-02", "pinCount": 120, "paths": ["info/bio", "ft/mrc20"] }
  ]
}
```

### _index.json (per month)

```json
{
  "month": "2024-01",
  "pinCount": 45,
  "paths": [
    { "path": "/info/name", "file": "info_name.json", "count": 3 },
    { "path": "/protocols/metaprotocol", "file": "protocols_metaprotocol.json", "count": 20 }
  ]
}
```

### timeline.json

```json
{
  "identity": "gm_xxx",
  "totalPins": 1523,
  "blockRange": { "start": 800000, "end": 900000 },
  "monthlyActivity": [
    { "month": "2024-01", "pinCount": 45, "firstBlock": 800100, "lastBlock": 800900 },
    { "month": "2024-02", "pinCount": 120, "firstBlock": 801000, "lastBlock": 802500 }
  ]
}
```

---

## 3. PIN Record Format

Each per-path JSON file contains an array of PIN records sorted by timestamp ascending.

### Text content

```json
{
  "id": "a1b2c3...",
  "metaid": "0x1234...",
  "globalMetaId": "gm_xxx",
  "address": "bc1...",
  "path": "/info/name",
  "operation": "create",
  "genesisHeight": 850000,
  "timestamp": 1719000000,
  "chainName": "bitcoin",
  "contentType": "text/plain;charset=utf-8",
  "content": "full text content...",
  "contentSummary": "",
  "status": 0
}
```

### Binary content (images, files, etc.)

```json
{
  "id": "d4e5f6...",
  "metaid": "0x1234...",
  "globalMetaId": "gm_xxx",
  "address": "bc1...",
  "path": "/info/avatar",
  "operation": "create",
  "genesisHeight": 851000,
  "timestamp": 1719100000,
  "chainName": "bitcoin",
  "contentType": "image/png",
  "content": "",
  "contentSummary": "Avatar image, 256x256, 45KB",
  "status": 0
}
```

**Classification rule**: `content` is included iff `contentType` starts with `text/`, contains `charset`, or is `application/json`. Otherwise only `contentSummary` is included.

---

## 4. Data Query Flow

```
Request
  └─ identity_type = global_meta_id
       └─ MetaidInfoDB: iterate all entries, filter by GlobalMetaId
       └─ collect all matching MetaId values
  └─ identity_type = address
       └─ common.GetMetaIdByAddress(address) → MetaId
  └─ for each MetaId:
       └─ iterate AddressDB with prefix "MetaId&"
            └─ parse key to extract height (last component after _)
            └─ if height in [start_height, end_height]:
                 └─ GetPinByKey(pinId) from PinsDBs
                 └─ collect into month bucket by timestamp
  └─ for each month:
       └─ group pins by path
       └─ write _index.json
       └─ write each path's JSON file
  └─ write export.json
  └─ write timeline.json
  └─ stream ZIP to response
```

**Timestamp guarantee**: MAN indexing must populate `PinInscription.Timestamp` for every PIN. If missing, the MAN indexer path must be fixed to call `Chain.GetBlockTime(height)` during indexing.

---

## 5. Architecture (New & Changed Files)

### New files

| File | Purpose |
|------|---------|
| `api/export_handler.go` | Gin handler: parse request, validate, stream ZIP |
| `export/userdata.go` | Core logic: query, organize, build archive entries |
| `export/format.go` | PIN → export JSON model, content classification |
| `export/archive.go` | Streaming ZIP writer abstraction |
| `export/export_test.go` | Unit + integration tests |

### Changed files

| File | Change |
|------|--------|
| `api/webapi.go` | Register `POST /api/export/user-data` route |
| `go.mod` | Add `archive/zip` is stdlib, no new deps |

---

## 6. Streaming Strategy

Use Go's `archive/zip` + `io.Pipe` to stream the archive through `http.ResponseWriter`:

```
goroutine 1: query + organize → write ZIP entries → pipe writer
goroutine 2 (HTTP handler): read from pipe reader → write to http.ResponseWriter
```

This avoids buffering the full archive in memory. If the export is large (>100MB estimated), add periodic `Flush()` to prevent timeout.

---

## 7. Edge Cases

- **Identity not found**: Return 404 with `{"error": "identity not found"}`.
- **No PINs in range**: Return valid ZIP with `totalPins: 0`, no month directories.
- **Very large exports** (millions of PINs): Streaming ensures O(1) memory. Consider adding an upper limit (e.g., 500K PINs) and returning a 413 with suggestion to narrow range.
- **Corrupt PIN data in store**: Skip individual corrupt entries, log warning, continue.
- **Concurrent requests**: Each export is isolated — no shared state. Optionally add a per-IP rate limit (1 req/30s) to prevent abuse.
- **Mempool PINs**: Excluded. Only confirmed (indexed) PINs are exported.
- **PATH → filename collision**: Since `/` → `_`, paths like `/a/b` and `/a_b` would collide. This is unlikely in practice (MetaID paths are semantic). Document as accepted limitation.

---

## 8. Future Considerations

- **Format options**: Could add `?format=jsonl` to emit a single JSONL file instead of per-path files.
- **Content inclusion**: Could add `?include_content=true|false` to let the caller choose.
- **Incremental export**: Save a cursor so subsequent exports only include new data since last export.
- **Direct file upload**: Option to upload the archive directly to IPFS / MetaWeb storage instead of downloading.
