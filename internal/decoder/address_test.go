package decoder

import (
	"encoding/hex"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
)

func testDecoder(t *testing.T, cfg *config.WalletConfig) *BtcAddressDecoder {
	t.Helper()
	return NewAddressDecoder(&manager.WalletManager{Config: cfg})
}

func testPubKey(t *testing.T) []byte {
	t.Helper()
	pubHex := "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
	pub, err := hex.DecodeString(pubHex)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestAddressVerifyKnownMainnet(t *testing.T) {
	dec := testDecoder(t, config.NewConfig("BTC"))
	if !dec.AddressVerify("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa") {
		t.Fatal("legacy mainnet address should verify")
	}
	if !dec.AddressVerify("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4") {
		t.Fatal("native segwit mainnet address should verify")
	}
}

func TestPublicKeyToAddressMainnetNativeSegWit(t *testing.T) {
	dec := testDecoder(t, config.NewConfig("BTC"))
	addr, err := dec.PublicKeyToAddress(testPubKey(t), false)
	if err != nil {
		t.Fatal(err)
	}
	const want = "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
	if addr != want {
		t.Fatalf("address: got %q want %q", addr, want)
	}
	if !dec.AddressVerify(addr) {
		t.Fatalf("generated address invalid: %q", addr)
	}
}

func TestPublicKeyToAddressMainnetLegacyP2PKH(t *testing.T) {
	cfg := config.NewConfig("BTC")
	cfg.AddressFormat = config.AddressFormatP2PKH
	dec := testDecoder(t, cfg)

	addr, err := dec.PublicKeyToAddress(testPubKey(t), false)
	if err != nil {
		t.Fatal(err)
	}
	if addr != "1BgGZ9tcN4rm9KBzDn7KprQz87SZ26SAMH" {
		t.Fatalf("legacy address: got %q", addr)
	}
}

func TestPublicKeyToAddressTestnetNativeSegWit(t *testing.T) {
	cfg := config.NewConfig("BTC")
	cfg.Network = config.NetworkTestnet
	dec := testDecoder(t, cfg)
	addr, err := dec.PublicKeyToAddress(testPubKey(t), true)
	if err != nil {
		t.Fatal(err)
	}
	const want = "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx"
	if addr != want {
		t.Fatalf("address: got %q want %q", addr, want)
	}
}

func TestPublicKeyToAddressIgnoresIsTestnetWhenConfigIsMainnet(t *testing.T) {
	dec := testDecoder(t, config.NewConfig("BTC"))
	addr, err := dec.PublicKeyToAddress(testPubKey(t), true)
	if err != nil {
		t.Fatal(err)
	}
	const want = "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
	if addr != want {
		t.Fatalf("isTestnet must not override config network: got %q want %q", addr, want)
	}
}

func TestPublicKeyToAddressRegtestNativeSegWit(t *testing.T) {
	cfg := config.NewConfig("BTC")
	cfg.Network = config.NetworkRegtest
	dec := testDecoder(t, cfg)

	addr, err := dec.PublicKeyToAddress(testPubKey(t), false)
	if err != nil {
		t.Fatal(err)
	}
	const want = "bcrt1qw508d6qejxtdg4y5r3zarvary0c5xw7kygt080"
	if addr != want {
		t.Fatalf("address: got %q want %q", addr, want)
	}
	if !dec.AddressVerify(addr) {
		t.Fatalf("regtest address invalid: %q", addr)
	}
}

func TestAddressDecodeEncodeRoundTripLegacy(t *testing.T) {
	cfg := config.NewConfig("BTC")
	cfg.AddressFormat = config.AddressFormatP2PKH
	dec := testDecoder(t, cfg)

	addr := "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	hash, err := dec.AddressDecode(addr)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := dec.AddressEncode(hash)
	if err != nil {
		t.Fatal(err)
	}
	if encoded != addr {
		t.Fatalf("round trip: got %q want %q", encoded, addr)
	}
}

func TestAddressEncodeUsesConfiguredNetwork(t *testing.T) {
	cfg := config.NewConfig("BTC")
	cfg.Network = config.NetworkTestnet
	dec := testDecoder(t, cfg)

	hash, err := dec.AddressDecode("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4")
	if err != nil {
		t.Fatal(err)
	}
	testAddr, err := dec.AddressEncode(hash)
	if err != nil {
		t.Fatal(err)
	}
	if testAddr != "tb1qw508d6qejxtdg4y5r3zarvary0c5xw7kxpjzsx" {
		t.Fatalf("testnet encode: got %q", testAddr)
	}
}

func TestAddressVerifyRegtestOnMainnetConfig(t *testing.T) {
	dec := testDecoder(t, config.NewConfig("BTC"))
	addr := "bcrt1qw508d6qejxtdg4y5r3zarvary0c5xw7kygt080"
	if !dec.AddressVerify(addr) {
		t.Fatalf("expected regtest address valid via prefix inference: %q", addr)
	}
}

func TestAddressVerifyInvalid(t *testing.T) {
	dec := testDecoder(t, config.NewConfig("BTC"))
	if dec.AddressVerify("not-a-btc-address") {
		t.Fatal("expected invalid address")
	}
}

func TestConfigBech32HRPRegtest(t *testing.T) {
	cfg := config.NewConfig("BTC")
	cfg.Network = config.NetworkRegtest
	if cfg.Bech32HRP() != "bcrt" {
		t.Fatalf("bech32 prefix: got %q want bcrt", cfg.Bech32HRP())
	}
}
