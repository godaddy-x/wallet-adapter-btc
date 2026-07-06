package btc

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/mailru/easyjson"
)

func TestVerifyPendingSignDataBIP143(t *testing.T) {
	RegisterSignVerify()

	signExt, err := types.BuildSignExtJSON(map[string]string{
		types.SignExtKeySignScheme:       signSchemeBIP143,
		types.SignExtKeyUnsignedEncoding: "hex",
		types.SignExtKeyHashAlgorithm:    "double_sha256",
		"network":                        "mainnet",
		"segwit":                         "true",
	})
	if err != nil {
		t.Fatal(err)
	}

	const msg = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	raw := types.RawTransaction{
		TxType:  0,
		Sid:     "sid-btc-test",
		SignExt: signExt,
		Coin:    types.Coin{Symbol: "BTC"},
		Signatures: map[string][]*types.KeySignature{
			"acc1": {{Message: msg}},
		},
	}
	data, err := easyjson.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}

	res, err := VerifyPendingSignData(string(data))
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if !res.OK {
		t.Fatalf("expected OK=true: %+v", res)
	}
	if res.Sid != "sid-btc-test" {
		t.Fatalf("sid: got %q", res.Sid)
	}
	if res.MessageExpected != msg || res.MessageReproduced != msg {
		t.Fatalf("message mismatch: expected=%q reproduced=%q", res.MessageExpected, res.MessageReproduced)
	}
}

func TestVerifyPendingSignDataEmptySignatures(t *testing.T) {
	RegisterSignVerify()

	signExt, err := types.BuildSignExtJSON(map[string]string{
		types.SignExtKeySignScheme: signSchemeBIP143,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := types.RawTransaction{
		TxType:  0,
		Sid:     "sid-empty",
		SignExt: signExt,
		Coin:    types.Coin{Symbol: "BTC"},
	}
	data, err := easyjson.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPendingSignData(string(data)); err == nil {
		t.Fatal("expected error when signatures are empty")
	}
}
