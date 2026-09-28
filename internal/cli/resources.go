package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"text/tabwriter"
)

// resourceOps records which CRUD operations a resource supports.
type resourceOps struct{ list, get, create, update, delete bool }

// allOps enables every operation.
var allOps = resourceOps{list: true, get: true, create: true, update: true, delete: true}

var listOnly = resourceOps{list: true}

// resourceDef declares a typed REST resource exposed as a CLI command. Paths
// are templates with a {site} token where the resource is site-scoped. These
// are the single source of truth for typed resource paths; the universal `api`
// command remains the guaranteed-correct escape hatch if any need adjusting.
type resourceDef struct {
	group string // e.g. "networks", "firewall", "dns"
	sub   string // e.g. "zones", "policies" for firewall; "" otherwise
	path  string // template, e.g. "/sites/{site}/firewall/policies"
	ops   resourceOps
}

// Paths verified against a live UniFi Network console (application v10.6.106, per its OpenAPI spec).
// Resources without a typed command (e.g. wifi/broadcasts, DPI, switching)
// remain reachable via the universal `api` passthrough. Use `unifi api GET <path>` to probe new resources.
var resources = []resourceDef{
	{group: "networks", path: "/sites/{site}/networks", ops: allOps},
	{group: "firewall", sub: "zones", path: "/sites/{site}/firewall/zones", ops: allOps},
	{group: "firewall", sub: "policies", path: "/sites/{site}/firewall/policies", ops: allOps},
	{group: "acl-rules", path: "/sites/{site}/acl-rules", ops: allOps},
	{group: "dns", path: "/sites/{site}/dns/policies", ops: allOps},
	{group: "traffic-lists", path: "/sites/{site}/traffic-matching-lists", ops: allOps},
	{group: "wans", path: "/sites/{site}/wans", ops: listOnly},
	{group: "vpn-servers", path: "/sites/{site}/vpn/servers", ops: listOnly},
	{group: "radius-profiles", path: "/sites/{site}/radius/profiles", ops: listOnly},
	{group: "device-tags", path: "/sites/{site}/device-tags", ops: listOnly},
	{group: "countries", path: "/countries", ops: listOnly},
}

// findResource returns the resourceDef matching group and sub, or nil.
func findResource(group, sub string) *resourceDef {
	for i := range resources {
		if resources[i].group == group && resources[i].sub == sub {
			return &resources[i]
		}
	}
	return nil
}

// dispatchResource routes a resource command. handled is false when group
// matches no registered resource so the caller can emit an unknown-command
// error.
func dispatchResource(group, action string, positionals, args []string, stdout, stderr io.Writer) (handled bool, code int) {
	var def *resourceDef
	var resourceAction string
	var id string

	if group == "firewall" {
		// firewall <sub> <action> [id] — action token is the sub.
		sub := action
		def = findResource("firewall", sub)
		if def == nil {
			fmt.Fprintf(stderr, "unifi: unknown firewall subcommand %q (expected zones or policies)\n", sub)
			return true, 2
		}
		resourceAction = "list"
		if len(positionals) > 0 {
			resourceAction = positionals[0]
		}
		if len(positionals) > 1 {
			id = positionals[1]
		}
	} else {
		def = findResource(group, "")
		if def == nil {
			return false, 0
		}
		resourceAction = action
		if resourceAction == "" {
			resourceAction = "list"
		}
		if len(positionals) > 0 {
			id = positionals[0]
		}
	}

	switch resourceAction {
	case "list":
		if !def.ops.list {
			break
		}
		return true, resourceList(def, args, stdout, stderr)
	case "get":
		if !def.ops.get {
			break
		}
		return true, resourceGet(def, id, args, stdout, stderr)
	case "create":
		if !def.ops.create {
			break
		}
		return true, resourceCreate(def, args, stdout, stderr)
	case "update":
		if !def.ops.update {
			break
		}
		return true, resourceUpdate(def, id, args, stdout, stderr)
	case "delete":
		if !def.ops.delete {
			break
		}
		return true, resourceDelete(def, id, args, stdout, stderr)
	}

	fmt.Fprintf(stderr, "unifi: unknown or unsupported action %q for %s\n", resourceAction, def.group)
	return true, 2
}

// resolveResourcePath substitutes the site token (when present) into the
// resource path, resolving the site from config.
func resolveResourcePath(def *resourceDef, g globals, stderr io.Writer) (API, string, bool) {
	api, eff, ok := buildAPI(g, stderr)
	if !ok {
		return nil, "", false
	}
	path := def.path
	if pathHasSiteToken(path) {
		site, err := resolveSite(eff)
		if err != nil {
			fmt.Fprintf(stderr, "unifi: %v\n", err)
			return nil, "", false
		}
		path = substituteSite(path, site)
	}
	return api, path, true
}

func resourceList(def *resourceDef, args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet(def.group+" list", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, path, ok := resolveResourcePath(def, g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	items, err := api.ListPath(ctx, path, g.all, 0, g.limit)
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
	fmt.Fprintln(tw, "ID\tNAME\tENABLED")
	for _, raw := range items {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		fmt.Fprintf(tw, "%s\t%s\t%s\n", strField(m, "id"), strField(m, "name"), strField(m, "enabled"))
	}
	if err := tw.Flush(); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

// strField formats a map field for table output, omitting missing values.
func strField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}

func resourceGet(def *resourceDef, id string, args []string, stdout, stderr io.Writer) int {
	if id == "" {
		fmt.Fprintf(stderr, "unifi: %s get requires an id\n", def.group)
		return 2
	}
	var g globals
	fs := newFlagSet(def.group+" get", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	api, path, ok := resolveResourcePath(def, g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.Do(ctx, "GET", path+"/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func resourceCreate(def *resourceDef, args []string, stdout, stderr io.Writer) int {
	var g globals
	fs := newFlagSet(def.group+" create", &g, stderr)
	var df dataFlags
	df.register(fs)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	body, hasBody, err := df.body(stdinReader())
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	if !hasBody {
		fmt.Fprintf(stderr, "unifi: %s create requires --data or --data-file\n", def.group)
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	api, path, ok := resolveResourcePath(def, g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.Do(ctx, "POST", path, nil, body)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func resourceUpdate(def *resourceDef, id string, args []string, stdout, stderr io.Writer) int {
	if id == "" {
		fmt.Fprintf(stderr, "unifi: %s update requires an id\n", def.group)
		return 2
	}
	var g globals
	fs := newFlagSet(def.group+" update", &g, stderr)
	var df dataFlags
	df.register(fs)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	body, hasBody, err := df.body(stdinReader())
	if err != nil {
		fmt.Fprintf(stderr, "unifi: %v\n", err)
		return 1
	}
	if !hasBody {
		fmt.Fprintf(stderr, "unifi: %s update requires --data or --data-file\n", def.group)
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	api, path, ok := resolveResourcePath(def, g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.Do(ctx, "PUT", path+"/"+url.PathEscape(id), nil, body)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}

func resourceDelete(def *resourceDef, id string, args []string, stdout, stderr io.Writer) int {
	if id == "" {
		fmt.Fprintf(stderr, "unifi: %s delete requires an id\n", def.group)
		return 2
	}
	var g globals
	fs := newFlagSet(def.group+" delete", &g, stderr)
	if !parseFlags(fs, &g, args, stderr) {
		return 1
	}
	if !g.mutationAllowed(stderr) {
		return 1
	}
	api, path, ok := resolveResourcePath(def, g, stderr)
	if !ok {
		return 1
	}
	ctx, cancel := ctxWithTimeout()
	defer cancel()
	raw, err := api.Do(ctx, "DELETE", path+"/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return renderError(stderr, err)
	}
	if err := printJSON(stdout, raw); err != nil {
		return renderError(stderr, err)
	}
	return 0
}
