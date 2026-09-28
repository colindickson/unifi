// Package cli implements the unifi command-line interface. It wires command
// routing, configuration resolution, and output formatting on top of the
// internal client and config packages.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/colindickson/unifi/internal/client"
	"github.com/colindickson/unifi/internal/config"
)

// API is the subset of the UniFi client used by the CLI. It is defined here so
// commands depend on an interface rather than the concrete client, allowing
// tests to inject a fake. *client.Client satisfies this interface.
type API interface {
	Info(ctx context.Context) (json.RawMessage, error)
	Sites(ctx context.Context, all bool, offset, limit int) ([]json.RawMessage, error)
	Devices(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error)
	Device(ctx context.Context, siteID, deviceID string) (json.RawMessage, error)
	DeviceStats(ctx context.Context, siteID, deviceID string) (json.RawMessage, error)
	DeviceAction(ctx context.Context, siteID, deviceID, action string) (json.RawMessage, error)
	PortAction(ctx context.Context, siteID, deviceID string, portIdx int, action string) (json.RawMessage, error)
	Clients(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error)
	ClientItem(ctx context.Context, siteID, clientID string) (json.RawMessage, error)
	ClientAction(ctx context.Context, siteID, clientID, action string) (json.RawMessage, error)
	Vouchers(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error)
	CreateVouchers(ctx context.Context, siteID string, req client.CreateVoucherRequest) (json.RawMessage, error)
	DeleteVoucher(ctx context.Context, siteID, voucherID string) (json.RawMessage, error)

	// Do is the universal request passthrough; ListPath paginates an arbitrary
	// list endpoint. Both back the `api` command and the resource registry.
	Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error)
	ListPath(ctx context.Context, path string, all bool, offset, limit int) ([]json.RawMessage, error)
}

// newAPI constructs the API implementation. It is a package variable so tests
// can replace it with a fake and capture the resolved config.
var newAPI = func(cfg config.Config) API { return client.New(cfg) }

// callTimeout bounds individual API calls.
const callTimeout = 30 * time.Second

// globals holds parsed global flags shared by all commands.
type globals struct {
	host     string
	apiKey   string
	site     string
	insecure bool
	output   string // raw -o/--output flag value ("" when unset)
	limit    int
	all      bool
	yes      bool

	// resolved is the full effective config (file < env < flags), computed
	// once during flag parsing and reused by buildAPI.
	resolved config.Config

	// effectiveOutput is the resolved output format (flag > env > file, default
	// json), computed after flag parsing. Commands read it via isJSON().
	effectiveOutput string

	// oAliasPtr points at the value bound to the -o output alias, reconciled
	// into output after flag parsing.
	oAliasPtr *string

	// insecureSet reports whether the user explicitly passed --insecure on the
	// command line. It is required to honour flag > env > file precedence for
	// the tri-state Insecure value.
	insecureSet bool
}

// flagCfg builds a config.Config from the raw global flag values (only the
// connection-related ones plus the raw output flag). It uses the RAW g.output
// so that `configure` persists only what the user explicitly typed.
func (g globals) flagCfg() config.Config {
	return config.Config{
		Host:     g.host,
		APIKey:   g.apiKey,
		Site:     g.site,
		Insecure: g.insecure,
		Output:   g.output,
	}
}

// isJSON reports whether the effective output mode is json.
func (g globals) isJSON() bool { return g.effectiveOutput == "json" }

// Run is the testable entrypoint. args is the full os.Args (args[0] is the
// program name). It returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	prog := "unifi"
	rest := args[1:]

	// Top-level help, no args, or -h/--help anywhere on the line. We honour
	// the help flag for every command (e.g. `unifi devices list --help`) by
	// short-circuiting to the usage text before any flag parsing or API call.
	if len(rest) == 0 || hasHelpFlag(rest) {
		printUsage(stdout)
		return 0
	}

	// Identify group/action tokens (first two non-flag tokens). We scan for
	// the leading positional tokens; everything is then re-parsed per command.
	group, action, positionals, ok := splitTokens(rest)
	if !ok {
		// Only flags present and none of them help: show usage.
		printUsage(stdout)
		return 0
	}

	switch group {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "configure":
		return cmdConfigure(rest, stdout, stderr)
	case "info":
		return cmdInfo(rest, stdout, stderr)
	case "sites":
		return cmdSites(action, rest, stdout, stderr)
	case "devices":
		return cmdDevices(action, positionals, rest, stdout, stderr)
	case "clients":
		return cmdClients(action, positionals, rest, stdout, stderr)
	case "vouchers":
		return cmdVouchers(action, positionals, rest, stdout, stderr)
	case "api":
		return cmdAPI(action, positionals, rest, stdout, stderr)
	case "site-manager", "sm":
		return cmdSiteManager(action, positionals, rest, stdout, stderr)
	default:
		if handled, code := dispatchResource(group, action, positionals, rest, stdout, stderr); handled {
			return code
		}
		fmt.Fprintf(stderr, "%s: unknown command %q\n\n", prog, group)
		printUsage(stderr)
		return 2
	}
}

// splitTokens returns the first non-flag token (group), the second non-flag
// token (action, may be ""), and all subsequent non-flag tokens (positionals).
// ok is false when there are no positional tokens at all.
func splitTokens(args []string) (group, action string, positionals []string, ok bool) {
	var pos []string
	skipNext := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			// A flag. If it's a known value-taking flag in "--name value"
			// form, skip its value too. We can't fully know flag arity here,
			// but global/command flags that take values are all of the form
			// "--flag value" or "--flag=value". To stay robust we only treat
			// "--flag=value" inline; for "--flag value" we conservatively skip
			// the next token when this flag is a known value flag.
			if !strings.Contains(a, "=") && takesValue(a) {
				skipNext = true
			}
			continue
		}
		pos = append(pos, a)
	}
	if len(pos) == 0 {
		return "", "", nil, false
	}
	group = pos[0]
	if len(pos) > 1 {
		action = pos[1]
	}
	if len(pos) > 2 {
		positionals = pos[2:]
	}
	return group, action, positionals, true
}

// takesValue reports whether the given flag token consumes the following
// argument as its value (used only by splitTokens to find positionals).
func takesValue(flag string) bool {
	name := strings.TrimLeft(flag, "-")
	switch name {
	case "host", "api-key", "site", "o", "output", "limit", "port",
		"name", "count", "minutes", "quota-mb", "guests", "rx-rate", "tx-rate",
		"data", "data-file", "query",
		"host-id", "duration", "begin", "end":
		return true
	}
	return false
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// buildAPI validates the already-resolved config and constructs the API. On
// failure it writes to stderr and returns ok=false with the exit code.
func buildAPI(g globals, stderr io.Writer) (API, config.Config, bool) {
	eff := g.resolved
	if err := eff.Validate(); err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return nil, eff, false
	}
	return newAPI(eff), eff, true
}

// resolveSite returns the site to use, or an error if none is configured.
func resolveSite(eff config.Config) (string, error) {
	if eff.Site != "" {
		return eff.Site, nil
	}
	return "", errors.New("site is required (set --site, UNIFI_SITE, or run `unifi configure`)")
}

// substituteSite replaces the {site} and :site tokens in path with the
// url-escaped site value. It is used by both the api command and the resource
// registry.
//
// Token matching is segment-exact: a path segment is the token when it equals
// "{site}" or ":site" exactly (split on "/"). This prevents ":site" from
// matching substrings like ":siteId".
func substituteSite(path, site string) string {
	esc := url.PathEscape(site)
	segs := strings.Split(path, "/")
	for i, seg := range segs {
		if seg == "{site}" || seg == ":site" {
			segs[i] = esc
		}
	}
	return strings.Join(segs, "/")
}

// pathHasSiteToken reports whether path contains a site placeholder.
// Like substituteSite, matching is segment-exact to avoid false positives on
// tokens such as ":siteId".
func pathHasSiteToken(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		if seg == "{site}" || seg == ":site" {
			return true
		}
	}
	return false
}

// ctxWithTimeout returns a 30s-bounded context.
func ctxWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), callTimeout)
}

// ---- output helpers ---------------------------------------------------------

// printJSON pretty-prints raw with 2-space indent and a trailing newline.
func printJSON(w io.Writer, raw json.RawMessage) error {
	if len(raw) == 0 {
		fmt.Fprintln(w)
		return nil
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		// Not indentable JSON; print as-is.
		fmt.Fprintln(w, string(raw))
		return nil
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// printJSONList marshals items into a JSON array and pretty-prints it.
func printJSONList(w io.Writer, items []json.RawMessage) error {
	if items == nil {
		items = []json.RawMessage{}
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	return printJSON(w, data)
}

// renderError writes an error to stderr in the standard CLI format and returns
// the exit code (always 1).
func renderError(stderr io.Writer, err error) int {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		msg := fmt.Sprintf("unifi: API error %d: %s", apiErr.StatusCode, apiErr.Message)
		if apiErr.RetryAfter > 0 {
			msg += fmt.Sprintf(" (retry after %ds)", apiErr.RetryAfter)
		}
		fmt.Fprintln(stderr, msg)
		return 1
	}
	msg := fmt.Sprintf("unifi: %v", err)
	if s := err.Error(); strings.Contains(s, "certificate") || strings.Contains(s, "x509") {
		msg += " — for self-signed console certs, pass --insecure or set UNIFI_INSECURE=true"
	}
	fmt.Fprintln(stderr, msg)
	return 1
}

// printUsage writes the top-level usage text.
func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: unifi [global flags] <command> [args]

Commands:
  configure                 Save connection settings to the config file
  info                      Show controller / application info
  sites list                List sites
  devices list              List devices for a site
  devices get <id>          Show a device
  devices stats <id>        Show latest device statistics
  devices restart <id>      Restart a device (mutating)
  devices port-cycle <id>   Power-cycle a port (--port N) (mutating)
  clients list              List clients for a site
  clients get <id>          Show a client
  clients authorize <id>    Authorize a guest client (mutating)
  vouchers list             List hotspot vouchers
  vouchers create           Create vouchers (mutating)
  vouchers delete <id>      Delete a voucher (mutating)
  help                      Show this help

Universal passthrough:
  api <method> <path>       Call any API endpoint directly. <method> is one of
                            GET/POST/PUT/PATCH/DELETE. {site} / :site tokens in
                            <path> are substituted from the resolved site.
                            Flags: --data <json>, --data-file <file> (- = stdin),
                            --query k=v (repeatable). Non-GET requires --yes.

Site Manager (cloud) — account-wide API at api.ui.com (alias: sm, read-only):
  site-manager hosts list         List consoles on the account
  site-manager hosts get <id>     Show a host
  site-manager sites list         List sites across all hosts
  site-manager devices list       List devices (--host-id <id> to filter)
  site-manager isp-metrics get <5m|1h>    ISP metrics (--duration|--begin/--end)
  site-manager isp-metrics query <5m|1h>  Filtered ISP query (--data)
  site-manager sdwan list|get|status      SD-WAN configs
  site-manager api <method> <path>        Cloud passthrough
  Cloud commands need only an API key (no --host/--site).

Resources (list/get/create/update/delete unless noted; writes require --yes):
  networks <action> [id]    /sites/{site}/networks
  firewall zones <action>   /sites/{site}/firewall/zones
  firewall policies <action> /sites/{site}/firewall/policies
  acl-rules <action> [id]   /sites/{site}/acl-rules
  dns <action> [id]         /sites/{site}/dns/policies
  traffic-lists <action>    /sites/{site}/traffic-matching-lists
  wans <action> [id]        /sites/{site}/wans
  vpn-servers <action> [id] /sites/{site}/vpn/servers
  radius-profiles <action>  /sites/{site}/radius/profiles
  device-tags <action>      /sites/{site}/device-tags
  countries list            /countries (list only)

  wans, vpn-servers, radius-profiles, and device-tags support list only.
  Endpoints without a typed command (e.g. wifi/broadcasts, dpi, switching)
  are reachable via: unifi api GET <path>

  For resources, create/update read the JSON body from --data or --data-file.

Global flags:
  --host <host>             Controller host
  --api-key <key>           API key
  --site <id>               Site ID
  --insecure                Skip TLS certificate verification
  -o, --output <table|json> Output format (default json)
  --limit <n>               Page size for list commands (default 25)
  --all                     Fetch all pages
  --yes                     Confirm mutating commands
  -h, --help                Show help

Output defaults to json (machine-readable). For human-readable tables pass
-o table, set UNIFI_OUTPUT=table, or add "output": "table" to the config file.

Configuration precedence (lowest to highest): config file, environment
(UNIFI_HOST, UNIFI_API_KEY, UNIFI_SITE, UNIFI_INSECURE, UNIFI_OUTPUT), then flags.
`)
}
