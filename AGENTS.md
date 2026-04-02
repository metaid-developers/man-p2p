# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## Start Here

Read in this order:

1. This `AGENTS.md`
2. `localdocs/README.md` if it exists locally
3. Relevant specs/plans under `docs/superpowers/` when the task is tied to the current IDBots P2P integration

Keep `AGENTS.md` stable and durable. Put fast-changing local context, current baselines, recent pitfalls, and new-session prompts in `localdocs/`.

## Project Overview

This repository started as **man-indexer-v2** and now also serves as **man-p2p**, the local-first MetaID indexer + P2P runtime embedded by IDBots.

Current practical role:
- expose the local HTTP API consumed by IDBots
- run a libp2p node for peer discovery / pin sync
- preserve fallback compatibility when no peers are available

The current Alpha baseline is the local-first P2P integration used by IDBots `v0.1.100` on March 23, 2026. In this phase, P2P-backed PIN sync is the focus; chain-source indexing remains optional, and MRC20 catch-up is intentionally disabled in the P2P-first runtime path.

## Build & Run Commands

```bash
# Build all release binaries
make all

# Focused P2P test suite
make test

# Alpha acceptance suite used during IDBots integration
make alpha-test

# Run the current app entrypoint with local p2p config
go run . -config ./config.toml -server=1 -p2p-config ./p2p-config.json -data-dir ./man_p2p_data

# Run a specific package/test directly
CGO_ENABLED=0 go test ./p2p -run TestLoadConfig -v -count=1

# Run a macOS cgo-backed smoke test through the repo wrapper
make cgo-api-smoke

# Regenerate Swagger docs
swag init -g app.go
```

**Note**:
- Current release binaries are built with `CGO_ENABLED=0` via `Makefile`.
- Some legacy indexer paths still depend on ZMQ / chain adapters; do not assume every package is exercised by the P2P alpha test suite.
- When the task is about IDBots integration, validate the binary contract in this repo first, then sync into IDBots and verify there.
- If plain macOS cgo commands hit `github.com/DataDog/zstd` header errors from `/usr/local/include`, use `./tools/with-macos-cgo-env.sh ...` or the `make cgo-*` wrapper targets instead of swapping compression implementations.

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

`main()` now initializes runtime config, optionally loads the P2P JSON config, starts the libp2p host/gossip/storage monitor, and conditionally enables the legacy chain adapters. IDBots currently relies on the local HTTP API and P2P host even when chain-source indexing is disabled.

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
| `p2p/` | Runtime config, libp2p host/bootstrap/relay, gossip, sync handlers, storage guardrails |
| `web/` | Embedded static assets and HTML templates (`//go:embed`) |

## IDBots Integration Notes

- IDBots embeds platform binaries from this repo under `resources/man-p2p/`.
- After changing runtime behavior that affects the bundled binary contract, rebuild the target binary here and then run `npm run sync:man-p2p` in the IDBots repo.
- Development flow is usually:
  1. change `man-p2p`
  2. run focused Go tests here
  3. build/sync the binary into IDBots
  4. validate in IDBots dev runtime
  5. validate again in packaged app builds for release/acceptance
- Packaged macOS app testing should launch `IDBots.app` normally, not `IDBots.app/Contents/MacOS/IDBots` directly.

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
- `docs/DEPLOY_SOP.md` — deployment / health-check SOP
- `docs/superpowers/specs/` — current P2P design docs used during the IDBots alpha work
