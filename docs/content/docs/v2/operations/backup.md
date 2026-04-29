---
title: Backup and Restore
prev: /docs/v2/operations
weight: 2
---

## What to Back Up

- `/config/` — Your `tsdproxy.yaml` and proxy list files
- `/data/` — Tailscale machine keys, certificates, OAuth caches

## Backup

```bash
docker compose stop tsdproxy
tar -czf tsdproxy-backup-$(date +%Y%m%d).tar.gz /path/to/config /path/to/data
docker compose start tsdproxy
```

## Restore

```bash
docker compose stop tsdproxy
tar -xzf tsdproxy-backup-20260401.tar.gz -C /
docker compose start tsdproxy
```

> [!IMPORTANT]
> After restoring `/data/`, Tailscale machines reconnect automatically.

## Migration

1. Back up both directories from old host
2. Start TSDProxy on new host with same config
3. Stop, restore `/data/` backup, start

> [!WARNING]
> Changing `dataDir` path means new Tailscale machines. Keep the same path.
