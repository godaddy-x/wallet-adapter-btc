package btc

import (
	"testing"

	btcscanner "github.com/godaddy-x/wallet-adapter-btc/internal/scanner"
)

func TestNewBtcAdapterMetadata(t *testing.T) {
	a := NewBtcAdapter("BTC", "Bitcoin", 8)
	if a.Symbol() != "BTC" {
		t.Fatalf("symbol: %q", a.Symbol())
	}
	if a.FullName() != "Bitcoin" {
		t.Fatalf("fullName: %q", a.FullName())
	}
	if a.Decimal() != 8 {
		t.Fatalf("decimals: %d", a.Decimal())
	}
	if a.GetTransactionDecoder() == nil {
		t.Fatal("transaction decoder is nil")
	}
	if a.GetAddressDecoder() == nil {
		t.Fatal("address decoder is nil")
	}
	if _, ok := a.GetBlockScanner().(*btcscanner.BtcBlockScanner); !ok {
		t.Fatalf("unexpected block scanner type: %T", a.GetBlockScanner())
	}
	if a.GetSmartContractDecoder() != nil {
		t.Fatal("expected nil smart contract decoder for native BTC adapter")
	}
}

func TestNewAdapterFromJSON(t *testing.T) {
	const jsonContent = `{
		"serverAPI": "http://127.0.0.1:18443",
		"broadcastAPI": "http://127.0.0.1:18443",
		"rpcUser": "testuser",
		"rpcPassword": "testpass123",
		"rpcServerType": "0",
		"network": "regtest",
		"isRegtest": "true",
		"supportSegWit": "true",
		"dataDir": "data"
	}`
	a, err := NewAdapter(jsonContent, "BTC", "Bitcoin", 8)
	if err != nil {
		t.Skipf("skip: local BTC regtest node unavailable: %v", err)
	}
	if a.Symbol() != "BTC" {
		t.Fatalf("symbol: %q", a.Symbol())
	}
	if a.Config().ServerAPI != "http://127.0.0.1:18443" {
		t.Fatalf("serverAPI: %q", a.Config().ServerAPI)
	}
	if !a.Config().SupportSegWit {
		t.Fatal("expected supportSegWit true")
	}
}
