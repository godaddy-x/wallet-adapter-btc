package decoder

import (
	"encoding/hex"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
)

func TestPubKeyForWitnessProgramCompressed(t *testing.T) {
	compressed, err := hex.DecodeString("0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798")
	if err != nil {
		t.Fatal(err)
	}
	pk, err := btcec.ParsePubKey(compressed)
	if err != nil {
		t.Fatal(err)
	}
	uncompressed := pk.SerializeUncompressed()
	addr, err := btcaddr.PubKeyToAddress(uncompressed, config.NetworkRegtest, false)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := btcaddr.PubKeyForWitnessProgram(addr, hex.EncodeToString(uncompressed), config.NetworkRegtest)
	if err != nil {
		t.Fatalf("witness pubkey: %v", err)
	}
	if len(pub) != 33 {
		t.Fatalf("want compressed pubkey for standard P2WPKH, got %d", len(pub))
	}
}

func TestTxIDFromSignedHexInvalidInput(t *testing.T) {
	if _, err := txIDFromSignedHex(""); err == nil {
		t.Fatal("expected error for empty hex")
	}
	if _, err := txIDFromSignedHex("abcd"); err == nil {
		t.Fatal("expected error for invalid tx")
	}
}
