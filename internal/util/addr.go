package util

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/btcsuite/btcd/txscript"
	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
)

// ScriptPubKeyToBech32Address decodes witness v0 script to bech32 address.
func ScriptPubKeyToBech32Address(scriptPubKey []byte, network string) (string, error) {
	if len(scriptPubKey) != 22 || scriptPubKey[0] != txscript.OP_0 || scriptPubKey[1] != 0x14 {
		return "", fmt.Errorf("scriptPubKey length is invalid")
	}
	hash := scriptPubKey[2:]
	return btcaddr.EncodeWitnessV0Address(hash, network)
}

// VoutAddressMatchesScript checks that decoded address matches scriptPubKey (anti-tamper when both are present).
func VoutAddressMatchesScript(addr, scriptHex, scriptType, network string) bool {
	if addr == "" || scriptHex == "" {
		return true
	}
	scriptBytes, err := hex.DecodeString(scriptHex)
	if err != nil {
		return false
	}
	addrHash, err := btcaddr.DecodeAddressHash160(addr, network)
	if err != nil {
		return false
	}
	switch scriptType {
	case "witness_v0_keyhash":
		return matchWitnessV0KeyHash(scriptBytes, addrHash)
	case "pubkeyhash":
		return matchP2PKH(scriptBytes, addrHash)
	case "scripthash":
		return matchP2SH(scriptBytes, addrHash)
	default:
		return true
	}
}

func matchWitnessV0KeyHash(script, hash []byte) bool {
	return len(script) == 22 && script[0] == txscript.OP_0 && script[1] == 0x14 && bytes.Equal(script[2:], hash)
}

func matchP2PKH(script, hash []byte) bool {
	return len(script) == 25 &&
		script[0] == txscript.OP_DUP &&
		script[1] == txscript.OP_HASH160 &&
		script[2] == txscript.OP_DATA_20 &&
		bytes.Equal(script[3:23], hash) &&
		script[23] == txscript.OP_EQUALVERIFY &&
		script[24] == txscript.OP_CHECKSIG
}

func matchP2SH(script, hash []byte) bool {
	return len(script) == 23 &&
		script[0] == txscript.OP_HASH160 &&
		script[1] == txscript.OP_DATA_20 &&
		bytes.Equal(script[2:22], hash) &&
		script[22] == txscript.OP_EQUAL
}
