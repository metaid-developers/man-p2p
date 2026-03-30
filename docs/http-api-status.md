# HTTP API Status

Last reviewed: 2026-03-28

This document summarizes the HTTP surface exposed by the current `man-p2p` binary.

Status meanings:

- `implemented`: route is mounted and handler returns a real response
- `stub`: route is mounted but handler is empty, commented out, or effectively unusable
- `not mounted`: handler code exists, but startup does not register the route

Route registration sources:

- `api/webapi.go`
- `api/btc_jsonapi.go`
- `api/p2p_api.go`
- `api/mrc20.go`

Important runtime note:

- `mrc20JsonApi(r)` is currently commented out in `api/webapi.go`, so the full `/api/mrc20/*` family is present in code but not exposed by the running server.
- In `p2p-only` mode, chain adapters are not initialized. Any route depending on `man.ChainAdapter[...]` or `man.IndexerAdapter[...]` may be mounted but not reliable at runtime.

## Common Sort Parameters

Many list APIs support the same query pattern:

- `sortBy=<field>`
- `order=asc|desc`
- `sortOrder=asc|desc`

Rules:

- `order` and `sortOrder` are aliases
- default order is descending
- if `sortBy` is omitted, each handler chooses its own default field

Implementation caveat:

- Most handlers fetch one page from Pebble first, then sort that page in memory.
- That means non-default sorting is only guaranteed within the current page payload.
- When combined with cursor pagination, `nextCursor` usually still follows the original storage order, not the requested `sortBy`.

## Mounted JSON And Content APIs

| Method | Path | Parameters | Status | Notes |
|---|---|---|---|---|
| `GET` | `/health` | none | `implemented` | Health check |
| `POST` | `/api/config/reload` | none | `implemented` | If process did not start with `-p2p-config`, returns success but effectively does nothing |
| `GET` | `/api/p2p/status` | none | `implemented` | `syncProgress` is currently hardcoded to `0.0` |
| `GET` | `/api/p2p/peers` | none | `implemented` | Returns peer ID list |
| `GET` | `/api/metaid/list` | query: `page` req int64, `size` req int64, `sortBy` opt `number|pdv|fdv|followcount|name|metaid|address|pinid|id`, `order/sortOrder` opt `asc|desc` | `implemented` | Default sort field is `number` |
| `GET` | `/api/pin/list` | query: `page` req int, `size` req int, `lastId` opt string, `sortBy` opt `time|timestamp|number|height|blockheight|id|metaid|path`, `order/sortOrder` opt `asc|desc` | `implemented` | Default sort field is `timestamp` |
| `GET` | `/api/block/list` | none currently read by handler | `stub` | Handler body is TODO only |
| `GET` | `/api/mempool/list` | query: `page` req int64, `size` req int64, `sortBy` opt `time|timestamp|number|height|blockheight|id|metaid|path`, `order/sortOrder` opt `asc|desc` | `implemented` | Default sort field is `timestamp` |
| `GET` | `/api/notifcation/list` | query: `address` req string, `lastId` opt int64 default `0`, `size` opt int default `20` when invalid, `sortBy` opt `time|timestamp|notifcationtime|id|notifcationid|frompinid`, `order/sortOrder` opt `asc|desc` | `implemented` | Spelling is `notifcation` in code and route |
| `GET` | `/api/block/file` | query: `height` req int64, `chain` req string, `part` opt int default `0` | `implemented` | Downloads a block shard file |
| `GET` | `/api/block/file/partCount` | query: `height` req int64, `chain` req string | `implemented` | Returns shard count plus btc/mvc file ranges |
| `GET` | `/api/block/file/create` | query: `token` req string and must equal `AdminToken`, `chain` req string, `from` opt int64, `to` opt int64 | `implemented` | Admin endpoint; weak validation on `from/to` |
| `GET` | `/api/block/id/list` | query: `token` req string and must equal `AdminToken`, `chain` req string, `height` req int64 | `implemented` | Admin endpoint |
| `GET` | `/api/block/id/create` | query: `token` req string and must equal `AdminToken`, `chain` req string, `from` opt int64, `to` opt int64 | `implemented` | Admin endpoint; weak validation on `from/to` |
| `GET` | `/api/pin/:numberOrId` | path: `numberOrId` req string | `implemented` | Local miss returns HTTP `404` |
| `GET` | `/api/pin/ver/:pinid/:ver` | path: `pinid` req string, `ver` req int | `implemented` | Invalid version returns empty payload rather than error |
| `GET` | `/api/pin/path/list` | query: `path` req string, `size` req int64 with clamp to `20` when invalid, `cursor` opt string, `sortBy` opt `time|timestamp|height|blockheight|genesisheight|number|id|metaid|path`, `order/sortOrder` opt `asc|desc` | `implemented` | Default sort field is `timestamp` |
| `GET` | `/api/address/pin/list/:address` | path: `address` req string; query: `path` req string, `size` req int64 with clamp to `20` when invalid, `cursor` opt string, `sortBy` opt `time|timestamp|height|blockheight|genesisheight|number|id|metaid|path`, `order/sortOrder` opt `asc|desc` | `implemented` | Default sort field is `timestamp`; sorting exists in code, but is page-local in-memory sorting after DB fetch |
| `GET` | `/api/metaid/pin/list/:metaid` | path: `metaid` req string; query: `cursor` req string, `path` req string, `size` req int64 with clamp to `20` when invalid, `sortBy` opt `time|timestamp|height|blockheight|genesisheight|number|id|metaid|path`, `order/sortOrder` opt `asc|desc` | `implemented` | `cursor` is treated as required by handler |
| `GET` | `/api/info/address/:address` | path: `address` req string | `implemented` | Returns MetaID info by address |
| `GET` | `/api/info/metaid/:metaId` | path: `metaId` req string | `implemented` | Returns MetaID info by metaid |
| `GET` | `/api/v1/users/info/address/:address` | path: `address` req string | `implemented` | Alias of `/api/info/address/:address` |
| `GET` | `/api/v1/users/info/metaid/:metaId` | path: `metaId` req string | `implemented` | Alias of `/api/info/metaid/:metaId` |
| `GET` | `/content/:number` | path: `number` req string | `implemented` | Oversized content may return `200` with `X-Man-Content-Status=metadata-only` and empty body |

## Mounted HTML And Debug Routes

| Method | Path | Parameters | Status | Notes |
|---|---|---|---|---|
| `GET` | `/swagger/*any` | path wildcard `any` | `implemented` | Swagger UI |
| `GET` | `/` | none | `implemented` | Home page |
| `GET` | `/pin/list/:page` | path: `page` req int, query: `lastId` opt string | `implemented` | HTML page |
| `GET` | `/metaid/:page` | path: `page` req int64 | `implemented` | HTML page |
| `GET` | `/blocks/:page` | path: `page` req int64 | `implemented` | Handler is live, but internal query hardcodes page `0`, so page parameter mostly affects chrome, not data selection |
| `GET` | `/mempool/:page` | path: `page` req int64 | `implemented` | HTML page |
| `GET` | `/block/:height` | path: `height` req string | `stub` | Handler is commented out |
| `GET` | `/pin/:number` | path: `number` req string | `implemented` | HTML pin page |
| `GET` | `/search/:key` | path: `key` req string | `stub` | Handler is empty |
| `GET` | `/tx/:chain/:txid` | path: `chain` req `btc|mvc|doge`, `txid` req string | `implemented` | In `p2p-only` mode this is not reliable because chain adapters may be uninitialized |
| `GET` | `/node/:rootid` | path: `rootid` req string | `stub` | Handler is commented out |
| `GET` | `/stream/:number` | path: `number` req string | `stub` | Handler is empty |
| `GET` | `/debug/count` | none | `implemented` | Direct stats dump |
| `GET` | `/debug/sync` | none | `implemented` | Reads sync heights from MetaDB |

## Code Exists But Routes Are Not Mounted: JSON MRC20 APIs

All routes below are defined in `api/mrc20.go`, but current startup does not call `mrc20JsonApi(r)`.

| Method | Path | Parameters | Status | Notes |
|---|---|---|---|---|
| `GET` | `/api/mrc20/tick/all` | query: `cursor` opt int64 default `0`, `size` opt int64 default `20`, `sortBy` opt `time|timestamp|deploytime|number|pinnumber|holders|txcount|tick|tickid|id|mrc20id`, `order/sortOrder` opt `asc|desc` | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/tick/info/:id` | path: `id` req string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/tick/info` | query: `id` opt string, `tick` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/tick/address` | query: `tickId` opt string, `address` opt string, `cursor` opt int64 default `0`, `size` opt int64 default `20`, `status` opt int, `verify` opt `true|false|1|0`, `sortBy` opt `time|timestamp|height|blockheight|amount|amtchange|status|txpoint`, `order/sortOrder` opt `asc|desc` | `not mounted` | Handler does not enforce `tickId/address` at API layer |
| `GET` | `/api/mrc20/tick/history` | query: `tickId` opt string, `cursor` opt int64 default `0`, `size` opt int64 default `20`, `sortBy` opt `time|timestamp|height|blockheight|txindex|amount|tick|tickid|txid`, `order/sortOrder` opt `asc|desc` | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/address/balance/:address` | path: `address` req string; query: `cursor` opt int64 default `0`, `size` opt int64 default `20`, `chain` opt string, `sortBy` opt `balance|pendingin|pendingout|tick|name|id|tickid`, `order/sortOrder` opt `asc|desc` | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/address/history/:tickId/:address` | path: `tickId` req string, `address` req string; query: `cursor` opt int64 default `0`, `size` opt int64 default `20`, `sortBy` opt `time|timestamp|height|blockheight|txindex|amount|tick|tickid|txid`, `order/sortOrder` opt `asc|desc` | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/tx/history` | query: `txId` req string, `index` opt int, `sortBy` opt `time|timestamp|height|blockheight|amount|amtchange|status|txpoint`, `order/sortOrder` opt `asc|desc` | `not mounted` | Returns UTXO rows |
| `GET` | `/api/mrc20/tick/AddressBalance` | query: `address` req string, `tickId` req string, `chain` opt string default `btc` | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/index-height/:chain` | path: `chain` req `btc|bitcoin|doge|dogecoin|mvc` | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/index-height/:chain/set` | path: `chain` req valid chain; query: `height` req int64, `token` opt string, `reason` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/reindex-block/:chain/:height` | path: `chain` req valid chain, `height` req int64 greater than `0`; query: `token` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/reindex-range/:chain/:start/:end` | path: `chain` req valid chain, `start` req int64 greater than `0`, `end` req int64 greater or equal to `start` and range at most `100`; query: `token` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/reindex-from/:chain/:height` | path: `chain` req valid chain, `height` req int64 greater than `0`; query: `token` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/recalculate-balance/:chain/:address/:tickId` | path: `chain` req valid chain, `address` req string, `tickId` req string; query: `token` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/verify-balance/:chain/:address/:tickId` | path: `chain` req valid chain, `address` req string, `tickId` req string; query: `token` opt string | `not mounted` | Token is logged but not strictly required |
| `GET` | `/api/mrc20/admin/fix-pending/:chain` | path: `chain` req valid chain; query: `token` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/teleport/list-pending` | none | `not mounted` | Even if mounted, handler only returns deprecated error |
| `GET` | `/api/mrc20/admin/teleport/diagnose/:coord` | path: `coord` req string | `not mounted` | Even if mounted, handler only returns deprecated error |
| `GET` | `/api/mrc20/admin/teleport/check-arrival/:pinId` | path: `pinId` req string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/teleport/check-asset-index/:assetOutpoint` | path: `assetOutpoint` req by route, query: `assetOutpoint` req by actual handler | `not mounted` | Route and handler disagree; path-only call would fail |
| `POST` | `/api/mrc20/admin/teleport/fix/:coord` | path: `coord` req string | `not mounted` | Even if mounted, hardcoded to fail because repair logic is disabled |
| `GET` | `/api/mrc20/admin/teleport/v2/list` | query: `limit` opt int default `100`, `sortBy` opt `time|timestamp|createdat|updatedat|completedat|sourceheight|sourceblockheight|targetheight|targetblockheight`, `order/sortOrder` opt `asc|desc` | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/teleport/v2/detail/:id` | path: `id` req string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/verify/supply/:tickId` | path: `tickId` req string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/verify/all` | none | `not mounted` | Handler implemented |
| `POST` | `/api/mrc20/admin/snapshot/create` | query: `token` opt string, `description` opt string fallback; body json opt `{ "description": "..." }` | `not mounted` | Defaults description to `Manual snapshot` |
| `GET` | `/api/mrc20/admin/snapshot/list` | query: `token` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/admin/snapshot/info/:id` | path: `id` req string; query: `token` opt string | `not mounted` | Handler implemented |
| `POST` | `/api/mrc20/admin/snapshot/restore/:id` | path: `id` req string; query: `token` opt string | `not mounted` | Handler implemented |
| `DELETE` | `/api/mrc20/admin/snapshot/:id` | path: `id` req string; query: `token` opt string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/debug/pending-in/:address` | path: `address` req string | `not mounted` | Handler implemented |
| `GET` | `/api/mrc20/debug/utxo-status/:address/:tickId` | path: `address` req string, `tickId` req string | `not mounted` | Handler implemented |

## Code Exists But Routes Are Not Mounted: HTML MRC20 And MRC721 Pages

| Method | Path | Parameters | Status | Notes |
|---|---|---|---|---|
| `GET` | `/mrc20/:page` | path: `page` req int64, invalid values fallback to `1` | `not mounted` | Handler implemented |
| `GET` | `/mrc20/info/:id` | path: `id` req string | `not mounted` | Handler implemented |
| `GET` | `/mrc20/holders/:id/:page` | path: `id` req string, `page` req int64 fallback `1`; query: `address` opt string | `not mounted` | Handler implemented |
| `GET` | `/mrc20/history/:id/:page` | path: `id` req string, `page` req int64 fallback `1` | `not mounted` | Handler implemented |
| `GET` | `/mrc20/address/:id/:address/:page` | path: `id` req string, `address` req string, `page` req int64 fallback `1` | `not mounted` | Handler implemented |
| `GET` | `/mrc721/:page` | path: `page` req string | `not mounted` | Function exists but body is effectively TODO |
| `GET` | `/mrc721/item/list/:name/:page` | path: `name` req string, `page` req string | `not mounted` | Function exists but body is effectively TODO |

## Specific Note: `/api/address/pin/list/:address`

Current code path:

- validates `address` path param
- validates query params `path` and `size`
- optional query param `cursor`
- converts `address` to `metaid`
- fetches one cursor page from Pebble
- enriches each pin with preview/content URLs and PopLv
- parses sorting with default field `timestamp`
- sorts the fetched page in memory

Accepted parameters:

- path:
  - `address` required
- query:
  - `path` required
  - `size` required
  - `cursor` optional
  - `sortBy` optional: `time|timestamp|height|blockheight|genesisheight|number|id|metaid|path`
  - `order` optional: `asc|desc`
  - `sortOrder` optional alias of `order`

Current implementation status:

- basic data fetch: implemented
- cursor pagination: implemented
- sorting: implemented, but only after the current page is fetched
- globally correct sorted pagination for non-default sort fields: not guaranteed
