package decoder

import (
	"encoding/json"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

func TestWritePayerSendOutExtParamDustPlusMain(t *testing.T) {
	raw := &types.RawTransaction{
		To: map[string]string{
			"bcrt1qexternal": "0.003",
		},
	}
	used := []*models.Unspent{
		{Address: "bcrt1qdust", Amount: "0.003"},
		{Address: "bcrt1qmain", Amount: "49.96694326"},
	}
	writePayerSendOutExtParam(raw, used, map[string]decimal.Decimal{
		"bcrt1qexternal": decimal.RequireFromString("0.003"),
	})
	got := raw.ExtParam[extparam.KeyPayerSendOut]
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("unmarshal: %v raw=%s", err, got)
	}
	if parsed["bcrt1qdust"] != "0.003" {
		t.Fatalf("dust sendOut=%q", parsed["bcrt1qdust"])
	}
	if parsed["bcrt1qmain"] != "0" {
		t.Fatalf("main sendOut=%q", parsed["bcrt1qmain"])
	}
}

func TestWritePayerSendOutExtParamSinglePayer(t *testing.T) {
	raw := &types.RawTransaction{To: map[string]string{"bcrt1qext": "1.5"}}
	writePayerSendOutExtParam(raw, []*models.Unspent{{Address: "bcrt1qa", Amount: "2"}}, map[string]decimal.Decimal{
		"bcrt1qext": decimal.RequireFromString("1.5"),
	})
	got := raw.ExtParam[extparam.KeyPayerSendOut]
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["bcrt1qa"] != "1.5" {
		t.Fatalf("sendOut=%q want 1.5", parsed["bcrt1qa"])
	}
}

func TestWritePayerSendOutExtParamFeeOnlyCancel(t *testing.T) {
	raw := &types.RawTransaction{}
	writePayerSendOutExtParam(raw, []*models.Unspent{{Address: "bcrt1qa", Amount: "50"}}, nil)
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(raw.ExtParam[extparam.KeyPayerSendOut]), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["bcrt1qa"] != "0" {
		t.Fatalf("sendOut=%q want 0", parsed["bcrt1qa"])
	}
}
