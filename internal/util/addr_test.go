package util

import (
	"encoding/hex"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
)

func TestVoutAddressMatchesScriptP2WPKH(t *testing.T) {
	const addr = "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
	hash, err := btcaddr.DecodeAddressHash160(addr, "mainnet")
	if err != nil {
		t.Fatal(err)
	}
	scriptHex := hex.EncodeToString(append([]byte{0x00, 0x14}, hash...))
	if !VoutAddressMatchesScript(addr, scriptHex, "witness_v0_keyhash", "mainnet") {
		t.Fatal("expected matching p2wpkh script")
	}
	tampered := "001488888888888888888888888888888888888888"
	if VoutAddressMatchesScript(addr, tampered, "witness_v0_keyhash", "mainnet") {
		t.Fatal("expected tampered script to fail verification")
	}
}
