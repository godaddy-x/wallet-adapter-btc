package models

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/tidwall/gjson"
)

func TestParseVoutFromCoreUsesSingularAddress(t *testing.T) {
	parser := NewBlockParser(config.NewConfig("BTC"))
	raw := `{
		"value": 0.5,
		"n": 2,
		"scriptPubKey": {
			"type": "witness_v0_keyhash",
			"address": "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
		}
	}`
	j := gjson.Parse(raw)
	vout := parser.ParseVoutFromCore(&j)
	if vout.N != 2 {
		t.Fatalf("vout.N = %d, want 2", vout.N)
	}
	if vout.Addr != "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4" {
		t.Fatalf("vout.Addr = %q", vout.Addr)
	}
}

func TestNewTxVinByCoreUsesPrevout(t *testing.T) {
	parser := NewBlockParser(config.NewConfig("BTC"))
	raw := `{
		"txid": "prevtx",
		"vout": 1,
		"prevout": {
			"value": 0.25,
			"scriptPubKey": {
				"address": "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",
				"type": "witness_v0_keyhash"
			}
		}
	}`
	j := gjson.Parse(raw)
	vin := parser.newTxVinByCore(&j)
	if vin.Addr != "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4" {
		t.Fatalf("vin.Addr = %q", vin.Addr)
	}
	if vin.Value != "0.25" {
		t.Fatalf("vin.Value = %q", vin.Value)
	}
}
