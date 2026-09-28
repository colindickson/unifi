# UniFi typed resources and `api` passthrough reference

Load this when the user wants something beyond the curated
sites/devices/clients/vouchers commands — firewall, DNS, networks/VLANs, WiFi
broadcasts (SSIDs), ACLs, RADIUS, WANs, VPN servers, etc.

## `api` — universal passthrough for ANY endpoint

```
unifi api <METHOD> <path> [--data <json>] [--data-file <file>] [--query k=v]...
```

- `<METHOD>` ∈ GET/POST/PUT/PATCH/DELETE. Output is always JSON.
- A `{site}` (or `:site`) token in the path is auto-replaced with the resolved
  site, so you usually don't need `--site`.
- `--data-file -` reads the body from stdin. `--data` and `--data-file` are
  mutually exclusive.
- Non-GET requires `--yes` (see safety in SKILL.md).

```
unifi api GET /sites/{site}/firewall/policies
unifi api GET /sites/{site}/clients --query limit=50
unifi api POST /sites/{site}/networks --yes --data '{"management":"GATEWAY","name":"IoT","enabled":true,"vlanId":40,"cellularBackupEnabled":false,"internetAccessEnabled":true,"isolationEnabled":false,"ipv4Configuration":{"autoScaleEnabled":false,"hostIpAddress":"192.168.40.1","prefixLength":24}}'  # GATEWAY-managed VLAN
unifi api DELETE /sites/{site}/wifi/broadcasts/<id> --yes
```

## Typed resource commands (prefer when available)

Forms: `unifi <group> list` · `get <id>` · `create --data '<json>'` ·
`update <id> --data '<json>'` · `delete <id>`.
For firewall the sub comes first: `unifi firewall <zones|policies> <action> [id]`.

Paths match the OpenAPI spec of UniFi Network application v10.6.106. The CLI works on
every v10 version (10.0.162 to 10.6.106). `firewall policies` and `dns` need 10.1.84 or
later, because 10.0.162 has no such endpoints.

| Group | Path | Ops |
|---|---|---|
| `networks` | `/sites/{site}/networks` | list/get/create/update/delete |
| `firewall zones` | `/sites/{site}/firewall/zones` | list/get/create/update/delete |
| `firewall policies` | `/sites/{site}/firewall/policies` | list/get/create/update/delete |
| `acl-rules` | `/sites/{site}/acl-rules` | list/get/create/update/delete |
| `dns` | `/sites/{site}/dns/policies` | list/get/create/update/delete |
| `traffic-lists` | `/sites/{site}/traffic-matching-lists` | list/get/create/update/delete |
| `wans` | `/sites/{site}/wans` | list |
| `vpn-servers` | `/sites/{site}/vpn/servers` | list |
| `radius-profiles` | `/sites/{site}/radius/profiles` | list |
| `device-tags` | `/sites/{site}/device-tags` | list |
| `countries` | `/countries` (not site-scoped) | list |

`wans`, `vpn-servers`, `radius-profiles`, `device-tags`, and `countries` are list-only.

No typed command on v10.6.106 (use the `api` passthrough): WiFi broadcasts (SSIDs)
→ `unifi api GET /sites/{site}/wifi/broadcasts`. Also `/dpi/applications`,
`/dpi/categories`, `/pending-devices`, `/sites/{site}/switching/{lags,mc-lag-domains,switch-stacks}`,
`/sites/{site}/vpn/site-to-site-tunnels`, and `.../{acl-rules,firewall/policies}/ordering`.
This version has no `wlans`, `port-forwards`, or `traffic-routes` endpoint.

Paths can vary by firmware. The console's **Settings → Integrations** is the
source of truth; if a typed path is wrong, fall back to `unifi api <METHOD> <path>`.

## Reading device stats

`unifi devices stats <id>` returns `uptimeSec`, `cpuUtilizationPct`,
`memoryUtilizationPct`, `loadAverage{1,5,15}Min`, and `uplink.txRateBps`/`rxRateBps`.
High memory (>90%) or load rising across the 1/5/15-min averages indicates a
struggling device. `-o table` renders a readable summary for humans.

## Site Manager (cloud) API

Base: `https://api.ui.com` · Auth: `X-API-KEY` (reuses `UNIFI_API_KEY`) ·
Read-only · cursor pagination (`--all` follows `nextToken`, `--limit` = pageSize).

| Command | Method | Path |
|---|---|---|
| `site-manager hosts list` | GET | `/v1/hosts` |
| `site-manager hosts get <id>` | GET | `/v1/hosts/{id}` |
| `site-manager sites list` | GET | `/v1/sites` |
| `site-manager devices list` | GET | `/v1/devices` (`?hostIds=`) |
| `site-manager isp-metrics get <5m\|1h>` | GET | `/ea/isp-metrics/{type}` |
| `site-manager isp-metrics query <5m\|1h>` | POST | `/ea/isp-metrics/{type}/query` |
| `site-manager sdwan list` | GET | `/ea/sd-wan-configs` |
| `site-manager sdwan get <id>` | GET | `/ea/sd-wan-configs/{id}` |
| `site-manager sdwan status <id>` | GET | `/ea/sd-wan-configs/{id}/status` |

Escape hatch: `unifi site-manager api <METHOD> <path>` (e.g.
`unifi site-manager api GET /v1/hosts`). Like the local `api` command, a non-GET
method requires `--yes` (the typed commands above are read-only and do not).
