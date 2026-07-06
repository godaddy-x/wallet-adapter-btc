package rpc

import "testing"

func TestWalletEndpoint(t *testing.T) {
	base := "http://127.0.0.1:18443"
	if got := WalletEndpoint(base, ""); got != base {
		t.Fatalf("empty wallet: got %q want %q", got, base)
	}
	if got := WalletEndpoint(base+"/", "btcwatch"); got != base+"/wallet/btcwatch" {
		t.Fatalf("btcwatch: got %q", got)
	}
}

func TestBroadcastWalletURL(t *testing.T) {
	base := "http://127.0.0.1:18443"
	c := NewClient(base, "u", "p")
	c.SetWalletURLs("btcwatch", "ops_watch")
	if got := c.broadcastWalletURL(); got != base+"/wallet/ops_watch" {
		t.Fatalf("query wallet: got %q", got)
	}
	c.QueryWalletURL = ""
	if got := c.broadcastWalletURL(); got != base+"/wallet/btcwatch" {
		t.Fatalf("wallet fallback: got %q", got)
	}
}
