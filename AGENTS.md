# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Project Overview

**man-indexer-v2** is a multi-chain blockchain indexer for the MetaID protocol. It indexes PIN inscriptions and MRC20 token operations across Bitcoin, Dogecoin, and MicroVision Chain (MVC), stores data in PebbleDB, and exposes a RESTful API via Gin.

## Build & Run Commands

```bash
# Build (Linux target from macOS, requires cross-compile toolchain + ZMQ)
make mac

# Build (native Linux)
make linux

# Run in regtest mode
make run_regtest
# equivalent to: CGO_ENABLED=1 go run app.go -test=2 -config=./config_regtest.toml

# Run tests (Go standard)
go test ./...

# Run a single test
go test -run TestFunctionName ./path/to/package/

# Run MRC20-specific tests
go test -run TestMrc20 ./man/

# Build MRC20 migration tool
make mrc20_migration

# Generate Swagger docs
swag init -g app.go
```

**Note**: CGO is required (ZMQ dependency via `github.com/pebbe/zmq4`). All builds need `CGO_ENABLED=1` except migration tools.

## Architecture

### Adapter Pattern (multi-chain abstraction)

Two core interfaces in `adapter/`:
- **`Chain`**: Block/transaction retrieval (`GetBlock`, `GetTransaction`, `GetBestHeight`, etc.)
- **`Indexer`**: PIN parsing and transfer detection (`CatchPins`, `CatchTransfer`, `CatchNativeMrc20Transfer`, `ZmqRun`, etc.)

Each chain has its own implementation under `adapter/{bitcoin,dogecoin,microvisionchain}/`. Chain-specific inscription parsing differs significantly:
- Bitcoin: SegWit Witness data (P2WSH/Taproot)
- Dogecoin: P2SH ScriptSig extraction
- MVC: Custom inscription format

### Main Loop (`app.go`)

`main()` → init config (TOML) → `man.InitAdapter()` → start API server (optional) → start ZMQ listener → run `IndexerRun()` every 10 seconds. MRC20 catch-up indexing runs after each main indexer loop.

### Core Packages

| Package | Role |
|---------|------|
| `man/` | Core indexer logic: PIN processing, MRC20 handling (deploy/mint/transfer/teleport), meltdown |
| `mrc20/` | Data structures, constants, status codes for MRC20 protocol |
| `pin/` | PIN inscription data structures and status constants |
| `pebblestore/` | PebbleDB storage layer with 16-shard partitioning for PINs |
| `api/` | Gin HTTP server, REST endpoints, Swagger docs, HTML templates |
| `common/` | Config parsing (TOML), CLI flags, shared utilities |
| `adapter/` | Chain/Indexer interfaces and per-chain implementations |
| `web/` | Embedded static assets and HTML templates (`//go:embed`) |

### MRC20 Token Protocol

MRC20 is a UTXO-based token protocol with operations: **deploy**, **mint**, **transfer** (native/data/teleport). Key files:
- `man/mrc20.go` — Main handler: `Mrc20Handle()`, arrival/teleport processing
- `man/mrc20_new_methods.go` — Balance management, transaction history
- `man/mrc20_pebble.go` — DB storage for MRC20 UTXOs, arrivals, teleports
- `man/mrc20_validator.go` — Validation logic (deploy, mint, transfer rules)

**UTXO status flow**: Available(0) → TeleportPending(1) or TransferPending(2) → Spent(-1)

### Teleport (Cross-Chain Transfer)

Bilateral verification system for moving MRC20 assets between chains:
1. **Target chain**: User creates `/ft/mrc20/arrival` PIN declaring `assetOutpoint`
2. **Source chain**: User creates `/ft/mrc20/transfer` PIN with `type=teleport`, `coord=arrival_pin_id`
3. Both sides confirmed → source UTXO marked spent, new UTXO created on target chain
4. Failed teleport → `handleFailedTeleportInputs()` returns assets to sender

Arrival status: Pending(0) → Completed(1) / Invalid(2). Either side can arrive first; pending queue handles coordination.

### PIN Meltdown

Consolidates ≥3 PIN UTXOs (546 sats each) into one transaction. Status: `PinStatusMeltdown = -2`. See `DOC_MELTDOWN.md`.

### Storage (PebbleDB)

Key-value store with prefix-based namespacing. Sharded across 16 databases for PIN data. Specialized DBs for blocks, counters, paths, addresses, MRC20, mempool, etc. Batch operations ensure atomicity.

## Configuration

TOML config files (`config.toml`, `config_regtest.toml`, etc.) with sections:
- Chain RPC endpoints: `[btc]`, `[mvc]`, `[doge]`
- Sync modes: `[sync]` (fullNode, mrc20Only)
- Database: `[pebble]` (shard count)
- CLI flags: `-chain`, `-test` (0=mainnet, 1=testnet, 2=regtest), `-config`, `-server`

## Conventions

- **Arithmetic**: Use `github.com/shopspring/decimal` for all token amounts — never float
- **Concurrency**: `sync.Map` for shared maps (e.g., `AllCreatorAddress`), `sync.Mutex` for caches
- **Error handling**: Early return `if err != nil { return err }` — no custom error types
- **Logging**: Standard `log` package with `[DEBUG]` prefix for dev context
- **MRC20 errors**: Predefined error message constants in `mrc20/mrc20.go` (e.g., `ErrDeployContent`, `ErrMintLimit`)
- **JSON**: Uses `github.com/bytedance/sonic` for high-performance serialization

## Key Documentation

- `TELEPORT_SPEC.md` — Cross-chain teleport specification
- `MRC20_IMPLEMENTATION.md` — MRC20 protocol details
- `MRC20_INDEXING_DESIGN.md` — Indexing architecture
- `DOC_MELTDOWN.md` — PIN meltdown mechanics
- `DOGECOIN_ADAPTER.md` — Dogecoin-specific implementation
