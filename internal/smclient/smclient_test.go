package smclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/colindickson/unifi/internal/client"
	"github.com/colindickson/unifi/internal/config"
)

// newTestClient points a Client at the test server (http, base override).
func newTestClient(serverURL string) *Client {
	c := New(config.Config{APIKey: "test-key"})
	c.baseURL = strings.TrimRight(serverURL, "/")
	return c
}

// cursorResponse encodes a {data, nextToken} envelope.
func cursorResponse(nextToken string, items []any) string {
	data, _ := json.Marshal(items)
	env := map[string]any{"data": json.RawMessage(data)}
	if nextToken != "" {
		env["nextToken"] = nextToken
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func TestNew_DefaultBaseURL(t *testing.T) {
	c := New(config.Config{APIKey: "k"})
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
}

func TestNew_BaseURLEnvOverride(t *testing.T) {
	t.Setenv("UNIFI_SITE_MANAGER_URL", "https://example.test/")
	c := New(config.Config{APIKey: "k"})
	if c.baseURL != "https://example.test" {
		t.Errorf("baseURL = %q, want trailing slash trimmed override", c.baseURL)
	}
}

func TestDo_SendsAPIKeyAndAccept(t *testing.T) {
	var gotKey, gotAccept, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-KEY")
		gotAccept = r.Header.Get("Accept")
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"data":{"ok":true}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Do(context.Background(), http.MethodGet, "/v1/hosts", nil, nil)
	if err != nil {
		t.Fatalf("Do error: %v", err)
	}
	if gotKey != "test-key" {
		t.Errorf("X-API-KEY = %q", gotKey)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotPath != "/v1/hosts" {
		t.Errorf("path = %q", gotPath)
	}
	// Do is the raw passthrough: it must NOT unwrap data.
	if !strings.Contains(string(raw), "data") {
		t.Errorf("Do should return the raw body incl. envelope, got %s", raw)
	}
}

func TestListPath_FollowsNextTokenWhenAll(t *testing.T) {
	var gotTokens []string
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTokens = append(gotTokens, r.URL.Query().Get("nextToken"))
		call++
		switch call {
		case 1:
			fmt.Fprint(w, cursorResponse("tok2", []any{map[string]string{"id": "a"}, map[string]string{"id": "b"}}))
		case 2:
			fmt.Fprint(w, cursorResponse("", []any{map[string]string{"id": "c"}}))
		default:
			t.Errorf("unexpected extra request (call %d)", call)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.ListPath(context.Background(), "/v1/hosts", true, 0)
	if err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("got %d items, want 3", len(items))
	}
	if call != 2 {
		t.Errorf("expected 2 page calls, got %d", call)
	}
	if gotTokens[0] != "" || gotTokens[1] != "tok2" {
		t.Errorf("nextToken sequence = %v, want ['', 'tok2']", gotTokens)
	}
}

func TestListPath_SinglePageWhenNotAll(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		// Always advertise a next token; not-all must still stop after one page.
		fmt.Fprint(w, cursorResponse("more", []any{map[string]string{"id": "a"}}))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.ListPath(context.Background(), "/v1/hosts", false, 25)
	if err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("got %d items, want 1", len(items))
	}
	if call != 1 {
		t.Errorf("not-all must fetch exactly one page, got %d calls", call)
	}
}

func TestListPath_SendsPageSize(t *testing.T) {
	var gotPageSize string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPageSize = r.URL.Query().Get("pageSize")
		fmt.Fprint(w, cursorResponse("", []any{}))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.ListPath(context.Background(), "/v1/hosts", false, 50); err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if gotPageSize != "50" {
		t.Errorf("pageSize = %q, want 50", gotPageSize)
	}
}

func TestListPath_StopsOnEmptyPage(t *testing.T) {
	call := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call++
		switch call {
		case 1:
			fmt.Fprint(w, cursorResponse("tok", []any{map[string]string{"id": "a"}}))
		case 2:
			// Empty data but still a token — guard against an infinite loop.
			fmt.Fprint(w, cursorResponse("tok", []any{}))
		default:
			t.Errorf("unexpected extra request (call %d)", call)
		}
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	items, err := c.ListPath(context.Background(), "/v1/hosts", true, 0)
	if err != nil {
		t.Fatalf("ListPath error: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("got %d items, want 1", len(items))
	}
	if call != 2 {
		t.Errorf("expected 2 calls then stop, got %d", call)
	}
}

func TestDo_429_ReturnsClientAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"code":"RATE_LIMIT","message":"slow down"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Do(context.Background(), http.MethodGet, "/ea/sd-wan-configs", nil, nil)
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("error type = %T, want *client.APIError", err)
	}
	if apiErr.StatusCode != 429 || apiErr.RetryAfter != 7 {
		t.Errorf("apiErr = %+v, want 429/retry 7", apiErr)
	}
}

func TestDo_ErrorFallbackToRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		fmt.Fprint(w, "service unavailable")
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Do(context.Background(), http.MethodGet, "/v1/hosts", nil, nil)
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("error type = %T", err)
	}
	if apiErr.StatusCode != 503 || !strings.Contains(apiErr.Message, "service unavailable") {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestHost_UnwrapsDataAndEscapesID(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		fmt.Fprint(w, `{"data":{"id":"h1","name":"Console"}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	raw, err := c.Host(context.Background(), "h 1")
	if err != nil {
		t.Fatalf("Host error: %v", err)
	}
	if gotPath != "/v1/hosts/h%201" {
		t.Errorf("path = %q, want /v1/hosts/h%%201", gotPath)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["id"] != "h1" {
		t.Errorf("Host should unwrap data, got %s", raw)
	}
}

func TestHosts_Sites_UseCorrectPaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(c *Client) ([]json.RawMessage, error)
		want string
	}{
		{"hosts", func(c *Client) ([]json.RawMessage, error) { return c.Hosts(context.Background(), false, 25) }, "/v1/hosts"},
		{"sites", func(c *Client) ([]json.RawMessage, error) { return c.Sites(context.Background(), false, 25) }, "/v1/sites"},
		{"sdwan", func(c *Client) ([]json.RawMessage, error) { return c.SDWANConfigs(context.Background(), false, 25) }, "/ea/sd-wan-configs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				fmt.Fprint(w, cursorResponse("", []any{map[string]string{"id": "x"}}))
			}))
			defer srv.Close()
			c := newTestClient(srv.URL)
			items, err := tc.call(c)
			if err != nil {
				t.Fatalf("%s error: %v", tc.name, err)
			}
			if gotPath != tc.want {
				t.Errorf("%s path = %q, want %q", tc.name, gotPath, tc.want)
			}
			if len(items) != 1 {
				t.Errorf("%s got %d items, want 1", tc.name, len(items))
			}
		})
	}
}

func TestDevices_SendsHostIDsFilter(t *testing.T) {
	var gotPath string
	var gotHostIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHostIDs = r.URL.Query()["hostIds"]
		fmt.Fprint(w, cursorResponse("", []any{}))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.Devices(context.Background(), []string{"h1", "h2"}, false, 25); err != nil {
		t.Fatalf("Devices error: %v", err)
	}
	if gotPath != "/v1/devices" {
		t.Errorf("path = %q, want /v1/devices", gotPath)
	}
	if len(gotHostIDs) != 2 || gotHostIDs[0] != "h1" || gotHostIDs[1] != "h2" {
		t.Errorf("hostIds = %v, want [h1 h2]", gotHostIDs)
	}

	// nil filter: no hostIds param should be emitted at all.
	var nilHostIDs []string
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nilHostIDs = r.URL.Query()["hostIds"]
		fmt.Fprint(w, cursorResponse("", []any{}))
	}))
	defer srv2.Close()
	c2 := newTestClient(srv2.URL)
	if _, err := c2.Devices(context.Background(), nil, false, 25); err != nil {
		t.Fatalf("Devices nil-filter error: %v", err)
	}
	if len(nilHostIDs) != 0 {
		t.Errorf("nil filter should emit no hostIds param, got %v", nilHostIDs)
	}
}

func TestISPMetrics_GETWithTypeAndQuery(t *testing.T) {
	var gotPath, gotDuration string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotDuration = r.URL.Query().Get("duration")
		fmt.Fprint(w, `{"data":{"metrics":[]}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	q := url.Values{}
	q.Set("duration", "24h")
	raw, err := c.ISPMetrics(context.Background(), "5m", q)
	if err != nil {
		t.Fatalf("ISPMetrics error: %v", err)
	}
	if gotPath != "/ea/isp-metrics/5m" {
		t.Errorf("path = %q, want /ea/isp-metrics/5m", gotPath)
	}
	if gotDuration != "24h" {
		t.Errorf("duration = %q, want 24h", gotDuration)
	}
	if !strings.Contains(string(raw), "metrics") {
		t.Errorf("ISPMetrics should unwrap data, got %s", raw)
	}
}

func TestQueryISPMetrics_POSTBody(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"data":{"ok":true}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.QueryISPMetrics(context.Background(), "1h", json.RawMessage(`{"sites":["s1"]}`))
	if err != nil {
		t.Fatalf("QueryISPMetrics error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/ea/isp-metrics/1h/query" {
		t.Errorf("path = %q, want /ea/isp-metrics/1h/query", gotPath)
	}
	if !strings.Contains(string(gotBody), "s1") {
		t.Errorf("body = %s, want sites filter", gotBody)
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
}

func TestSDWANConfigAndStatus_Paths(t *testing.T) {
	for _, tc := range []struct {
		name    string
		call    func(c *Client) (json.RawMessage, error)
		want    string
		escaped bool
	}{
		{"config", func(c *Client) (json.RawMessage, error) { return c.SDWANConfig(context.Background(), "cfg1") }, "/ea/sd-wan-configs/cfg1", false},
		{"status", func(c *Client) (json.RawMessage, error) { return c.SDWANStatus(context.Background(), "cfg1") }, "/ea/sd-wan-configs/cfg1/status", false},
		{"config-escaped", func(c *Client) (json.RawMessage, error) { return c.SDWANConfig(context.Background(), "cfg 1") }, "/ea/sd-wan-configs/cfg%201", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.escaped {
					gotPath = r.URL.EscapedPath()
				} else {
					gotPath = r.URL.Path
				}
				fmt.Fprint(w, `{"data":{"id":"cfg1"}}`)
			}))
			defer srv.Close()
			c := newTestClient(srv.URL)
			raw, err := tc.call(c)
			if err != nil {
				t.Fatalf("%s error: %v", tc.name, err)
			}
			if gotPath != tc.want {
				t.Errorf("%s path = %q, want %q", tc.name, gotPath, tc.want)
			}
			var m map[string]string
			if err := json.Unmarshal(raw, &m); err != nil || m["id"] != "cfg1" {
				t.Errorf("%s should unwrap data, got %s", tc.name, raw)
			}
		})
	}
}

func TestMethods_EmptyArgsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP call for empty arg: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()
	c := newTestClient(srv.URL)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"Host", func() error { _, err := c.Host(ctx, ""); return err }},
		{"SDWANConfig", func() error { _, err := c.SDWANConfig(ctx, ""); return err }},
		{"SDWANStatus", func() error { _, err := c.SDWANStatus(ctx, ""); return err }},
		{"ISPMetrics", func() error { _, err := c.ISPMetrics(ctx, "", nil); return err }},
		{"QueryISPMetrics", func() error {
			_, err := c.QueryISPMetrics(ctx, "", json.RawMessage(`{"sites":["s1"]}`))
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Errorf("%s with empty arg should return an error", tc.name)
			}
		})
	}
}

func TestDo_StatusNameErrorBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"statusName":"UNAUTHORIZED","message":"bad key"}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Do(context.Background(), http.MethodGet, "/v1/hosts", nil, nil)
	apiErr, ok := err.(*client.APIError)
	if !ok {
		t.Fatalf("error type = %T, want *client.APIError", err)
	}
	if apiErr.StatusCode != 401 || apiErr.Code != "UNAUTHORIZED" || apiErr.Message != "bad key" {
		t.Errorf("apiErr = %+v, want 401/UNAUTHORIZED/bad key", apiErr)
	}
}

func TestQueryISPMetrics_NilBodySendsEmpty(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, `{"data":{"ok":true}}`)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.QueryISPMetrics(context.Background(), "1h", nil); err != nil {
		t.Fatalf("QueryISPMetrics error: %v", err)
	}
	if len(gotBody) != 0 {
		t.Errorf("nil body should send empty request body, got %q", gotBody)
	}
}
