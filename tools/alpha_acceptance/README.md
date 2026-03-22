# Alpha Acceptance CLI

`tools/alpha_acceptance` is a scripted dual-machine Alpha validation tool for the current `man-p2p` + packaged `IDBots.app` flow.

It verifies three Alpha gates in one run:

1. Peer discovery between two packaged desktop nodes
2. `local-first` miss fallback through the local MetaID RPC bridge
3. Realtime PIN propagation across the P2P mesh

## Preconditions

- Local machine can launch the packaged app with `open -n`.
- Remote machine is reachable over SSH.
- Remote machine already has a packaged `IDBots.app`, or you pass `--remote-copy`.
- `ssh`, `scp`, `curl`, `launchctl`, `pgrep`, and `open` are available locally.
- If the remote host needs a password, export it through `IDBOTS_REMOTE_PASSWORD`. The tool will use `SSH_ASKPASS`.

## Run

```bash
cd /Users/tusm/.config/superpowers/worktrees/man-p2p/codex/bootstrap-reload-reconnect

export IDBOTS_REMOTE_PASSWORD=123456

CGO_ENABLED=0 go run ./tools/alpha_acceptance \
  --local-app /Users/tusm/Documents/MetaID_Projects/IDBots/IDBots-indev/release/mac-arm64/IDBots.app \
  --remote-user showpay \
  --remote-host 192.168.3.52 \
  --remote-app '~/tmp/idbots-alpha/IDBots.app' \
  --preferred-local-ip 192.168.3.30
```

To start an isolated remote runtime instead of reusing an already-running GUI instance on `7281`, pass a dedicated remote port:

```bash
CGO_ENABLED=0 go run ./tools/alpha_acceptance \
  --local-app /Users/tusm/Documents/MetaID_Projects/IDBots/IDBots-indev/release/mac-arm64/IDBots.app \
  --remote-user showpay \
  --remote-host 192.168.3.52 \
  --remote-app '~/tmp/idbots-alpha/IDBots.app' \
  --remote-base-url http://127.0.0.1:62196 \
  --remote-launch-mode binary \
  --preferred-local-ip 192.168.3.30
```

## Notes

- The tool starts the local packaged app in an isolated runtime root under `/tmp`.
- The remote packaged app defaults to `binary` launch mode, which runs `Contents/MacOS/IDBots` under an isolated `/tmp` runtime root over SSH instead of relying on `open -n`.
- If the requested remote base URL is already healthy, the tool reuses that running remote runtime and patches the matching config file in place.
- Use a dedicated `--remote-base-url` port when you want an isolated scripted remote runtime that does not collide with a normal `IDBots` session already running on the remote machine.
- Remote config is patched, reloaded, and restored automatically.
- If remote reload does not connect the new bootstrap peer, the tool falls back to restarting the remote `man-p2p` child.
- The output is a JSON summary containing both peer IDs, both bootstrap multiaddrs, the fallback PIN ID, and the synthetic realtime PIN ID.
