# AGENTS.md

## Start Here

1. This file
2. `localdocs/README.md` (local-only context, baselines, pitfalls)
3. `docs/superpowers/` (P2P specs) when task ties to IDBots integration

Durable guidance lives here; fast-changing session context goes in `localdocs/`.

## Project Identity

This repo started as **man-indexer-v2** and now serves as **man-p2p**, the local-first MetaID indexer + libp2p runtime embedded by IDBots. `main` is the only long-lived branch.

## Build & Run

```bash
make all                          # cross-compile release binaries (CGO_ENABLED=0)
make test                         # CGO_ENABLED=0 go test ./p2p/... -v -count=1
make alpha-test                   # focused acceptance: p2p + api + man package tests
make cgo-api-smoke                # macOS cgo-backed smoke test (uses CGO_ENV_WRAPPER)
CGO_ENABLED=0 go run . -config ./config.toml -server=1 -p2p-config ./p2p-config.json -data-dir ./man_p2p_data
CGO_ENABLED=0 go test ./p2p -run TestLoadConfig -v -count=1
swag init -g app.go               # regenerate Swagger docs
make run-local-mvc                # shortcut: go run . -chain mvc -config ./config.toml -server=1 ...
```

Release binaries are built with `CGO_ENABLED=0`. The `p2p/` package does not require CGO; legacy indexer paths do (ZMQ via `github.com/pebbe/zmq4`). Do not assume every package is exercised by the `p2p/` test suite.

On macOS, if cgo commands hit `github.com/DataDog/zstd` header errors from `/usr/local/include`, use `./tools/with-macos-cgo-env.sh ...` or `make cgo-*` wrappers.

## Architecture

Dual entrypoint in `app.go`:
1. P2P-first path: init config → load JSON p2p config → `p2p.InitHost/Gossip/Presence/SyncHandler` → optionally start Gin API
2. Chain-source indexing path: legacy adapter init + ZMQ loop + 10s `IndexerRun` tick

Two adapter interfaces in `adapter/`:
- **`Chain`**: block/tx retrieval per blockchain
- **`Indexer`**: PIN parsing & transfer detection per chain

Inscription parsing differs per chain (BTC: SegWit witness, DOGE: P2SH ScriptSig, MVC: custom format). See `adapter/{bitcoin,dogecoin,microvisionchain}/`.

Wiring from p2p callbacks to core indexer: `p2p_wiring.go` → `man.IngestP2PPin()`.

### Package Notes

| Package | Non-obvious facts |
|---------|-------------------|
| `main` (root) | `app.go` entrypoint, `app_runtime.go` guards MRC20 behind chainSource, `p2p_wiring.go` bridges p2p → man callbacks |
| `p2p/` | 18 files: `host.go`, `gossip.go`, `sync.go`, `presence.go`, `config.go`, `relay.go`, `storage.go`, `subscription.go`. `presence.go` is the largest (681 lines). |
| `pebblestore/` | 16-shard partitioning for PINs, plus specialized DBs for blocks, counters, MRC20, mempool, metaid info, etc. |
| `api/` | Gin HTTP server — endpoints consumed by IDBots for health, status, peers, config reload |
| `web/` | Embedded via `//go:embed web/static/* web/template/* ...` |
| `mrc20/` | Data structures + status constants (separated from `man/`) |

## IDBots Integration Workflow

1. Change `man-p2p` → run focused Go tests here → build target binary
2. In IDBots repo: `npm run sync:man-p2p` (copies platform binary)
3. Validate in `npm run electron:dev`, then again in the packaged `.app` bundle
4. For macOS packaged testing, launch `IDBots.app` normally (not `Contents/MacOS/IDBots` directly — it reproduces networking differently)

## Gotchas

- **Peerless is healthy**: `0 peers` is acceptable in current Alpha. Don't treat it as offline.
- **mDNS log**: `mdns: skipped (zeroconf dependency not available)` is expected. Not a blocker.
- **CGO_ENABLED=0**: Always use for normal builds and P2P tests. Only cgo-backed smoke tests need CGO.
- **.gitignore excludes**: `config.toml`, `p2p-config.json`, `localdocs/`, `*.local.toml`, `*.local.json` — create your own from the `.example.*` files.

## Conventions

- **Arithmetic**: `github.com/shopspring/decimal` for all token amounts — never float
- **JSON**: `github.com/bytedance/sonic`
- **Concurrency**: `sync.Map` for shared maps, `sync.Mutex` for caches
- **Error handling**: `if err != nil { return err }` — no custom error types
- **Logging**: standard `log` with `[DEBUG]` prefix

## Commit and Merge Rules

- If you notice unfamiliar or unrelated file changes, continue working and stay focused on your own scoped edits unless the user asks you to inspect them.
- For each completed round that modifies existing code/docs or adds new code/docs, automatically stage and commit only the files you changed and understand.
- For deletion changes, wait until the user explicitly says "commit" before staging and committing those deletions.
- Prefer small, frequent commits. Commit each independent, verifiable unit of work as soon as it is complete.
- For every modification or newly added feature, create one commit.
- For every commit, use the `metabot-post-buzz` skill with the Lisa Hahn identity (slug: `lisa-hahn`) to post a detailed development-journal entry on-chain describing the change.
- Use commit messages in the format `<type>: <short description>`, where `<type>` is one of `feat`, `fix`, `refactor`, `docs`, or `chore`.
- Before committing, make sure the relevant local tests or verification steps pass for your changes.
- When merging completed work into `main`, use `git merge --no-ff` to preserve the feature merge point.

## Behavioral Guidelines

**Tradeoff:** These guidelines bias toward caution over speed. For trivial tasks, use judgment.

### 1. Think Before Coding

**Don't assume. Don't hide confusion. Surface tradeoffs.**

Before implementing:
- State your assumptions explicitly. If uncertain, ask.
- If multiple interpretations exist, present them — don't pick silently.
- If a simpler approach exists, say so. Push back when warranted.
- If something is unclear, stop. Name what's confusing. Ask.

### 2. Simplicity First

**Minimum code that solves the problem. Nothing speculative.**

- No features beyond what was asked.
- No abstractions for single-use code.
- No "flexibility" or "configurability" that wasn't requested.
- No error handling for impossible scenarios.
- If you write 200 lines and it could be 50, rewrite it.

Ask yourself: "Would a senior engineer say this is overcomplicated?" If yes, simplify.

### 3. Surgical Changes

**Touch only what you must. Clean up only your own mess.**

When editing existing code:
- Don't "improve" adjacent code, comments, or formatting.
- Don't refactor things that aren't broken.
- Match existing style, even if you'd do it differently.
- If you notice unrelated dead code, mention it — don't delete it.

When your changes create orphans:
- Remove imports/variables/functions that YOUR changes made unused.
- Don't remove pre-existing dead code unless asked.

The test: Every changed line should trace directly to the user's request.

### 4. Goal-Driven Execution

**Define success criteria. Loop until verified.**

Transform tasks into verifiable goals:
- "Add validation" → "Write tests for invalid inputs, then make them pass"
- "Fix the bug" → "Write a test that reproduces it, then make it pass"
- "Refactor X" → "Ensure tests pass before and after"

For multi-step tasks, state a brief plan:
```
1. [Step] → verify: [check]
2. [Step] → verify: [check]
3. [Step] → verify: [check]
```

Strong success criteria let you loop independently. Weak criteria ("make it work") require constant clarification.

### 5. No Guessing, No Drive-By Fixes

**Verify boundaries before acting. Don't fix bugs you didn't create.**

- Never guess. When writing a plan or code, if anything is unclear or any scope boundary is ambiguous, either read the relevant code or discuss with the user — keep going until every boundary is clear.
- Don't opportunistically fix pre-existing bugs that fall outside the current task. Surface them to the user and let them decide; never silently change behavior you weren't asked to change.

## Reference Docs Worth Reading

`TELEPORT_SPEC.md` `MRC20_IMPLEMENTATION.md` `MRC20_INDEXING_DESIGN.md` `DOC_MELTDOWN.md` `DOGECOIN_ADAPTER.md` `docs/DEPLOY_SOP.md`
