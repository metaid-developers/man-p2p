# man-p2p Deployment Notes for 8.217.14.206

## Summary

This host can run `man-p2p` alongside the current production `man`, but it should **not** share the live Pebble database directory.

Recommended first deployment shape:

- keep current `man` unchanged on `/mnt/man_v2`
- create a separate `man-p2p` working root on `/data/man-p2p`
- seed `man-p2p` from a copy of `/mnt/man_v2/man_base_data_pebble`
- run `man-p2p` on a different local HTTP port such as `127.0.0.1:7778`
- keep `p2p_enable_chain_source=true` for the first production trial

## Current Server Facts

Observed on `8.217.14.206` on March 31, 2026:

- current `man` process listens on `*:7777`
- current production config is `/mnt/man_v2/config.toml`
- current production Pebble path is `/mnt/man_v2/man_base_data_pebble`
- `/mnt/man_v2/man_base_data_pebble` size is about `34G`
- `/mnt` free space is about `11G`
- `/data` free space is about `105G`

Conclusion:

- `/mnt` does **not** have enough free space for another full Pebble copy
- `/data` **does** have enough free space for one seeded `man-p2p` deployment

## Why The Live Pebble Directory Must Not Be Shared

`man-p2p` opens every Pebble sub-database directly with `pebble.Open(...)` under the configured base path. Running two indexer processes against the same live directory is not a supported deployment shape and risks lock conflicts or database corruption.

Do not point `man-p2p` at:

- `/mnt/man_v2/man_base_data_pebble`

while the production `man` is still using it.

## Recommended Layout

Use a dedicated root on `/data`:

```text
/data/man-p2p/
  man-p2p                     # binary
  config.toml                 # copied from /mnt/man_v2/config.toml, then edited
  p2p-config.json             # p2p runtime config
  man_base_data_pebble/       # seeded Pebble copy
  runtime/                    # p2p identity.key and runtime files
  logs/                       # optional
```

## Config Changes

Start from the current production config:

```bash
cp /mnt/man_v2/config.toml /data/man-p2p/config.toml
```

Then edit these fields:

- `[web].port = "127.0.0.1:7778"`
- `[web].host` should match how you plan to access it during validation
- `[pebble].dir = "/data/man-p2p/man_base_data_pebble"`
- keep `[pebble].num` the same as the source database

Do **not** commit real RPC credentials. Keep them only on the server copy.

Suggested initial `p2p-config.json`:

```json
{
  "p2p_sync_mode": "full",
  "p2p_bootstrap_nodes": [],
  "p2p_enable_relay": false,
  "p2p_listen_port": 4001,
  "p2p_announce_addrs": [
    "/ip4/8.217.14.206/tcp/4001",
    "/dns4/manapi.metaid.io/tcp/4001"
  ],
  "p2p_enable_chain_source": true
}
```

For the first node, `p2p_bootstrap_nodes` can be empty.
`p2p_listen_port` and `p2p_announce_addrs` should be set together if this node is meant to be reused as a stable bootstrap target by other machines.

## Safe Seed Copy Procedure

Copying a live Pebble tree without a freeze window is risky. The safest simple approach here is:

1. pre-copy while `man` is still running
2. stop `man` briefly
3. run a final delta sync while the source is frozen
4. restart `man`
5. start `man-p2p` from the copied tree

Suggested commands:

```bash
mkdir -p /data/man-p2p /data/man-p2p/runtime /data/man-p2p/logs

rsync -aH --delete /mnt/man_v2/man_base_data_pebble/ /data/man-p2p/man_base_data_pebble/

docker stop man-indexer-v2
rsync -aH --delete /mnt/man_v2/man_base_data_pebble/ /data/man-p2p/man_base_data_pebble/
docker start man-indexer-v2
```

This keeps the downtime to the final delta sync window instead of the whole 34G copy.

## First Run Command

Example:

```bash
cd /data/man-p2p
nohup ./man-p2p \
  -config /data/man-p2p/config.toml \
  -server=1 \
  -p2p-config /data/man-p2p/p2p-config.json \
  -data-dir /data/man-p2p/runtime \
  >/data/man-p2p/logs/man-p2p.log 2>&1 &
```

## First Validation Checklist

Verify locally first:

```bash
curl -sS http://127.0.0.1:7778/debug/count
curl -sS http://127.0.0.1:7778/api/p2p/status
curl -sS http://127.0.0.1:7778/api/p2p/peers
tail -n 120 /data/man-p2p/logs/man-p2p.log
```

What to expect:

- HTTP API responds
- `runtimeMode` is `chain-enabled`
- `peerId` is non-empty
- `listenAddrs` is non-empty
- `peerCount` may still be `0` and that is acceptable for the first node

## Disk Capacity Guidance

No immediate disk expansion is required for the **first** seeded deployment if `man-p2p` uses `/data`.

Current rough headroom:

- `/data` free: about `105G`
- seeded Pebble copy: about `34G`
- remaining free after seed: about `71G`

That is enough for:

- one full `man-p2p` seed copy
- binary, logs, runtime files
- some database growth

It is **not** generous headroom if:

- you want a second full replica on the same machine
- `utxov2_data` on `/data` continues growing quickly
- you plan to keep long-lived snapshots/backups on `/data`

Practical rule:

- first deployment: no disk expansion required
- before adding another full Pebble replica or if `/data` free space drops below `40G`: add disk first

## Notes

- Do not use `/mnt/man_v2/pebble_data` as the seed source unless its sync heights are verified. It is not the active path in the current production config.
- If `p2p_listen_port` is left unset, libp2p will still choose a dynamic port and the node will not be a stable bootstrap target across restarts.
- For the first production trial, keep the service private on `127.0.0.1:7778` and validate locally before deciding whether to expose it via nginx.
