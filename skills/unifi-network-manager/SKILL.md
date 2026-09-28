---
name: unifi-network-manager
description: Use when the user wants to inspect or manage a UniFi / Ubiquiti network — listing sites, devices (access points, switches, gateways), WiFi clients, guest vouchers, restarting a device, cycling a PoE port, authorizing a guest, or configuring firewall zones/policies, DNS, VLANs, WiFi broadcasts/SSIDs, ACL rules, RADIUS profiles, or WAN interfaces; ALSO covers the account-wide UniFi Site Manager cloud API (api.ui.com) for listing hosts/consoles, cross-site devices, ISP metrics, and SD-WAN configs. Triggers on UniFi, Ubiquiti, access point, WiFi, PoE, firewall, VLAN, SSID, ACL, RADIUS, Site Manager, hosts, ISP metrics, SD-WAN.
---

# UniFi Network Manager (CLI)

## Overview

`unifi` is a CLI wrapping the UniFi Network Integration API
(`https://{host}/proxy/network/integration/v1`, `X-API-KEY` auth). Use it to
inspect and manage a local UniFi console. JSON is the default output — parse it
to answer.

## Safety (read first)

- **READ before WRITE** — inspect with list/get/stats before any mutation.
- **Every mutation requires `--yes`.** First state exactly what will happen and
  get the user's explicit confirmation in conversation (e.g. "This reboots AP
  <name> (id ...) — proceed?"), THEN run the command with `--yes`.
- You operate the user's real network. Never run a mutation the user did not
  approve, and keep actions scoped to the network they asked about. If a request
  is ambiguous or risky, stop and ask.

## Setup Check

1. Verify the tool: `command -v unifi`. If missing, tell the user to install it
   (repo has `make install`). Do NOT install it yourself without asking.
2. Test connectivity: `unifi info`.

If you see `host is required` or auth errors, config is missing. Precedence:
**flags > env (`UNIFI_HOST`, `UNIFI_API_KEY`, `UNIFI_SITE`, `UNIFI_INSECURE`) >
file** (`~/.config/unifi/config.json`). Have the user configure it themselves —
never ask for the API key in chat:

```
unifi configure --host <ip> --insecure   # set UNIFI_API_KEY in their env
```

Local consoles use self-signed certs, so `--insecure` is normally required.

## Workflow

- Discover the site first if unknown: `unifi sites list` → use the site `id` as
  `--site`. One site → use it (often named "default").
- Use `--all` for the complete list (handles pagination); else `--limit N`.

### Read commands

```
unifi info                                  # connectivity check
unifi sites list                            # get site id
unifi devices list --site <id>              # APs/switches/gateways
unifi devices get <deviceId> --site <id>
unifi devices stats <deviceId> --site <id>  # health telemetry
unifi clients list --site <id> --all        # connected devices
unifi clients get <clientId> --site <id>
unifi vouchers list --site <id>             # guest WiFi vouchers
```

`devices stats` is the only telemetry endpoint in the integration API.

### Mutations (confirm in conversation, THEN add `--yes`)

```
unifi devices restart <deviceId> --site <id> --yes          # reboots device
unifi devices port-cycle <deviceId> --port <N> --site <id> --yes  # power-cycles PoE port
unifi clients authorize <clientId> --site <id> --yes        # authorize a guest
unifi vouchers create --site <id> --count 5 --minutes 1440 --yes
unifi vouchers delete <voucherId> --site <id> --yes
```

Warn the user: **device restart** briefly drops all clients on that device;
**PoE port-cycle** reboots whatever device is powered on that port.

## Beyond the curated commands

For firewall, DNS, networks/VLANs, WiFi broadcasts (SSIDs), ACLs,
RADIUS, WANs, VPN servers, or any other endpoint, the CLI reaches EVERY endpoint
via typed resource commands or the `api` passthrough. See `resources.md` in this
skill directory for the full path table and `api` usage.

## Site Manager (cloud) commands

Besides the local console API above, the CLI also wraps the **UniFi Site Manager
API** — Ubiquiti's account-wide cloud API at `https://api.ui.com`. Use it to see
*all* consoles ("hosts") on an account, devices across sites, ISP performance
metrics, and SD-WAN configs — handy when a console is behind CGNAT or you manage
many sites.

Key differences from the local commands:
- **Account-wide, not per-console.** No console URL (`--host`) or `--site` needed.
- **Read-only today** (write scope is rolling out through 2026) — no `--yes`.
- **Separate API key.** The cloud key is created at unifi.ui.com → Settings →
  API Keys, and is generally *different* from the local console key. The CLI
  reuses `UNIFI_API_KEY` for it; if cloud calls return 401/403 while local calls
  work, the user likely needs to set their cloud key.

```
unifi site-manager hosts list                 # all consoles on the account
unifi site-manager hosts get <hostId>
unifi site-manager sites list
unifi site-manager devices list --host-id <id> # filter to a console
unifi site-manager isp-metrics get 1h --duration 7d
unifi site-manager isp-metrics query 1h --data '{"sites":["<id>"]}'
unifi site-manager sdwan list
unifi site-manager sdwan get <configId>
unifi site-manager sdwan status <configId>
unifi sm hosts list                            # `sm` is a shorthand
```

Anything not yet typed is reachable via `unifi site-manager api GET <path>`
(e.g. `unifi site-manager api GET /v1/hosts`). The typed commands above are all
read-only and need no `--yes`. The `api` passthrough mirrors the local `api`
command: a non-GET method (for when cloud write scope ships) requires `--yes`.

## Troubleshooting

| Symptom | Cause / Fix |
|---|---|
| Exit code 2 | Bad command usage |
| Exit code 1 | Runtime / API error |
| `API error 401/403` | API key problem — re-run `unifi configure` |
| `API error 429 (retry after Ns)` | Rate limited — wait N seconds |
| x509 / certificate error | Add `--insecure` (or `UNIFI_INSECURE=true`) |
| `host is required` | Config missing — see Setup Check |
