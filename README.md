# unifi

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8.svg)](go.mod)

A small, dependency-free Go CLI for the UniFi Network Integration API.

> **Unofficial tool.** This project is not affiliated with, endorsed by, or
> sponsored by Ubiquiti Inc. "UniFi" and "Ubiquiti" are trademarks of their
> respective owner. Use at your own risk.

## Contents

- [Overview](#overview)
- [Install](#install)
- [Quickstart](#quickstart)
- [Configuration](#configuration)
- [Global Flags](#global-flags)
- [Commands](#commands)
- [Full API coverage](#full-api-coverage)
- [Output Formats](#output-formats)
- [Pagination](#pagination)
- [Safety: Mutating Commands](#safety-mutating-commands)
- [Security](#security)
- [Claude skill](#claude-skill)
- [Exit Codes](#exit-codes)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)
- [License](#license)

## Overview

`unifi` talks to the UniFi Network *Integration* API at:

```
https://{host}/proxy/network/integration/v1
```

Authentication is via the `X-API-KEY` header. Requires **UniFi Network 9.x+** and **UniFi OS 9.3.43+**.

Obtain an API key from the UniFi console: **Settings → Control Plane / Integrations → API Keys** (also available via [unifi.ui.com](https://unifi.ui.com) Site Manager).

> **Note:** Local UniFi consoles use self-signed TLS certificates. Pass `--insecure` (or set `UNIFI_INSECURE=true`) for any local console.

## Install

**Prerequisites:** Go 1.23+, and a controller running **UniFi Network 9.x+** on
**UniFi OS 9.3.43+**.

### With `go install`

```sh
go install github.com/colindickson/unifi/cmd/unifi@latest
```

This drops the `unifi` binary in `$(go env GOPATH)/bin` — make sure that
directory is on your `PATH`.

### From source

```sh
# Build binary to ./unifi
make build

# Build, install the binary to ~/bin, and link the Claude skill (see below)
make install

# Override the install location
make install INSTALL_DIR=/usr/local/bin
```

> `make install` puts the binary in `~/bin` by default, which is **not** on the
> `PATH` on most systems. Either add `~/bin` to your `PATH` or install elsewhere
> with `make install INSTALL_DIR=/usr/local/bin`.

`make install` also runs `make install-skill`, which symlinks the bundled
`unifi-network-manager` Claude skill into `~/.claude/skills/`. The symlink
points back at `skills/unifi-network-manager/` in this repo, so edits to the
skill are picked up live. Manage it independently with:

```sh
make install-skill     # create/refresh the symlink (idempotent)
make uninstall-skill   # remove the symlink (only if it is a symlink)
```

`make uninstall` removes both the binary and the skill symlink. Run
`make help` to list every target.

## Quickstart

```sh
# 1. Install (see above), then confirm it runs.
unifi --help

# 2. Get an API key from your console:
#    Settings → Control Plane / Integrations → API Keys

# 3. Save your connection settings. Pass the key via the environment so it
#    never lands in your shell history (local consoles need --insecure):
UNIFI_API_KEY=<your-key> unifi configure --host 192.168.1.1 --insecure

# 4. Verify connectivity.
unifi info

# 5. Discover your site, then list devices.
unifi sites list
unifi devices list --site <site-id>
```

## Configuration

Configuration is resolved in order: **flags > environment variables > config file** (highest to lowest precedence).

### Flags

| Flag | Description |
|------|-------------|
| `--host` | Controller host (IP or hostname) |
| `--api-key` | API key |
| `--site` | Site ID |
| `--insecure` | Skip TLS certificate verification |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `UNIFI_HOST` | Controller host |
| `UNIFI_API_KEY` | API key |
| `UNIFI_SITE` | Site ID |
| `UNIFI_INSECURE` | Skip TLS verification — truthy values: `1`, `true`, `yes`, `on` |
| `UNIFI_OUTPUT` | Output format — `json` (default) or `table` |

### Config File

The config file is stored at `~/.config/unifi/config.json` (mode `0600`). Override the path with `$UNIFI_CONFIG`.

Recognized fields: `host`, `api_key`, `site`, `insecure`, and `output` (`json` or `table`).

Write settings with the `configure` command. Prefer supplying the key via the
`UNIFI_API_KEY` environment variable so it is not recorded in your shell
history or visible in the process list:

```sh
UNIFI_API_KEY=<your-key> unifi configure --host 192.168.1.1 --insecure
```

> **Avoid `--api-key <key>` on the command line on shared systems.** Arguments
> are visible to other local users (via `ps` / `/proc`) and are saved to your
> shell history. Use `UNIFI_API_KEY` or the config file instead. See
> [Security](#security).

## Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--host <host>` | Controller host | — |
| `--api-key <key>` | API key | — |
| `--site <id>` | Site ID | — |
| `--insecure` | Skip TLS certificate verification | `false` |
| `-o`, `--output <table\|json>` | Output format (table is opt-in) | `json` |
| `--limit <n>` | Page size for list commands | `25` |
| `--all` | Auto-paginate and fetch all pages | `false` |
| `--yes` | Confirm mutating commands | `false` |
| `-h`, `--help` | Show help | — |

## Commands

| Command | Purpose |
|---------|---------|
| `configure` | Save connection settings |
| `info` | Show controller info |
| `sites` | List sites |
| `devices` | List/get/restart/port-cycle devices |
| `clients` | List/get/authorize clients |
| `vouchers` | Manage hotspot vouchers |
| `api` | Universal passthrough to any API endpoint — see [Full API coverage](#full-api-coverage) |
| `networks`, `firewall`, `acl-rules`, `dns`, `traffic-lists`, `wans`, `vpn-servers`, `radius-profiles`, `device-tags`, `countries` | Typed resource commands — see [Full API coverage](#full-api-coverage) |

### `configure` — Save connection settings

```sh
unifi configure [flags]
```

Writes the supplied flags to the config file. Existing values are preserved for any flags not supplied.

```sh
UNIFI_API_KEY=my-api-key unifi configure --host 192.168.1.1 --insecure
# Saved configuration to ~/.config/unifi/config.json
```

---

### `info` — Show controller info

```sh
unifi info [flags]
```

Returns JSON with application/controller metadata.

```sh
unifi info   # uses saved config; or pass --host / --insecure ad hoc
```

---

### `sites` — List sites

#### `sites list`

```sh
unifi sites list [flags]
```

Lists all sites. Table output shows `ID` and `NAME`.

```sh
unifi sites list
# ID                                    NAME
# 66b1e2f4a1b2c3d4e5f60001              Default
```

```sh
unifi sites list -o json
```

---

### `devices` — Manage devices

#### `devices list`

```sh
unifi devices list [--site <id>] [--limit N] [--all] [flags]
```

Lists devices for the configured site. Table output shows `ID`, `NAME`, `MODEL`, `STATE`, and `MAC`.

```sh
unifi devices list --site default
# ID                                    NAME            MODEL     STATE    MAC
# 66b1e2f4a1b2c3d4e5f60002              Living Room AP  U6-Lite   online   aa:bb:cc:dd:ee:ff
```

#### `devices get <id>`

```sh
unifi devices get <device-id> [flags]
```

Shows full JSON detail for a single device.

```sh
unifi devices get 66b1e2f4a1b2c3d4e5f60002
```

#### `devices stats <id>`

```sh
unifi devices stats <device-id> [flags]
```

Shows the latest statistics for a device. The default JSON output is the full
object (`uptimeSec`, `cpuUtilizationPct`, `memoryUtilizationPct`, load averages,
`uplink.txRateBps`/`rxRateBps`, per-radio data). In table mode (`-o table`) it
renders a readable health summary instead.

```sh
unifi devices stats 66b1e2f4a1b2c3d4e5f60002
```

```sh
unifi devices stats 66b1e2f4a1b2c3d4e5f60002 -o table
# UPTIME          11h 57m
# CPU             20.2%
# MEMORY          84.7%
# LOAD (1/5/15m)  1.27 / 1.15 / 1.21
# UPLINK TX       99.14 Kbps
# UPLINK RX       117.47 Kbps
# LAST HEARTBEAT  2026-06-01T02:53:55Z
```

#### `devices restart <id>` *(mutating)*

```sh
unifi devices restart <device-id> --yes [flags]
```

Sends a `RESTART` action to the device. Requires `--yes`.

```sh
unifi devices restart 66b1e2f4a1b2c3d4e5f60002 --yes
```

#### `devices port-cycle <id>` *(mutating)*

```sh
unifi devices port-cycle <device-id> --port N --yes [flags]
```

Power-cycles the specified port (`--port N` is required). Requires `--yes`.

```sh
unifi devices port-cycle 66b1e2f4a1b2c3d4e5f60002 --port 3 --yes
```

---

### `clients` — Manage clients

#### `clients list`

```sh
unifi clients list [--site <id>] [--limit N] [--all] [flags]
```

Lists clients for the configured site. Table output shows `ID`, `NAME`, `IP`, and `MAC`.

```sh
unifi clients list
# ID                                    NAME             IP             MAC
# 66b1e2f4a1b2c3d4e5f60003              My Laptop        192.168.1.42   11:22:33:44:55:66
```

#### `clients get <id>`

```sh
unifi clients get <client-id> [flags]
```

Shows full JSON detail for a single client.

```sh
unifi clients get 66b1e2f4a1b2c3d4e5f60003
```

#### `clients authorize <id>` *(mutating)*

```sh
unifi clients authorize <client-id> --yes [flags]
```

Authorizes a guest client (`AUTHORIZE_GUEST_ACCESS` action). Requires `--yes`.

```sh
unifi clients authorize 66b1e2f4a1b2c3d4e5f60003 --yes
```

---

### `vouchers` — Manage hotspot vouchers

#### `vouchers list`

```sh
unifi vouchers list [--site <id>] [--limit N] [--all] [flags]
```

Lists hotspot vouchers. Always outputs JSON (both table and json modes).

```sh
unifi vouchers list
```

#### `vouchers create` *(mutating)*

```sh
unifi vouchers create [--name N] [--count C] [--minutes M] [--quota-mb MB] [--guests G] [--rx-rate Kbps] [--tx-rate Kbps] --yes [flags]
```

| Flag | Description | Default |
|------|-------------|---------|
| `--name` | Voucher name (the API requires one) | `Voucher` |
| `--count` | Number of vouchers to create | `1` |
| `--minutes` | Time limit in minutes | `1440` (24 h) |
| `--quota-mb` | Data usage limit in MB | unlimited |
| `--guests` | Authorized guest limit | unlimited |
| `--rx-rate` | Download rate limit in Kbps | unlimited |
| `--tx-rate` | Upload rate limit in Kbps | unlimited |

Requires `--yes`.

```sh
# Create 5 day-pass vouchers with a 1 GB data cap and 10/2 Mbps rate limits
unifi vouchers create --count 5 --minutes 1440 --quota-mb 1024 --rx-rate 10000 --tx-rate 2000 --name "Day Pass" --yes
```

#### `vouchers delete <id>` *(mutating)*

```sh
unifi vouchers delete <voucher-id> --yes [flags]
```

Deletes a voucher. Requires `--yes`.

```sh
unifi vouchers delete 66b1e2f4a1b2c3d4e5f60099 --yes
```

---

### `site-manager` — Site Manager cloud API (alias: `sm`)

```sh
unifi site-manager <subcommand> [args]
# or using the short alias:
unifi sm <subcommand> [args]
```

Account-wide cloud API at `https://api.ui.com`. Read-only. Requires only an API key — no `--host` or `--site` needed. Create a cloud key at [unifi.ui.com](https://unifi.ui.com) → **Settings → API Keys** (this key is typically different from your local console key; if cloud calls return 401/403 while local calls work, you need your cloud key in `UNIFI_API_KEY`).

```sh
# List all consoles on the account
unifi site-manager hosts list
unifi site-manager hosts get <hostId>

# Sites and devices across all hosts
unifi site-manager sites list
unifi site-manager devices list
unifi site-manager devices list --host-id <hostId>   # filter to one console

# ISP performance metrics
unifi site-manager isp-metrics get 1h --duration 7d
unifi site-manager isp-metrics get 5m --begin 2026-06-01T00:00:00Z --end 2026-06-02T00:00:00Z
unifi site-manager isp-metrics query 1h --data '{"sites":["<siteId>"]}'

# SD-WAN configurations
unifi site-manager sdwan list
unifi site-manager sdwan get <configId>
unifi site-manager sdwan status <configId>

# Escape hatch — any cloud endpoint
unifi site-manager api GET /v1/hosts
```

Supports `--all` (auto-paginate) and `--limit N`. Cloud commands are read-only and never require `--yes`. The `sm` alias is interchangeable with `site-manager`.

---

## Full API coverage

Beyond the curated `sites`/`devices`/`clients`/`vouchers` commands above, `unifi` exposes **every** UniFi Network Integration API endpoint through two complementary mechanisms:

1. **Typed resource commands** — a convenience layer for the common, well-known resource groups (networks, firewall, DNS, etc.). Consistent `list`/`get`/`create`/`update`/`delete` verbs, friendly table output, and automatic `{site}` resolution.
2. **The `api` passthrough** — a universal catch-all that issues a raw request to *any* path. Use it for anything not covered by a typed command, including undocumented or future-firmware endpoints. It always works because you supply the exact method and path.

> All `create`/`update`/`delete` typed commands, and **any** non-`GET` `api` call, require the `--yes` flag.

### `api` — Universal passthrough

```sh
unifi api <METHOD> <path> [--data <json>] [--data-file <file|->] [--query k=v]... [flags]
```

- `<METHOD>` is one of `GET`, `POST`, `PUT`, `PATCH`, `DELETE` (case-insensitive).
- `<path>` is the API path relative to the integration base. A leading `/` is added if missing.
- A `{site}` or `:site` token anywhere in the path is replaced with the resolved site (URL-escaped), so you don't have to hardcode the site ID.
- `--data` / `--data-file` supply a JSON request body and are **mutually exclusive**; the body must be valid JSON. `--data-file -` reads the body from stdin.
- `--query k=v` is repeatable and adds URL query parameters.
- Non-`GET` methods require `--yes`.
- Output is always JSON.

```sh
# GET firewall policies for the resolved site
unifi api GET /sites/{site}/firewall/policies

# Create a gateway-managed network (POST with an inline body) — requires --yes.
# The body varies by `management` type (GATEWAY, SWITCH, UNMANAGED); on
# versions before 10.3.58, GATEWAY also requires `mdnsForwardingEnabled`.
unifi api POST /sites/{site}/networks \
  --data '{"management":"GATEWAY","name":"IoT","enabled":true,"vlanId":40,"cellularBackupEnabled":false,"internetAccessEnabled":true,"isolationEnabled":false,"ipv4Configuration":{"autoScaleEnabled":false,"hostIpAddress":"192.168.40.1","prefixLength":24}}' --yes

# Delete a DNS policy — requires --yes
unifi api DELETE /sites/{site}/dns/policies/66b1e2f4a1b2c3d4e5f6abcd --yes

# Use --query for filtering / pagination knobs
unifi api GET /sites/{site}/clients --query limit=50 --query offset=0

# Read the body from a file (or stdin with -)
unifi api PUT /sites/{site}/networks/<id> --data-file ./network.json --yes

# Reach a resource that has no typed command (e.g. WiFi broadcasts / SSIDs)
unifi api GET /sites/{site}/wifi/broadcasts
```

### Typed resource groups

Each group supports `list`, `get`, `create`, `update`, and `delete` unless the table says otherwise. All paths are site-scoped (`/sites/{site}/...`) except `countries`. Paths below match the OpenAPI spec of UniFi Network application **v10.6.106**. The CLI works on every v10 version from **10.0.162** to **10.6.106**, except that `firewall policies` and `dns` need **10.1.84** or later (10.0.162 has no such endpoints).

| Group | Path | Ops |
|-------|------|-----|
| `networks` | `/sites/{site}/networks` | list, get, create, update, delete |
| `firewall zones` | `/sites/{site}/firewall/zones` | list, get, create, update, delete |
| `firewall policies` | `/sites/{site}/firewall/policies` | list, get, create, update, delete |
| `acl-rules` | `/sites/{site}/acl-rules` | list, get, create, update, delete |
| `dns` | `/sites/{site}/dns/policies` | list, get, create, update, delete |
| `traffic-lists` | `/sites/{site}/traffic-matching-lists` | list, get, create, update, delete |
| `wans` | `/sites/{site}/wans` | list |
| `vpn-servers` | `/sites/{site}/vpn/servers` | list |
| `radius-profiles` | `/sites/{site}/radius/profiles` | list |
| `device-tags` | `/sites/{site}/device-tags` | list |
| `countries` | `/countries` | list |

> **No typed command:** WiFi broadcasts (`/sites/{site}/wifi/broadcasts`), DPI (`/dpi/applications`, `/dpi/categories`), pending devices (`/pending-devices`), switching (`/sites/{site}/switching/lags`, `mc-lag-domains`, `switch-stacks`), site-to-site tunnels (`/sites/{site}/vpn/site-to-site-tunnels`), ACL and firewall-policy `ordering`, and network `references`. Reach them via the `api` passthrough, for example `unifi api GET /sites/{site}/wifi/broadcasts`. The v10.6.106 API has no `wlans`, `port-forwards`, or `traffic-routes` endpoint.

Generic command forms:

```sh
unifi <group> list
unifi <group> get <id>
unifi <group> create --data '<json>' --yes
unifi <group> update <id> --data '<json>' --yes
unifi <group> delete <id> --yes
```

For `firewall`, the sub-resource (`zones` or `policies`) comes first: `unifi firewall <sub> <action> [id]`.

In table mode (`-o table`), `list` shows `ID`, `NAME`, and `ENABLED` columns; the default JSON mode shows the full objects.

```sh
unifi firewall policies list
unifi firewall zones get 66b1e2f4a1b2c3d4e5f60010
unifi networks list -o table
unifi dns create --data '{"name":"block-ads","action":"BLOCK"}' --yes
unifi wans list
unifi countries list
```

> **Source of truth:** the exact schemas and paths for *your* console are documented in the UniFi Network app under **Settings → Integrations**. The typed paths above are best-effort against the official API surface; if a typed command's path differs on your firmware, fall back to `unifi api <METHOD> <path>` using the path from that documentation. The `api` passthrough accepts whatever path that doc specifies.

---

## Output Formats

The default output format is **json** (machine-readable, recommended for scripting and automation). For human-readable tables, opt in with `-o table`, `UNIFI_OUTPUT=table`, or `"output": "table"` in the config file.

| Mode | Behavior |
|------|----------|
| `json` (default) | Raw API JSON, pretty-printed — recommended for scripting and automation |
| `table` (`-o table`) | Human-readable columns for list commands; JSON pretty-print for single-item and action responses |

```sh
# Pipe device list into jq (json is the default)
unifi devices list | jq '.[].name'

# Human-readable table
unifi devices list -o table
```

## Pagination

List commands default to `--limit 25`. Use `--all` to auto-paginate and retrieve every item across all pages (page size 200 internally).

```sh
unifi clients list --all
unifi devices list --limit 50
```

## Safety: Mutating Commands

Mutating commands (`devices restart`, `devices port-cycle`, `clients authorize`, `vouchers create`, `vouchers delete`, every typed-resource `create`/`update`/`delete`, and any non-`GET` `api` call) **always** require the `--yes` flag, regardless of output format. The guard is independent of the `-o`/`UNIFI_OUTPUT` setting.

```sh
# Always requires --yes
unifi devices restart <id> --yes
```

## Security

`unifi` authenticates with an API key. Handle it carefully:

- **Prefer `UNIFI_API_KEY` or `unifi configure`** over `--api-key` on the
  command line — command-line arguments are visible to other local users (`ps`,
  `/proc`) and are written to shell history.
- The config file (`~/.config/unifi/config.json`) stores the key in plaintext
  with `0600` permissions. Don't commit it or loosen its permissions; `unifi`
  warns if it finds the file readable by other users.
- **TLS verification is on by default.** Only use `--insecure` /
  `UNIFI_INSECURE=true` for local consoles with self-signed certificates, and
  only when you trust the network path to the controller. The API key is
  stripped if the controller redirects to a different host.
- `--data-file` reads from the local filesystem with your full permissions;
  don't point it at untrusted input in automated pipelines.

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Claude skill

This repo bundles a Claude Code skill, **`unifi-network-manager`**, that teaches
Claude when and how to drive this CLI (site discovery, read-before-write,
the `api` passthrough, and `--yes` confirmation for mutations). The source lives
at `skills/unifi-network-manager/SKILL.md` and is symlinked into
`~/.claude/skills/` by `make install` (or `make install-skill`). Because it is a
symlink, edits to the skill in this repo are live in Claude immediately.

The skill is **optional** — `make install` links it as a convenience, but the
CLI works without it. Install or remove it independently with `make
install-skill` / `make uninstall-skill`.

> **⚠️ Using this skill with an AI agent is at your own risk.** The skill is a
> best-effort convenience for driving the CLI with Claude. It can issue
> commands that change your network — restarting devices, cycling PoE ports,
> creating or deleting firewall policies, networks, and other resources. The
> project and its authors accept **no responsibility** for any outcome,
> including downtime, misconfiguration, lockout, or data loss, that results
> from an AI agent operating this tool. **You are solely responsible for the
> actions of any agent you run**, for reviewing what it proposes before
> confirming, and for keeping it scoped to networks you are authorized to
> manage. Mutating commands always require explicit `--yes` confirmation — keep
> a human in the loop.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | Runtime or API error |
| `2` | Unknown command or usage error |

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `x509: certificate signed by unknown authority` | Pass `--insecure` or set `UNIFI_INSECURE=true` |
| `API error 401` / `403` | Bad or missing API key — check `--api-key` / `UNIFI_API_KEY` |
| `API error 429` | Rate limited — the CLI prints the `retry-after` value in seconds |
| `host is required` | Run `unifi configure --host ...` or set `UNIFI_HOST` |
| `config file ... has permissions ...` | Restrict it: `chmod 600 ~/.config/unifi/config.json` |

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). The project
is standard-library only and developed test-first; run `make test` (which
includes the race detector) before sending a PR.

## License

Released under the [MIT License](LICENSE). © 2026 Colin Dickson.
