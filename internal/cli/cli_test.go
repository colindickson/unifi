package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colindickson/unifi/internal/client"
	"github.com/colindickson/unifi/internal/config"
)

// fakeAPI records calls and returns canned responses.
type fakeAPI struct {
	infoRaw     json.RawMessage
	sitesRaw    []json.RawMessage
	devicesRaw  []json.RawMessage
	clientsRaw  []json.RawMessage
	vouchersRaw []json.RawMessage
	objRaw      json.RawMessage

	err error

	// recorded calls
	deviceActionCalled bool
	deviceActionArgs   [3]string

	portActionCalled bool
	portIdx          int
	portAction       string

	clientActionCalled bool
	clientActionArgs   [3]string

	createVoucherCalled bool
	createVoucherReq    client.CreateVoucherRequest

	deleteVoucherCalled bool

	// Do / ListPath passthrough recording
	doCalled  bool
	doMethod  string
	doPath    string
	doQuery   url.Values
	doBody    any
	listPath  string
	listAll   bool
	listLimit int

	// listRawItems is returned by ListPath when set.
	listRawItems []json.RawMessage
}

func (f *fakeAPI) Do(ctx context.Context, method, path string, query url.Values, body any) (json.RawMessage, error) {
	f.doCalled = true
	f.doMethod = method
	f.doPath = path
	f.doQuery = query
	f.doBody = body
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) ListPath(ctx context.Context, path string, all bool, offset, limit int) ([]json.RawMessage, error) {
	f.listPath = path
	f.listAll = all
	f.listLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	if f.listRawItems != nil {
		return f.listRawItems, nil
	}
	return f.sitesRaw, nil
}

func (f *fakeAPI) Info(ctx context.Context) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.infoRaw, nil
}

func (f *fakeAPI) Sites(ctx context.Context, all bool, offset, limit int) ([]json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.sitesRaw, nil
}

func (f *fakeAPI) Devices(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.devicesRaw, nil
}

func (f *fakeAPI) Device(ctx context.Context, siteID, deviceID string) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) DeviceStats(ctx context.Context, siteID, deviceID string) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) DeviceAction(ctx context.Context, siteID, deviceID, action string) (json.RawMessage, error) {
	f.deviceActionCalled = true
	f.deviceActionArgs = [3]string{siteID, deviceID, action}
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) PortAction(ctx context.Context, siteID, deviceID string, portIdx int, action string) (json.RawMessage, error) {
	f.portActionCalled = true
	f.portIdx = portIdx
	f.portAction = action
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) Clients(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.clientsRaw, nil
}

func (f *fakeAPI) ClientItem(ctx context.Context, siteID, clientID string) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) ClientAction(ctx context.Context, siteID, clientID, action string) (json.RawMessage, error) {
	f.clientActionCalled = true
	f.clientActionArgs = [3]string{siteID, clientID, action}
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) Vouchers(ctx context.Context, siteID string, all bool, offset, limit int) ([]json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.vouchersRaw, nil
}

func (f *fakeAPI) CreateVouchers(ctx context.Context, siteID string, req client.CreateVoucherRequest) (json.RawMessage, error) {
	f.createVoucherCalled = true
	f.createVoucherReq = req
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

func (f *fakeAPI) DeleteVoucher(ctx context.Context, siteID, voucherID string) (json.RawMessage, error) {
	f.deleteVoucherCalled = true
	if f.err != nil {
		return nil, f.err
	}
	return f.objRaw, nil
}

// withFakeAPI installs a fake API and returns a restore func.
func withFakeAPI(t *testing.T, f *fakeAPI) {
	t.Helper()
	orig := newAPI
	newAPI = func(cfg config.Config) API { return f }
	t.Cleanup(func() { newAPI = orig })
}

// withCapturedCfg installs a fake that also captures the resolved config.
func withCapturedCfg(t *testing.T, f *fakeAPI, got *config.Config) {
	t.Helper()
	orig := newAPI
	newAPI = func(cfg config.Config) API {
		*got = cfg
		return f
	}
	t.Cleanup(func() { newAPI = orig })
}

// clearEnv removes all UNIFI_* env vars for the duration of the test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"UNIFI_HOST", "UNIFI_API_KEY", "UNIFI_SITE", "UNIFI_INSECURE", "UNIFI_OUTPUT", "UNIFI_CONFIG"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	// Point config at a nonexistent file so Load returns zero.
	t.Setenv("UNIFI_CONFIG", filepath.Join(t.TempDir(), "nope.json"))
}

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	full := append([]string{"unifi"}, args...)
	code := Run(full, &out, &errb)
	return code, out.String(), errb.String()
}

func TestHelpAndNoArgs(t *testing.T) {
	clearEnv(t)
	for _, args := range [][]string{{}, {"help"}, {"--help"}, {"-h"}} {
		code, out, _ := run(args...)
		if code != 0 {
			t.Errorf("args %v: code=%d want 0", args, code)
		}
		if !strings.Contains(out, "Usage") {
			t.Errorf("args %v: stdout %q missing Usage", args, out)
		}
	}
}

func TestHelpFlagOnSubcommandPrintsUsage(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	// A fake whose calls would fail the test if invoked.
	f := &fakeAPI{devicesRaw: []json.RawMessage{json.RawMessage(`{"id":"d1"}`)}}
	withFakeAPI(t, f)

	for _, args := range [][]string{
		{"devices", "list", "--help"},
		{"devices", "-h"},
		{"vouchers", "create", "--help"},
		{"--help", "info"},
	} {
		code, out, errb := run(args...)
		if code != 0 {
			t.Errorf("args %v: code=%d want 0 (stderr=%s)", args, code, errb)
		}
		if !strings.Contains(out, "Usage") {
			t.Errorf("args %v: stdout %q missing Usage", args, out)
		}
	}
	if f.doCalled || f.deviceActionCalled {
		t.Error("help flag should short-circuit before any API call")
	}
}

func TestInvalidOutputFormatRejected(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{}
	withFakeAPI(t, f)

	code, _, errb := run("sites", "list", "-o", "xml")
	if code == 0 {
		t.Fatalf("invalid -o value should fail, got code=0")
	}
	if !strings.Contains(errb, "output") {
		t.Errorf("stderr %q should mention the output format", errb)
	}
}

func TestInsecureConfigPermsWarn(t *testing.T) {
	clearEnv(t)
	tmp := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(tmp, []byte(`{"host":"h","api_key":"k"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("UNIFI_CONFIG", tmp)
	f := &fakeAPI{infoRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)

	code, _, errb := run("info")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "permissions") {
		t.Errorf("stderr %q should warn about loose config file permissions", errb)
	}
}

func TestUnknownCommand(t *testing.T) {
	clearEnv(t)
	code, _, _ := run("bogus")
	if code != 2 {
		t.Errorf("code=%d want 2", code)
	}
}

func TestSitesListTableAndJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_OUTPUT", "")
	t.Setenv("UNIFI_HOST", "h.example.com")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{sitesRaw: []json.RawMessage{
		json.RawMessage(`{"id":"s1","name":"HQ"}`),
	}}
	withFakeAPI(t, f)

	// Table is no longer the default, so request it explicitly.
	code, out, errb := run("sites", "list", "-o", "table")
	if code != 0 {
		t.Fatalf("table: code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "HQ") {
		t.Errorf("table out %q missing site name", out)
	}

	code, out, errb = run("sites", "list", "-o", "json")
	if code != 0 {
		t.Fatalf("json: code=%d stderr=%s", code, errb)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("json out not valid JSON: %q", out)
	}
}

func TestOutputDefaultsToJSON(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_OUTPUT", "")
	t.Setenv("UNIFI_HOST", "h.example.com")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{sitesRaw: []json.RawMessage{
		json.RawMessage(`{"id":"s1","name":"HQ"}`),
	}}
	withFakeAPI(t, f)

	// No -o flag, no UNIFI_OUTPUT, no config file output: default is json.
	code, out, errb := run("sites", "list")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	trimmed := strings.TrimSpace(out)
	if !strings.HasPrefix(trimmed, "[") {
		t.Errorf("default out %q should be a JSON array (start with '[')", out)
	}
	if !json.Valid([]byte(out)) {
		t.Errorf("default out not valid JSON: %q", out)
	}
}

func TestOutputEnvTableRendersTable(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_OUTPUT", "table")
	t.Setenv("UNIFI_HOST", "h.example.com")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{sitesRaw: []json.RawMessage{
		json.RawMessage(`{"id":"s1","name":"HQ"}`),
	}}
	withFakeAPI(t, f)

	// UNIFI_OUTPUT=table with no -o flag renders a table.
	code, out, errb := run("sites", "list")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "HQ") {
		t.Errorf("env-table out %q should be table-shaped", out)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("env-table out %q should not be a JSON array", out)
	}
}

func TestOutputFlagOverridesEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_OUTPUT", "json")
	t.Setenv("UNIFI_HOST", "h.example.com")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{sitesRaw: []json.RawMessage{
		json.RawMessage(`{"id":"s1","name":"HQ"}`),
	}}
	withFakeAPI(t, f)

	// -o table must override UNIFI_OUTPUT=json.
	code, out, errb := run("sites", "list", "-o", "table")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "NAME") {
		t.Errorf("flag-over-env out %q should be table-shaped", out)
	}
}

func TestDeviceStatsTableSummary(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{
		"uptimeSec": 3661,
		"cpuUtilizationPct": 11.5,
		"memoryUtilizationPct": 88.2,
		"loadAverage1Min": 1.02,
		"uplink": {"txRateBps": 2000000, "rxRateBps": 1000000}
	}`)}
	withFakeAPI(t, f)

	// Table mode renders a labeled summary, not a raw JSON blob.
	code, out, errb := run("devices", "stats", "d1", "-o", "table")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	for _, want := range []string{"UPTIME", "CPU", "MEMORY", "11.5", "88.2"} {
		if !strings.Contains(out, want) {
			t.Errorf("table stats out missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "uptimeSec") {
		t.Errorf("table mode should not dump raw JSON keys, got:\n%s", out)
	}

	// JSON mode (default) still emits the raw object.
	code, out, errb = run("devices", "stats", "d1", "-o", "json")
	if code != 0 {
		t.Fatalf("json: code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "uptimeSec") || !json.Valid([]byte(out)) {
		t.Errorf("json mode should emit raw valid JSON, got:\n%s", out)
	}
}

func TestConfigPrecedenceEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "env-host")
	t.Setenv("UNIFI_API_KEY", "env-key")
	f := &fakeAPI{infoRaw: json.RawMessage(`{}`)}
	var got config.Config
	withCapturedCfg(t, f, &got)

	code, _, errb := run("info")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if got.Host != "env-host" || got.APIKey != "env-key" {
		t.Errorf("got cfg=%+v want host/key from env", got)
	}
}

func TestFlagOverridesEnv(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "env-host")
	t.Setenv("UNIFI_API_KEY", "env-key")
	f := &fakeAPI{infoRaw: json.RawMessage(`{}`)}
	var got config.Config
	withCapturedCfg(t, f, &got)

	code, _, errb := run("info", "--host", "flag-host")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if got.Host != "flag-host" {
		t.Errorf("got host=%q want flag-host", got.Host)
	}
}

func TestValidateFailure(t *testing.T) {
	clearEnv(t)
	f := &fakeAPI{}
	withFakeAPI(t, f)
	code, _, errb := run("info")
	if code != 1 {
		t.Fatalf("code=%d want 1", code)
	}
	if !strings.Contains(errb, "host") {
		t.Errorf("stderr %q missing host hint", errb)
	}
}

func TestMutationGuard(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")

	// table mode without --yes: refused, not called.
	f := &fakeAPI{}
	withFakeAPI(t, f)
	code, _, errb := run("devices", "restart", "d1")
	if code != 1 {
		t.Errorf("table no-yes: code=%d want 1", code)
	}
	if f.deviceActionCalled {
		t.Errorf("table no-yes: DeviceAction should not be called")
	}
	if !strings.Contains(errb, "--yes") {
		t.Errorf("stderr %q missing --yes hint", errb)
	}

	// with --yes: called.
	f2 := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f2)
	code, _, errb = run("devices", "restart", "d1", "--yes")
	if code != 0 {
		t.Errorf("with-yes: code=%d stderr=%s", code, errb)
	}
	if !f2.deviceActionCalled || f2.deviceActionArgs[2] != "RESTART" {
		t.Errorf("with-yes: action not called correctly: %+v", f2.deviceActionArgs)
	}

	// json mode without --yes: BLOCKED — the guard no longer keys off output.
	f3 := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f3)
	code, _, errb = run("devices", "restart", "d1", "-o", "json")
	if code != 1 {
		t.Errorf("json no-yes: code=%d want 1", code)
	}
	if f3.deviceActionCalled {
		t.Errorf("json no-yes: DeviceAction should not be called")
	}
	if !strings.Contains(errb, "--yes") {
		t.Errorf("json no-yes: stderr %q missing --yes hint", errb)
	}
}

func TestPortCycle(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")

	// missing --port → error.
	f := &fakeAPI{}
	withFakeAPI(t, f)
	code, _, _ := run("devices", "port-cycle", "d1", "--yes")
	if code == 0 {
		t.Errorf("missing port: code=%d want nonzero", code)
	}
	if f.portActionCalled {
		t.Errorf("missing port: PortAction should not be called")
	}

	// with --port 3.
	f2 := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f2)
	code, _, errb := run("devices", "port-cycle", "d1", "--port", "3", "--yes")
	if code != 0 {
		t.Fatalf("with port: code=%d stderr=%s", code, errb)
	}
	if !f2.portActionCalled || f2.portIdx != 3 || f2.portAction != "POWER_CYCLE" {
		t.Errorf("port action wrong: called=%v idx=%d action=%q", f2.portActionCalled, f2.portIdx, f2.portAction)
	}
}

func TestClientAuthorize(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")
	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)
	code, _, errb := run("clients", "authorize", "c1", "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.clientActionCalled || f.clientActionArgs[2] != "AUTHORIZE_GUEST_ACCESS" {
		t.Errorf("client action wrong: %+v", f.clientActionArgs)
	}
}

func TestVoucherCreate(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")

	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)
	code, _, errb := run("vouchers", "create", "--count", "5", "--minutes", "60", "-o", "json", "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !f.createVoucherCalled {
		t.Fatal("CreateVouchers not called")
	}
	if f.createVoucherReq.Count != 5 || f.createVoucherReq.TimeLimitMinutes != 60 {
		t.Errorf("req=%+v want count 5 minutes 60", f.createVoucherReq)
	}
	if f.createVoucherReq.AuthorizedGuestLimit != nil || f.createVoucherReq.DataUsageLimitMBytes != nil {
		t.Errorf("optional pointers should be nil: %+v", f.createVoucherReq)
	}

	// with --guests 2.
	f2 := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f2)
	code, _, errb = run("vouchers", "create", "--guests", "2", "-o", "json", "--yes")
	if code != 0 {
		t.Fatalf("guests: code=%d stderr=%s", code, errb)
	}
	if f2.createVoucherReq.AuthorizedGuestLimit == nil || *f2.createVoucherReq.AuthorizedGuestLimit != 2 {
		t.Errorf("AuthorizedGuestLimit want 2, got %v", f2.createVoucherReq.AuthorizedGuestLimit)
	}
}

func TestVoucherCreateDefaultName(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")

	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)
	if code, _, errb := run("vouchers", "create", "-o", "json", "--yes"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.createVoucherReq.Name == "" {
		t.Error("name is required by the API; want a non-empty default")
	}

	f2 := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f2)
	if code, _, errb := run("vouchers", "create", "--name", "Day Pass", "-o", "json", "--yes"); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f2.createVoucherReq.Name != "Day Pass" {
		t.Errorf("Name = %q, want %q", f2.createVoucherReq.Name, "Day Pass")
	}
}

func TestVoucherCreateRateLimits(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	t.Setenv("UNIFI_SITE", "s1")

	f := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f)
	code, _, errb := run("vouchers", "create", "--rx-rate", "1000", "--tx-rate", "2000", "-o", "json", "--yes")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if f.createVoucherReq.RxRateLimitKbps == nil || *f.createVoucherReq.RxRateLimitKbps != 1000 {
		t.Errorf("RxRateLimitKbps want 1000, got %v", f.createVoucherReq.RxRateLimitKbps)
	}
	if f.createVoucherReq.TxRateLimitKbps == nil || *f.createVoucherReq.TxRateLimitKbps != 2000 {
		t.Errorf("TxRateLimitKbps want 2000, got %v", f.createVoucherReq.TxRateLimitKbps)
	}

	// Omitting them leaves both nil.
	f2 := &fakeAPI{objRaw: json.RawMessage(`{}`)}
	withFakeAPI(t, f2)
	code, _, errb = run("vouchers", "create", "-o", "json", "--yes")
	if code != 0 {
		t.Fatalf("omit: code=%d stderr=%s", code, errb)
	}
	if f2.createVoucherReq.RxRateLimitKbps != nil || f2.createVoucherReq.TxRateLimitKbps != nil {
		t.Errorf("rate limit pointers should be nil when omitted: %+v", f2.createVoucherReq)
	}
}

func TestInsecureFlagPresenceResolvesTrue(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{infoRaw: json.RawMessage(`{}`)}
	var got config.Config
	withCapturedCfg(t, f, &got)

	code, _, errb := run("info", "--insecure")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !got.Insecure {
		t.Error("Insecure should be true when --insecure is passed")
	}
}

func TestInsecureFlagAbsenceResolvesFalse(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{infoRaw: json.RawMessage(`{}`)}
	var got config.Config
	withCapturedCfg(t, f, &got)

	code, _, errb := run("info")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if got.Insecure {
		t.Error("Insecure should be false when --insecure is absent and file/env unset")
	}
}

func TestInsecureFlagFalseOverridesFileTrue(t *testing.T) {
	clearEnv(t)
	tmp := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(tmp, config.Config{Host: "h", APIKey: "k", Insecure: true}); err != nil {
		t.Fatalf("save: %v", err)
	}
	t.Setenv("UNIFI_CONFIG", tmp)
	f := &fakeAPI{infoRaw: json.RawMessage(`{}`)}
	var got config.Config
	withCapturedCfg(t, f, &got)

	// No --insecure flag and no env: file's insecure:true must be preserved.
	code, _, errb := run("info")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !got.Insecure {
		t.Error("file insecure=true should be preserved when neither flag nor env set")
	}
}

func TestAPIErrorRendering(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNIFI_HOST", "h")
	t.Setenv("UNIFI_API_KEY", "k")
	f := &fakeAPI{err: &client.APIError{StatusCode: 429, Message: "slow down", RetryAfter: 5}}
	withFakeAPI(t, f)
	code, _, errb := run("info")
	if code != 1 {
		t.Fatalf("code=%d want 1", code)
	}
	if !strings.Contains(errb, "429") || !strings.Contains(errb, "retry after 5s") {
		t.Errorf("stderr %q missing 429/retry after 5s", errb)
	}
}

func TestConfigureWritesFile(t *testing.T) {
	clearEnv(t)
	tmp := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("UNIFI_CONFIG", tmp)

	code, out, errb := run("configure", "--host", "h", "--api-key", "k")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "Saved") {
		t.Errorf("stdout %q missing Saved", out)
	}
	got, err := config.Load(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Host != "h" || got.APIKey != "k" {
		t.Errorf("loaded cfg=%+v want host h key k", got)
	}
}

// TestClientSatisfiesAPI ensures the concrete client implements API.
func TestClientSatisfiesAPI(t *testing.T) {
	var _ API = (*client.Client)(nil)
}
