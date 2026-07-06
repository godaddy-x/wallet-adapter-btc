package config

import (
	"path/filepath"
	"testing"

	adapterconfig "github.com/godaddy-x/wallet-adapter/config"
	"github.com/shopspring/decimal"
)

func TestBuildConfigFromConfigerDefaults(t *testing.T) {
	cfg := BuildConfigFromConfiger(adapterconfig.MapConfig(map[string]string{}), "BTC")
	if cfg.Symbol != "BTC" {
		t.Fatalf("symbol: got %q want BTC", cfg.Symbol)
	}
	if cfg.NetworkName() != NetworkMainnet {
		t.Fatalf("network: got %q want %q", cfg.NetworkName(), NetworkMainnet)
	}
	if cfg.AddressFormat != AddressFormatP2WPKH {
		t.Fatalf("addressFormat: got %q", cfg.AddressFormat)
	}
	if cfg.RPCServerType != RPCServerCore {
		t.Fatalf("rpcServerType: got %d want %d", cfg.RPCServerType, RPCServerCore)
	}
	if !cfg.SupportSegWit {
		t.Fatal("expected supportSegWit default true")
	}
	if !cfg.MinFees.Equal(decimal.Zero) {
		t.Fatalf("minFees: got %s want 0", cfg.MinFees)
	}
}

func TestBuildConfigFromConfigerRegtest(t *testing.T) {
	cfg := BuildConfigFromConfiger(adapterconfig.MapConfig(map[string]string{
		"network": "regtest",
	}), "BTC")
	if cfg.NetworkName() != NetworkRegtest {
		t.Fatalf("network: got %q want %q", cfg.NetworkName(), NetworkRegtest)
	}
	if cfg.Bech32HRP() != "bcrt" {
		t.Fatalf("bech32 prefix: got %q want bcrt", cfg.Bech32HRP())
	}
}

func TestBuildConfigFromConfigerExplicit(t *testing.T) {
	cfg := BuildConfigFromConfiger(adapterconfig.MapConfig(map[string]string{
		"serverAPI":     "http://127.0.0.1:8332",
		"broadcastAPI":  "http://127.0.0.1:8333",
		"rpcUser":       "u",
		"rpcPassword":   "p",
		"rpcServerType": "1",
		"isTestNet":     "true",
		"supportSegWit": "false",
		"minFees":       "0.00001",
		"maxTxInputs":   "100",
		"dataDir":       "testdata",
	}), "BTC")

	if cfg.ServerAPI != "http://127.0.0.1:8332" {
		t.Fatalf("serverAPI: %q", cfg.ServerAPI)
	}
	if cfg.BroadcastAPI != "http://127.0.0.1:8333" {
		t.Fatalf("broadcastAPI: %q", cfg.BroadcastAPI)
	}
	if cfg.RPCServerType != RPCServerExplorer {
		t.Fatalf("rpcServerType: got %d want %d", cfg.RPCServerType, RPCServerExplorer)
	}
	if !cfg.IsTestNet {
		t.Fatal("expected isTestNet true")
	}
	if cfg.SupportSegWit {
		t.Fatal("expected supportSegWit false")
	}
	if cfg.MaxTxInputs != 100 {
		t.Fatalf("maxTxInputs: got %d want 100", cfg.MaxTxInputs)
	}
	wantMinFees := decimal.RequireFromString("0.00001")
	if !cfg.MinFees.Equal(wantMinFees) {
		t.Fatalf("minFees: got %s want %s", cfg.MinFees, wantMinFees)
	}
}

func TestMakeDataDir(t *testing.T) {
	cfg := NewConfig("BTC")
	cfg.DataDir = "data"
	cfg.MakeDataDir()
	if cfg.DataDir != filepath.Join("data", "btc") {
		t.Fatalf("dataDir: got %q want %q", cfg.DataDir, filepath.Join("data", "btc"))
	}
}
