package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/colindickson/unifi/internal/client"
	"github.com/colindickson/unifi/internal/config"
)

// newFlagSet builds a FlagSet wired with all global flags, binding into g.
// Command-specific flags can be registered on the returned set by the caller
// before parsing. Output is discarded so usage errors are handled by us.
func newFlagSet(name string, g *globals, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var oAlias string
	fs.StringVar(&g.host, "host", "", "controller host")
	fs.StringVar(&g.apiKey, "api-key", "", "API key")
	fs.StringVar(&g.site, "site", "", "site ID")
	fs.BoolVar(&g.insecure, "insecure", false, "skip TLS verification")
	fs.StringVar(&g.output, "output", "", "output format")
	fs.StringVar(&oAlias, "o", "", "output format (alias)")
	fs.IntVar(&g.limit, "limit", 25, "page size")
	fs.BoolVar(&g.all, "all", false, "fetch all pages")
	fs.BoolVar(&g.yes, "yes", false, "confirm mutating commands")
	// Note: -h/--help is handled centrally in Run before any command parses
	// flags, so it is intentionally not registered here.

	fs.Usage = func() {}
	// The -o alias is reconciled into output after Parse via reconcile().
	g.oAliasPtr = &oAlias
	return fs
}

// reconcile folds the -o alias into output after flag parsing.
func (g *globals) reconcile() {
	if g.oAliasPtr != nil && *g.oAliasPtr != "" {
		g.output = *g.oAliasPtr
	}
}

// mutationAllowed enforces the mutation guard. It returns ok=false (and writes
// to stderr) when a mutating command is run without --yes. The guard is
// independent of the output format.
func (g globals) mutationAllowed(stderr io.Writer) bool {
	if g.yes {
		return true
	}
	fmt.Fprintln(stderr, "unifi: refusing to run mutating command without --yes")
	return false
}

// parseFlags filters args down to flag tokens (Go's flag package stops at the
// first positional, so we pre-extract flags to allow them anywhere on the line)
// and parses them into the flag set, then reconciles the -o alias.
func parseFlags(fs *flag.FlagSet, g *globals, args []string, stderr io.Writer) bool {
	flagsOnly := extractFlags(args)
	if err := fs.Parse(flagsOnly); err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return false
	}
	// Record which presence-sensitive flags the user actually set so config
	// resolution can honour flag > env > file precedence for tri-state values.
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "insecure" {
			g.insecureSet = true
		}
	})
	g.reconcile()

	// Resolve file < env < flag config exactly once per invocation. Warn (but
	// don't fail) if the config file is readable by other users.
	path := config.DefaultPath()
	if w := config.InsecurePermsWarning(path); w != "" {
		fmt.Fprintf(stderr, "unifi: warning: %s\n", w)
	}
	fileCfg, _ := config.Load(path)
	envCfg, envInsecureSet := config.FromEnvSource()
	g.resolved = config.Resolve(fileCfg, envCfg, envInsecureSet, g.flagCfg(), g.insecureSet)
	g.effectiveOutput = g.resolved.Output

	if !config.ValidOutput(g.effectiveOutput) {
		fmt.Fprintf(stderr, "unifi: unknown output format %q (want json or table)\n", g.effectiveOutput)
		return false
	}
	return true
}

// extractFlags returns only the flag tokens (and their values) from args,
// dropping positional tokens (group/action/ids). This lets the flag package
// parse flags that appear anywhere on the line.
func extractFlags(args []string) []string {
	var out []string
	skipNext := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if skipNext {
			out = append(out, a)
			skipNext = false
			continue
		}
		if len(a) > 0 && a[0] == '-' {
			out = append(out, a)
			if !strings.Contains(a, "=") && takesValue(a) {
				skipNext = true
			}
			continue
		}
		// positional: drop.
	}
	return out
}

// ---- configure --------------------------------------------------------------

func cmdConfigure(args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet("configure", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}

	path := config.DefaultPath()
	existing, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	merged := config.Merge(existing, g.flagCfg())
	if err := config.Save(path, merged); err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Saved configuration to %s\n", path)
	return 0
}

// ---- info -------------------------------------------------------------------

func cmdInfo(args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet("info", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, _, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.Info(ctx)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

// ---- sites ------------------------------------------------------------------

func cmdSites(action string, args []string, stdout, stderr io.Writer) int {
	if action != "" && action != "list" {
		fmt.Fprintf(stderr, "unifi: unknown sites action %q\n", action)
		return 2
	}
	var g globals
	fs := newFlagSet("sites", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, _, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	items, err := api.Sites(ctx, g.all, 0, g.limit)
	if err != nil {
		return renderError(stderr, err)
	}
	if g.isJSON() {
		if err := printJSONList(stdout, items); err != nil {
			return renderError(stderr, err)
		}
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME")
	for _, raw := range items {
		var s client.Site
		_ = json.Unmarshal(raw, &s)
		fmt.Fprintf(tw, "%s\t%s\n", s.ID, s.Name)
	}
	if err := tw.Flush(); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

// ---- devices ----------------------------------------------------------------

func cmdDevices(action string, positionals, args []string, stdout, stderr io.Writer) int {
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		return devicesList(args, stdout, stderr)
	case "get":
		return devicesShow(positionals, args, stdout, stderr, "get")
	case "stats":
		return devicesShow(positionals, args, stdout, stderr, "stats")
	case "restart":
		return devicesRestart(positionals, args, stdout, stderr)
	case "port-cycle":
		return devicesPortCycle(positionals, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unifi: unknown devices action %q\n", action)
		return 2
	}
}

func devicesList(args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet("devices list", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	items, err := api.Devices(ctx, site, g.all, 0, g.limit)
	if err != nil {
		return renderError(stderr, err)
	}
	if g.isJSON() {
		if err := printJSONList(stdout, items); err != nil {
			return renderError(stderr, err)
		}
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tMODEL\tSTATE\tMAC")
	for _, raw := range items {
		var d client.Device
		_ = json.Unmarshal(raw, &d)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Model, d.State, d.MacAddress)
	}
	if err := tw.Flush(); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func devicesShow(positionals, args []string, stdout, stderr io.Writer, kind string) int {
	if len(positionals) < 1 {
		fmt.Fprintf(stderr, "unifi: devices %s requires a device id\n", kind)
		return 2
	}
	id := positionals[0]
	var g globals
	fs := newFlagSet("devices "+kind, &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	var raw json.RawMessage
	if kind == "stats" {
		raw, err = api.DeviceStats(ctx, site, id)
	} else {
		raw, err = api.Device(ctx, site, id)
	}
	if err != nil {
		return renderError(stderr, err)
	}
	// In table mode, render device stats as a readable health summary instead
	// of a raw JSON blob. JSON mode (the default) stays lossless.
	if kind == "stats" && !g.isJSON() {
		printDeviceStats(stdout, raw)
		return 0
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

// printDeviceStats renders the latest-statistics object as a labeled summary.
// Unknown/missing fields are simply omitted, so it degrades gracefully across
// firmware versions and device types.
func printDeviceStats(w io.Writer, raw json.RawMessage) {
	var s struct {
		UptimeSec     *float64 `json:"uptimeSec"`
		CPUPct        *float64 `json:"cpuUtilizationPct"`
		MemPct        *float64 `json:"memoryUtilizationPct"`
		Load1         *float64 `json:"loadAverage1Min"`
		Load5         *float64 `json:"loadAverage5Min"`
		Load15        *float64 `json:"loadAverage15Min"`
		LastHeartbeat string   `json:"lastHeartbeatAt"`
		Uplink        *struct {
			TxRateBps *float64 `json:"txRateBps"`
			RxRateBps *float64 `json:"rxRateBps"`
		} `json:"uplink"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		// Not the shape we expect — fall back to raw JSON.
		_ = printJSON(w, raw)
		return
	}
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	row := func(label, val string) {
		if val != "" {
			fmt.Fprintf(tw, "%s\t%s\n", label, val)
		}
	}
	row("UPTIME", formatUptime(s.UptimeSec))
	row("CPU", pct(s.CPUPct))
	row("MEMORY", pct(s.MemPct))
	if s.Load1 != nil || s.Load5 != nil || s.Load15 != nil {
		row("LOAD (1/5/15m)", fmt.Sprintf("%s / %s / %s", num(s.Load1), num(s.Load5), num(s.Load15)))
	}
	if s.Uplink != nil {
		row("UPLINK TX", bitrate(s.Uplink.TxRateBps))
		row("UPLINK RX", bitrate(s.Uplink.RxRateBps))
	}
	row("LAST HEARTBEAT", s.LastHeartbeat)
	_ = tw.Flush() // best-effort; this helper renders to stdout and returns no error
}

func pct(v *float64) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%.1f%%", *v)
}

func num(v *float64) string {
	if v == nil {
		return "-"
	}
	return fmt.Sprintf("%.2f", *v)
}

func formatUptime(v *float64) string {
	if v == nil {
		return ""
	}
	total := int64(*v)
	d := total / 86400
	h := (total % 86400) / 3600
	m := (total % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh %dm", d, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func bitrate(v *float64) string {
	if v == nil {
		return ""
	}
	bps := *v
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.2f Gbps", bps/1e9)
	case bps >= 1e6:
		return fmt.Sprintf("%.2f Mbps", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.2f Kbps", bps/1e3)
	default:
		return fmt.Sprintf("%.0f bps", bps)
	}
}

func devicesRestart(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "unifi: devices restart requires a device id")
		return 2
	}
	id := positionals[0]
	var g globals
	fs := newFlagSet("devices restart", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.DeviceAction(ctx, site, id, "RESTART")
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func devicesPortCycle(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "unifi: devices port-cycle requires a device id")
		return 2
	}
	id := positionals[0]
	var g globals
	fs := newFlagSet("devices port-cycle", &g, stderr)
	port := -1
	fs.IntVar(&port, "port", -1, "port index")
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	if port < 0 {
		fmt.Fprintln(stderr, "unifi: devices port-cycle requires --port N")
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.PortAction(ctx, site, id, port, "POWER_CYCLE")
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

// ---- clients ----------------------------------------------------------------

func cmdClients(action string, positionals, args []string, stdout, stderr io.Writer) int {
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		return clientsList(args, stdout, stderr)
	case "get":
		return clientsGet(positionals, args, stdout, stderr)
	case "authorize":
		return clientsAuthorize(positionals, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unifi: unknown clients action %q\n", action)
		return 2
	}
}

func clientsList(args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet("clients list", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	items, err := api.Clients(ctx, site, g.all, 0, g.limit)
	if err != nil {
		return renderError(stderr, err)
	}
	if g.isJSON() {
		if err := printJSONList(stdout, items); err != nil {
			return renderError(stderr, err)
		}
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tIP\tMAC")
	for _, raw := range items {
		var c client.ClientInfo
		_ = json.Unmarshal(raw, &c)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.ID, c.Name, c.IPAddress, c.MacAddress)
	}
	if err := tw.Flush(); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func clientsGet(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "unifi: clients get requires a client id")
		return 2
	}
	id := positionals[0]
	var g globals
	fs := newFlagSet("clients get", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.ClientItem(ctx, site, id)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func clientsAuthorize(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "unifi: clients authorize requires a client id")
		return 2
	}
	id := positionals[0]
	var g globals
	fs := newFlagSet("clients authorize", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.ClientAction(ctx, site, id, "AUTHORIZE_GUEST_ACCESS")
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

// ---- vouchers ---------------------------------------------------------------

func cmdVouchers(action string, positionals, args []string, stdout, stderr io.Writer) int {
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		return vouchersList(args, stdout, stderr)
	case "create":
		return vouchersCreate(args, stdout, stderr)
	case "delete":
		return vouchersDelete(positionals, args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unifi: unknown vouchers action %q\n", action)
		return 2
	}
}

func vouchersList(args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet("vouchers list", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	items, err := api.Vouchers(ctx, site, g.all, 0, g.limit)
	if err != nil {
		return renderError(stderr, err)
	}
	// Vouchers have no fixed columns; pretty-print JSON in both modes.
	if err := printJSONList(stdout, items); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func vouchersCreate(args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet("vouchers create", &g, stderr)
	var (
		name    string
		count   int
		minutes int
		quotaMB int
		guests  int
		rxRate  int
		txRate  int
	)
	fs.StringVar(&name, "name", "Voucher", "voucher name (required by the API)")
	fs.IntVar(&count, "count", 1, "number of vouchers")
	fs.IntVar(&minutes, "minutes", 1440, "time limit in minutes")
	fs.IntVar(&quotaMB, "quota-mb", -1, "data usage limit (MB)")
	fs.IntVar(&guests, "guests", -1, "authorized guest limit")
	fs.IntVar(&rxRate, "rx-rate", -1, "download rate limit (Kbps)")
	fs.IntVar(&txRate, "tx-rate", -1, "upload rate limit (Kbps)")
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	req := client.CreateVoucherRequest{
		Name:             name,
		Count:            count,
		TimeLimitMinutes: minutes,
	}
	if quotaMB >= 0 {
		v := quotaMB
		req.DataUsageLimitMBytes = &v
	}
	if guests >= 0 {
		v := guests
		req.AuthorizedGuestLimit = &v
	}
	if rxRate >= 0 {
		v := rxRate
		req.RxRateLimitKbps = &v
	}
	if txRate >= 0 {
		v := txRate
		req.TxRateLimitKbps = &v
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.CreateVouchers(ctx, site, req)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func vouchersDelete(positionals, args []string, stdout, stderr io.Writer) int {
	if len(positionals) < 1 {
		fmt.Fprintln(stderr, "unifi: vouchers delete requires a voucher id")
		return 2
	}
	id := positionals[0]
	var g globals
	fs := newFlagSet("vouchers delete", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return 1
	}
	site, err := resolveSite(eff)
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.DeleteVoucher(ctx, site, id)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}
