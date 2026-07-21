// Package rpc provides Bitcoin Core JSON-RPC and Insight-API REST clients.
package rpc

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/imroc/req"
	"github.com/tidwall/gjson"
)

const defaultRPCTimeout = 60 * time.Second

// Client is a Bitcoin Core JSON-RPC client.
type Client struct {
	BaseURL        string
	BroadcastURL   string
	WalletURL      string // listunspent and other UTXO queries
	QueryWalletURL string // gettransaction wallet fallback
	AccessToken    string
	http           *req.Req
}

// NewClient creates a JSON-RPC client with HTTP Basic auth.
func NewClient(url, user, password string) *Client {
	token := ""
	if user != "" || password != "" {
		token = base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	}
	r := req.New()
	r.SetTimeout(defaultRPCTimeout)
	return &Client{
		BaseURL:     url,
		BroadcastURL: url,
		AccessToken: token,
		http:        r,
	}
}

// SetWalletURLs configures /wallet/<name> endpoints for multi-wallet bitcoind nodes.
func (c *Client) SetWalletURLs(walletName, queryWalletName string) {
	if c == nil {
		return
	}
	c.WalletURL = WalletEndpoint(c.BaseURL, walletName)
	if strings.TrimSpace(queryWalletName) != "" {
		c.QueryWalletURL = WalletEndpoint(c.BaseURL, queryWalletName)
	} else {
		c.QueryWalletURL = c.WalletURL
	}
}

// WalletEndpoint builds http://host:port/wallet/<name> from base RPC URL.
func WalletEndpoint(baseURL, walletName string) string {
	walletName = strings.TrimSpace(walletName)
	if walletName == "" {
		return strings.TrimRight(strings.TrimSpace(baseURL), "/")
	}
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/wallet/" + walletName
}

// Call invokes JSON-RPC method on the chain endpoint.
func (c *Client) Call(method string, params []interface{}) (*gjson.Result, error) {
	return c.callAt(c.BaseURL, method, normalizeRPCParams(params))
}

// WalletCall invokes JSON-RPC on the configured wallet endpoint (listunspent, etc.).
func (c *Client) WalletCall(method string, params []interface{}) (*gjson.Result, error) {
	if c.WalletURL != "" {
		return c.callAt(c.WalletURL, method, normalizeRPCParams(params))
	}
	return c.Call(method, params)
}

// QueryWalletCall invokes JSON-RPC on the query wallet endpoint (gettransaction fallback).
func (c *Client) QueryWalletCall(method string, params []interface{}) (*gjson.Result, error) {
	url := c.QueryWalletURL
	if url == "" {
		url = c.WalletURL
	}
	if url != "" {
		return c.callAt(url, method, normalizeRPCParams(params))
	}
	return c.Call(method, params)
}

// BroadcastCall invokes JSON-RPC on broadcast URL (defaults to BaseURL).
func (c *Client) BroadcastCall(method string, params []interface{}) (*gjson.Result, error) {
	url := c.BroadcastURL
	if url == "" {
		url = c.BaseURL
	}
	return c.callAt(url, method, normalizeRPCParams(params))
}

// BroadcastWalletCall invokes sendrawtransaction on a loaded wallet endpoint.
// Prefers QueryWalletURL (e.g. /wallet/ops_watch) to avoid -19 Multiple wallets on multi-wallet nodes.
func (c *Client) BroadcastWalletCall(method string, params []interface{}) (*gjson.Result, error) {
	url := c.broadcastWalletURL()
	return c.callAt(url, method, normalizeRPCParams(params))
}

func (c *Client) broadcastWalletURL() string {
	if c == nil {
		return ""
	}
	if c.QueryWalletURL != "" {
		return c.QueryWalletURL
	}
	if c.WalletURL != "" {
		return c.WalletURL
	}
	if c.BroadcastURL != "" {
		return c.BroadcastURL
	}
	return c.BaseURL
}

func normalizeRPCParams(params []interface{}) []interface{} {
	if params == nil {
		return []interface{}{}
	}
	return params
}

func (c *Client) callAt(url, method string, params []interface{}) (*gjson.Result, error) {
	if c.http == nil || url == "" {
		return nil, errors.New("rpc client is not configured")
	}
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  method,
		"params":  params,
	}
	headers := req.Header{"Accept": "application/json"}
	if c.AccessToken != "" {
		headers["Authorization"] = "Basic " + c.AccessToken
	}
	r, err := c.http.Post(url, req.BodyJSON(&body), headers)
	if err != nil {
		return nil, err
	}
	resp := gjson.ParseBytes(r.Bytes())
	if resp.Get("error").IsObject() {
		return nil, fmt.Errorf("[%d] %s",
			resp.Get("error.code").Int(),
			resp.Get("error.message").String())
	}
	if !resp.Get("result").Exists() {
		body := strings.TrimSpace(r.String())
		if len(body) > 256 {
			body = body[:256] + "..."
		}
		if body == "" {
			return nil, fmt.Errorf("rpc response is empty: method=%s url=%s", method, url)
		}
		return nil, fmt.Errorf("rpc response missing result: method=%s url=%s body=%s", method, url, body)
	}
	result := resp.Get("result")
	return &result, nil
}

// Explorer is an Insight-API REST client.
type Explorer struct {
	BaseURL string
	http    *req.Req
}

// NewExplorer creates an Insight-API client.
func NewExplorer(url string) *Explorer {
	e := req.New()
	e.SetTimeout(defaultRPCTimeout)
	return &Explorer{BaseURL: url, http: e}
}

// Call performs HTTP request against Insight-API.
func (e *Explorer) Call(path string, request interface{}, method string) (*gjson.Result, error) {
	if e.http == nil || e.BaseURL == "" {
		return nil, errors.New("explorer client is not configured")
	}
	url := e.BaseURL + path
	r, err := e.http.Do(method, url, request)
	if err != nil {
		return nil, err
	}
	if r.Response() == nil || r.Response().StatusCode != http.StatusOK {
		return nil, fmt.Errorf("explorer request failed: %s", r.String())
	}
	resp := gjson.ParseBytes(r.Bytes())
	return &resp, nil
}
