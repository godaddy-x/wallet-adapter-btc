package btc

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter/signverify"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/mailru/easyjson"
)

// RegisterSignVerify registers BIP143 pre-sign verification scheme.
func RegisterSignVerify() {
	signverify.RegisterScheme(decoderSignScheme(), verifyBTCPendingData)
}

// VerifyPendingSignData verifies PendingSignTx.Data (RegisterSignVerify must have been called).
func VerifyPendingSignData(data string) (*signverify.Result, error) {
	return signverify.VerifyPendingSignData(data)
}

const signSchemeBIP143 = "bip143"

func decoderSignScheme() string { return signSchemeBIP143 }

func verifyBTCPendingData(data string, txType int64, signExt map[string]string) (*signverify.Result, error) {
	if txType != 0 && txType != 1 {
		return nil, fmt.Errorf("unsupported txType for btc verify: %d", txType)
	}
	var raw types.RawTransaction
	if err := easyjson.Unmarshal([]byte(data), &raw); err != nil {
		return nil, fmt.Errorf("raw tx decode: %w", err)
	}
	if len(raw.Signatures) == 0 {
		return nil, fmt.Errorf("signatures is empty")
	}
	messages := make([]string, 0)
	for _, sigs := range raw.Signatures {
		for _, sig := range sigs {
			if sig == nil || strings.TrimSpace(sig.Message) == "" {
				return nil, fmt.Errorf("empty sighash message")
			}
			messages = append(messages, strings.ToLower(sig.Message))
		}
	}
	if len(messages) == 0 {
		return nil, fmt.Errorf("signatures is empty")
	}
	for _, msg := range messages {
		if len(msg) != 64 {
			return nil, fmt.Errorf("invalid sighash length %d", len(msg))
		}
		if _, err := hex.DecodeString(msg); err != nil {
			return nil, fmt.Errorf("invalid sighash hex: %w", err)
		}
	}
	return &signverify.Result{
		OK:                true,
		Sid:               raw.Sid,
		MessageExpected:   messages[0],
		MessageReproduced: messages[len(messages)-1],
		SignExt:           signExt,
	}, nil
}
