package smclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// requireArg returns an error when a required path argument is empty. Without it
// an empty id/type would build a path to the collection endpoint (e.g.
// "/v1/hosts/") and silently return the wrong shape instead of failing clearly.
func requireArg(kind, v string) error {
	if v == "" {
		return fmt.Errorf("smclient: %s is required", kind)
	}
	return nil
}

// Hosts lists all UniFi OS hosts (consoles) on the account.
func (c *Client) Hosts(ctx context.Context, all bool, limit int) ([]json.RawMessage, error) {
	return c.listCursor(ctx, "/v1/hosts", nil, all, limit)
}

// Host fetches a single host by id.
func (c *Client) Host(ctx context.Context, id string) (json.RawMessage, error) {
	if err := requireArg("host id", id); err != nil {
		return nil, err
	}
	b, err := c.do(ctx, http.MethodGet, "/v1/hosts/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}
	return unwrapData(b), nil
}

// Sites lists all sites across the account's hosts.
func (c *Client) Sites(ctx context.Context, all bool, limit int) ([]json.RawMessage, error) {
	return c.listCursor(ctx, "/v1/sites", nil, all, limit)
}

// Devices lists devices across hosts, optionally filtered to the given host ids.
func (c *Client) Devices(ctx context.Context, hostIDs []string, all bool, limit int) ([]json.RawMessage, error) {
	var base url.Values
	if len(hostIDs) > 0 {
		base = url.Values{}
		for _, id := range hostIDs {
			base.Add("hostIds", id)
		}
	}
	return c.listCursor(ctx, "/v1/devices", base, all, limit)
}

// ISPMetrics fetches ISP performance metrics for the account. mtype is "5m" or
// "1h"; q carries duration or beginTimestamp/endTimestamp.
func (c *Client) ISPMetrics(ctx context.Context, mtype string, q url.Values) (json.RawMessage, error) {
	if err := requireArg("metric type", mtype); err != nil {
		return nil, err
	}
	b, err := c.do(ctx, http.MethodGet, "/ea/isp-metrics/"+url.PathEscape(mtype), q, nil)
	if err != nil {
		return nil, err
	}
	return unwrapData(b), nil
}

// QueryISPMetrics runs a filtered ISP-metrics query. body is the raw JSON filter.
func (c *Client) QueryISPMetrics(ctx context.Context, mtype string, body json.RawMessage) (json.RawMessage, error) {
	if err := requireArg("metric type", mtype); err != nil {
		return nil, err
	}
	var reqBody any
	if len(body) > 0 {
		reqBody = body
	}
	b, err := c.do(ctx, http.MethodPost, "/ea/isp-metrics/"+url.PathEscape(mtype)+"/query", nil, reqBody)
	if err != nil {
		return nil, err
	}
	return unwrapData(b), nil
}

// SDWANConfigs lists SD-WAN configurations.
func (c *Client) SDWANConfigs(ctx context.Context, all bool, limit int) ([]json.RawMessage, error) {
	return c.listCursor(ctx, "/ea/sd-wan-configs", nil, all, limit)
}

// SDWANConfig fetches a single SD-WAN configuration by id.
func (c *Client) SDWANConfig(ctx context.Context, id string) (json.RawMessage, error) {
	if err := requireArg("config id", id); err != nil {
		return nil, err
	}
	b, err := c.do(ctx, http.MethodGet, "/ea/sd-wan-configs/"+url.PathEscape(id), nil, nil)
	if err != nil {
		return nil, err
	}
	return unwrapData(b), nil
}

// SDWANStatus fetches the deployment status of an SD-WAN configuration.
func (c *Client) SDWANStatus(ctx context.Context, id string) (json.RawMessage, error) {
	if err := requireArg("config id", id); err != nil {
		return nil, err
	}
	b, err := c.do(ctx, http.MethodGet, "/ea/sd-wan-configs/"+url.PathEscape(id)+"/status", nil, nil)
	if err != nil {
		return nil, err
	}
	return unwrapData(b), nil
}
