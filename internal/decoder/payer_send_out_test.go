package decoder

import (
	"encoding/json"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
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
	}, nil)
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
	}, nil)
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
	writePayerSendOutExtParam(raw, []*models.Unspent{{Address: "bcrt1qa", Amount: "50"}}, nil, nil)
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(raw.ExtParam[extparam.KeyPayerSendOut]), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["bcrt1qa"] != "0" {
		t.Fatalf("sendOut=%q want 0", parsed["bcrt1qa"])
	}
}

func TestWritePayerSendOutExtParamTwoPayerSummaryEachBearsFee(t *testing.T) {
	addrA := "bcrt1qpayer-a"
	addrB := "bcrt1qpayer-b"
	external := "bcrt1qsummary-dest"
	externalAmt := decimal.RequireFromString("6.19492016")
	totalFee := decimal.RequireFromString("0.00001")
	feeRate := totalFee.Mul(decimal.New(1000, 0)).Div(decimal.New(util.SegwitTxVsize(2, 1), 0))
	raw := &types.RawTransaction{
		Fees:    totalFee.String(),
		FeeRate: feeRate.String(),
		To: map[string]string{
			external: externalAmt.String(),
		},
	}
	used := []*models.Unspent{
		{Address: addrA, Amount: "3.06993016"},
		{Address: addrB, Amount: "3.125"},
	}
	writePayerSendOutExtParam(raw, used, map[string]decimal.Decimal{
		external: externalAmt,
	}, map[string]decimal.Decimal{external: externalAmt})
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(raw.ExtParam[extparam.KeyPayerSendOut]), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed[addrA] == "3.06993016" {
		t.Fatalf("payer A must not absorb zero fee: sendOut=%q", parsed[addrA])
	}
	if parsed[addrB] == "3.12499" || parsed[addrB] == "3.125" {
		t.Fatalf("payer B must not absorb all chain fee alone: sendOut=%q", parsed[addrB])
	}
	sendA, _ := decimal.NewFromString(parsed[addrA])
	sendB, _ := decimal.NewFromString(parsed[addrB])
	feeA := decimal.RequireFromString("3.06993016").Sub(sendA)
	feeB := decimal.RequireFromString("3.125").Sub(sendB)
	if !feeA.GreaterThan(decimal.Zero) || !feeB.GreaterThan(decimal.Zero) {
		t.Fatalf("each payer needs fee share: feeA=%s feeB=%s", feeA, feeB)
	}
	feeSum := feeA.Add(feeB)
	if !feeSum.Equal(decimal.RequireFromString("0.00001")) {
		t.Fatalf("fee sum=%s want 0.00001", feeSum)
	}
}
