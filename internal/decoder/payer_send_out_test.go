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
	raw := &types.RawTransaction{To: map[string]string{"bcrt1qexternal": "0.003"}}
	used := []*models.Unspent{
		{Address: "bcrt1qdust", Amount: "0.003"},
		{Address: "bcrt1qmain", Amount: "49.96694326"},
	}
	writePayerSendOutExtParam(raw, used, map[string]decimal.Decimal{
		"bcrt1qexternal": decimal.RequireFromString("0.003"),
	}, nil)
	parsed := mustParseSendOut(t, raw)
	if parsed["bcrt1qdust"] != "0.003" || parsed["bcrt1qmain"] != "0" {
		t.Fatalf("got %v", parsed)
	}
}

func TestWritePayerSendOutExtParamSinglePayer(t *testing.T) {
	raw := &types.RawTransaction{To: map[string]string{"bcrt1qext": "1.5"}}
	writePayerSendOutExtParam(raw, []*models.Unspent{{Address: "bcrt1qa", Amount: "2"}}, map[string]decimal.Decimal{
		"bcrt1qext": decimal.RequireFromString("1.5"),
	}, nil)
	parsed := mustParseSendOut(t, raw)
	if parsed["bcrt1qa"] != "1.5" {
		t.Fatalf("sendOut=%q", parsed["bcrt1qa"])
	}
}

func TestWritePayerSendOutExtParamFeeOnlyCancel(t *testing.T) {
	raw := &types.RawTransaction{}
	writePayerSendOutExtParam(raw, []*models.Unspent{{Address: "bcrt1qa", Amount: "50"}}, nil, nil)
	parsed := mustParseSendOut(t, raw)
	if parsed["bcrt1qa"] != "0" {
		t.Fatalf("sendOut=%q", parsed["bcrt1qa"])
	}
}

func TestWritePayerSendOutExtParamMultiPayerIdentity(t *testing.T) {
	addrA, addrB := "bcrt1qpayer-a", "bcrt1qpayer-b"
	external := "bcrt1qsummary-dest"
	extAmt := decimal.RequireFromString("6.19492016")
	raw := &types.RawTransaction{
		Fees: "0.00001",
		To:   map[string]string{external: extAmt.String()},
	}
	used := []*models.Unspent{
		{Address: addrA, Amount: "3.06993016"},
		{Address: addrB, Amount: "3.125"},
	}
	writePayerSendOutExtParam(raw, used, map[string]decimal.Decimal{external: extAmt},
		map[string]decimal.Decimal{external: extAmt})
	parsed := mustParseSendOut(t, raw)
	sendA := decimal.RequireFromString(parsed[addrA])
	sendB := decimal.RequireFromString(parsed[addrB])
	if !sendA.Add(sendB).Equal(extAmt) {
		t.Fatalf("Σ应付=%s want %s", sendA.Add(sendB), extAmt)
	}
	// fee_i = vin_i − 应付_i (no change)
	feeA := decimal.RequireFromString("3.06993016").Sub(sendA)
	feeB := decimal.RequireFromString("3.125").Sub(sendB)
	if feeA.IsNegative() || feeB.IsNegative() {
		t.Fatalf("feeA=%s feeB=%s", feeA, feeB)
	}
	if !feeA.Add(feeB).Equal(decimal.RequireFromString("0.00001")) {
		t.Fatalf("Σfee=%s", feeA.Add(feeB))
	}
}

func TestWritePayerSendOutExtParamThreeEqualVin(t *testing.T) {
	addrs := []string{"bcrt1qa", "bcrt1qb", "bcrt1qc"}
	ext := "bcrt1qext"
	extAmt := decimal.RequireFromString("37.49999000")
	raw := &types.RawTransaction{Fees: "0.00001", To: map[string]string{ext: extAmt.String()}}
	used := []*models.Unspent{
		{Address: addrs[0], Amount: "12.5"},
		{Address: addrs[1], Amount: "12.5"},
		{Address: addrs[2], Amount: "12.5"},
	}
	writePayerSendOutExtParam(raw, used, map[string]decimal.Decimal{ext: extAmt},
		map[string]decimal.Decimal{ext: extAmt})
	parsed := mustParseSendOut(t, raw)
	var minFee, maxFee int64
	for i, addr := range addrs {
		fee := toSats(decimal.RequireFromString("12.5").Sub(decimal.RequireFromString(parsed[addr])))
		if i == 0 || fee < minFee {
			minFee = fee
		}
		if i == 0 || fee > maxFee {
			maxFee = fee
		}
	}
	if maxFee-minFee > 2 {
		t.Fatalf("equal vin fee spread %d..%d sendOut=%v", minFee, maxFee, parsed)
	}
}

func mustParseSendOut(t *testing.T, raw *types.RawTransaction) map[string]string {
	t.Helper()
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(raw.ExtParam[extparam.KeyPayerSendOut]), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return parsed
}
