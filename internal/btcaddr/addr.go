package btcaddr

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/txscript"
	"github.com/godaddy-x/wallet-adapter-btc/internal/chainparams"
)

// Hash160 returns RIPEMD160(SHA256(data)).
func Hash160(data []byte) []byte {
	return btcutil.Hash160(data)
}

// CompressPubKey normalizes secp256k1 pubkeys to 33-byte compressed form.
func CompressPubKey(pub []byte) ([]byte, error) {
	if len(pub) == 33 {
		return pub, nil
	}
	if len(pub) == 65 && pub[0] == 0x04 {
		pk, err := btcec.ParsePubKey(pub)
		if err != nil {
			return nil, err
		}
		return pk.SerializeCompressed(), nil
	}
	return nil, fmt.Errorf("invalid pubkey length %d", len(pub))
}

// ParamsForNetwork resolves btcd params from config network name.
func ParamsForNetwork(network string) *chaincfg.Params {
	return chainparams.Params(network)
}

func inferNetworkFromAddress(addr string) (string, bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", false
	}
	lower := strings.ToLower(addr)
	switch {
	case strings.HasPrefix(lower, "bcrt1"):
		return chainparams.NetworkRegtest, true
	case strings.HasPrefix(lower, "tb1"):
		return chainparams.NetworkTestnet, true
	case strings.HasPrefix(lower, "bc1"):
		return chainparams.NetworkMainnet, true
	case strings.HasPrefix(addr, "m"), strings.HasPrefix(addr, "n"), strings.HasPrefix(addr, "2"):
		return chainparams.NetworkTestnet, true
	case strings.HasPrefix(addr, "1"), strings.HasPrefix(addr, "3"):
		return chainparams.NetworkMainnet, true
	default:
		return "", false
	}
}

// EncodeWitnessV0Address encodes hash160 as native segwit v0 address.
func EncodeWitnessV0Address(hash []byte, network string) (string, error) {
	params := ParamsForNetwork(network)
	addr, err := btcutil.NewAddressWitnessPubKeyHash(hash, params)
	if err != nil {
		return "", err
	}
	return addr.EncodeAddress(), nil
}

// EncodePubKeyHashAddress encodes hash160 as legacy P2PKH address.
func EncodePubKeyHashAddress(hash []byte, network string) (string, error) {
	params := ParamsForNetwork(network)
	addr, err := btcutil.NewAddressPubKeyHash(hash, params)
	if err != nil {
		return "", err
	}
	return addr.EncodeAddress(), nil
}

// DecodeAddressHash160 decodes any supported address to hash160 bytes.
func DecodeAddressHash160(addr string, network string) ([]byte, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, fmt.Errorf("empty address")
	}
	if inferred, ok := inferNetworkFromAddress(addr); ok {
		network = inferred
	}
	params := ParamsForNetwork(network)
	decoded, err := btcutil.DecodeAddress(addr, params)
	if err != nil {
		return nil, err
	}
	switch a := decoded.(type) {
	case *btcutil.AddressWitnessPubKeyHash:
		return a.ScriptAddress(), nil
	case *btcutil.AddressPubKeyHash:
		return a.ScriptAddress(), nil
	case *btcutil.AddressScriptHash:
		return a.ScriptAddress(), nil
	default:
		return nil, fmt.Errorf("unsupported address type %T", decoded)
	}
}

// VerifyAddress checks address format for the given network (with prefix inference).
func VerifyAddress(addr, network string) bool {
	if _, err := DecodeAddressHash160(addr, network); err == nil {
		return true
	}
	if inferred, ok := inferNetworkFromAddress(addr); ok {
		_, err := DecodeAddressHash160(addr, inferred)
		return err == nil
	}
	return false
}

// PubKeyToAddress maps MPC pubkey bytes to on-chain address (default P2WPKH).
func PubKeyToAddress(pub []byte, network string, legacyP2PKH bool) (string, error) {
	pub, err := CompressPubKey(pub)
	if err != nil {
		return "", err
	}
	hash := Hash160(pub)
	if legacyP2PKH {
		return EncodePubKeyHashAddress(hash, network)
	}
	return EncodeWitnessV0Address(hash, network)
}

// PubKeyForWitnessProgram returns pubkey bytes whose HASH160 matches the address script.
// Native SegWit (bc1/tb1/bcrt1) always uses compressed secp256k1 pubkeys per BIP141.
func PubKeyForWitnessProgram(address, pubHex, network string) ([]byte, error) {
	wantHash, err := DecodeAddressHash160(address, network)
	if err != nil {
		return nil, err
	}
	pub, err := hex.DecodeString(strings.TrimSpace(pubHex))
	if err != nil {
		return nil, fmt.Errorf("decode pubkey: %w", err)
	}
	segwitV0 := isWitnessV0Address(address)
	candidates := make([][]byte, 0, 2)
	if len(pub) == 33 {
		candidates = append(candidates, pub)
	}
	if len(pub) == 65 && pub[0] == 0x04 {
		if compressed, err := CompressPubKey(pub); err == nil {
			candidates = append(candidates, compressed)
		}
		if !segwitV0 {
			candidates = append(candidates, pub)
		}
	}
	for _, candidate := range candidates {
		got := Hash160(candidate)
		if len(got) == len(wantHash) && string(got) == string(wantHash) {
			if segwitV0 {
				compressed, err := CompressPubKey(candidate)
				if err != nil {
					return nil, err
				}
				return compressed, nil
			}
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("pubkey does not match address witness program")
}

func isWitnessV0Address(addr string) bool {
	lower := strings.ToLower(strings.TrimSpace(addr))
	return strings.HasPrefix(lower, "bc1") ||
		strings.HasPrefix(lower, "tb1") ||
		strings.HasPrefix(lower, "bcrt1")
}

// PayToAddressScript returns pkScript for a decoded address string.
func PayToAddressScript(address, network string) ([]byte, error) {
	hash, err := DecodeAddressHash160(address, network)
	if err != nil {
		return nil, err
	}
	addr := strings.TrimSpace(address)
	lower := strings.ToLower(addr)
	switch {
	case strings.HasPrefix(lower, "bc1"), strings.HasPrefix(lower, "tb1"), strings.HasPrefix(lower, "bcrt1"):
		return append([]byte{txscript.OP_0, 0x14}, hash...), nil
	case strings.HasPrefix(addr, "3"), strings.HasPrefix(addr, "2"):
		return txscript.NewScriptBuilder().
			AddOp(txscript.OP_HASH160).
			AddData(hash).
			AddOp(txscript.OP_EQUAL).
			Script()
	default:
		return txscript.NewScriptBuilder().
			AddOp(txscript.OP_DUP).
			AddOp(txscript.OP_HASH160).
			AddData(hash).
			AddOp(txscript.OP_EQUALVERIFY).
			AddOp(txscript.OP_CHECKSIG).
			Script()
	}
}
