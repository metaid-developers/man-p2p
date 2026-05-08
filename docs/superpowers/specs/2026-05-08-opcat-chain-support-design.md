# OpcatLayer Chain Support — Design Spec

Date: 2026-05-08
Status: draft

## Overview

Add OpcatLayer (`opcat`) as the fourth supported chain in man-p2p, joining BTC, MVC, and Doge.

OpcatLayer is a BSV-fork chain (no SegWit) with inscription format identical to MVC: OP_RETURN → metaid → operation → path → encryption → version → content_type → body. The chain exposes standard Bitcoin-compatible JSON-RPC and ZMQ.

**Scope**: PIN indexing only. MRC20/MRC721 parsing is explicitly excluded for this chain.

## Chain Parameters

| Parameter | Value |
|---|---|
| Chain code | `"opcat"` |
| RPC | `34.92.196.81:18443`, user `scrypt` |
| ZMQ | `tcp://34.92.196.81:18442` |
| Network magic | `0xe3e1f3e8` |
| Address format | Legacy `1` prefix (P2PKH=0x00, P2SH=0x05), same as BTC/MVC mainnet |
| Wire library | `github.com/bitcoinsv/bsvd` (same as MVC) |
| Inscription carrier | `nonstandard` output after OP_RETURN |
| TXID calculation | No SegWit — needs custom `GetNewHash` (BSV-style re-hash) |
| Initial index height | 120418 (first confirmed MetaID tx) |
| MRC20 | **Disabled** (`mrc20Height = -1`) |

## Design Decision: Approach C — New adapter package, MVC pattern

Three approaches were considered (full copy, shared base class, new package referencing MVC patterns). Approach C was selected: a new `adapter/opcat/` package mirroring MVC's structure, with core parsing logic following the identical MVC pattern. Zero changes to existing MVC/BTC/Doge adapter code.

## Changes

### 1. New package: `adapter/opcat/`

Five files:

- **`opcat.go`** — `Chain` interface implementation: RPC connection (`InitChain`), `GetBlock`, `GetBestHeight`, `GetTransaction`, `GetMempoolTransactionList`, `GetBlockMsg`, `GetTxSizeAndFees`, `GetInitialHeight`
- **`indexer.go`** — `Indexer` interface implementation: `InitIndexer`, `CatchPins`, `CatchPinsByTx`, `CatchMempoolPins`, `CatchTransfer`, `GetAddress`, `GetBlockTxHash`
- **`util.go`** — `GetNewHash` (BSV-style TXID recalculation without witness), address encoding (bsvd → btcutil Base58), `DecodeRawTransaction` helper
- **`zmq.go`** — ZMQ subscriber (`//go:build cgo`), connects to `common.Config.Opcat.ZmqHost`
- **`zmq_stub.go`** — Stub for non-cgo builds (`//go:build !cgo`), empty implementations

MRC20-specific methods are implemented as no-ops:

```go
func (indexer *Indexer) CatchNativeMrc20Transfer(...) []*mrc20.Mrc20Utxo { return nil }
func (indexer *Indexer) CatchMempoolNativeMrc20Transfer(...) []*mrc20.Mrc20Utxo { return nil }
```

Network params use `bsvd/chaincfg.MainNetParams` directly (address prefixes match BTC standard).

`parseOnePin` logic is identical to MVC's implementation.

### 2. Config layer: `common/config.go`

Add `opcatConfig` struct (mirrors `mvcConfig`):

```go
type opcatConfig struct {
    InitialHeight   int64  `toml:"initialHeight"`
    Mrc20Height     int64  `toml:"mrc20Height"`
    RpcHost         string `toml:"rpcHost"`
    RpcUser         string `toml:"rpcUser"`
    RpcPass         string `toml:"rpcPass"`
    RpcHTTPPostMode bool   `toml:"rpcHttpPostMode"`
    RpcDisableTLS   bool   `toml:"rpcDisableTLS"`
    ZmqHost         string `toml:"zmqHost"`
    PopCutNum       int    `toml:"popCutNum"`
}
```

Add `Opcat opcatConfig` field to `AllConfig`.

Add CLI flags: `--opcat_height`, `--opcat_rpc_host`, `--opcat_rpc_user`, `--opcat_rpc_password`, `--opcat_zmqpubrawtx`.

Add flag→config mapping and PopCutNum setting in `InitConfig`.

### 3. Config file: `config.toml`

```toml
[opcat]
initialHeight = 120418
mrc20Height = -1
rpcHost = "34.92.196.81:18443"
rpcUser = "scrypt"
rpcPass = "W4iULFuKidPY6wCv"
rpcHttpPostMode = true
rpcDisableTLS = true
zmqHost = "tcp://34.92.196.81:18442"
popCutNum = 21
```

### 4. Registration points

| File | Location | Change |
|---|---|---|
| `man/man.go` | `InitRuntime()` switch | Add `case "opcat":` — register Chain + Indexer adapters |
| `man/man.go` | `getSyncHeight()` fallback | Add `case "opcat": initialHeight = 0` (let config control) |
| `man/man.go` | import block | Add `"man-p2p/adapter/opcat"` |
| `man/indexer_pebble.go` | `handleMrc20()` switch | Add `case "opcat": mrc20Height = common.Config.Opcat.Mrc20Height` |

No changes to `Mrc20CatchUpRun()` — opcat is skipped because `mrc20StartHeight <= 0`.

### 5. Usage

```bash
go run . -config ./config.toml -server=1 -chain=opcat -p2p-config ./p2p-config.json
```

Or multi-chain: `-chain=btc,mvc,doge,opcat`.

## Non-goals

- MRC20/MRC721 parsing on OpcatLayer
- Meltdown transaction detection (PIN transfer tracking still works)
- Changes to existing BTC/MVC/Doge adapter code

## Estimated size

- New code: ~300 lines (adapter/opcat/)
- Modified code: ~50 lines across 4 existing files
