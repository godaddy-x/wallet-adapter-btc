package decoder

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

func TestApplySpeedUpFeeRateBumpPercent(t *testing.T) {
	rawTx := &types.RawTransaction{FeeRate: "0.00001000"}
	su := &types.SpeedUp{
		Nonce:          "abc123",
		BaseFeeRate:    "0.00001000",
		FeeBumpPercent: 25,
	}
	if err := applySpeedUpFeeRate(su, rawTx); err != nil {
		t.Fatal(err)
	}
	if rawTx.FeeRate != "0.00001250" {
		t.Fatalf("feeRate = %q want 0.00001250", rawTx.FeeRate)
	}
}

func TestIsCancelReplacement(t *testing.T) {
	cancel, err := isCancelReplacement(&types.RawTransaction{To: map[string]string{"bc1qxxx": "0"}})
	if err != nil || !cancel {
		t.Fatalf("expected cancel when to amount is zero, cancel=%v err=%v", cancel, err)
	}
	speedUp, err := isCancelReplacement(&types.RawTransaction{To: map[string]string{"bc1qxxx": "0.1"}})
	if err != nil || speedUp {
		t.Fatalf("expected speed-up when to amount is non-zero, cancel=%v err=%v", speedUp, err)
	}
	_, err = isCancelReplacement(&types.RawTransaction{To: map[string]string{"bc1qxxx": "bad"}})
	if err == nil {
		t.Fatal("expected error for invalid cancel amount")
	}
	_, err = isCancelReplacement(&types.RawTransaction{})
	if err == nil {
		t.Fatal("expected error for empty to map")
	}
}

func TestEnforceRBFMinimumFee(t *testing.T) {
	dec := &BtcTransactionDecoder{}
	origin := &models.Transaction{
		Vins:  []*models.Vin{{Value: "0.001"}},
		Vouts: []*models.Vout{{Value: "0.00099"}},
	}
	proposed := decimal.NewFromFloat(0.00001200)
	got := dec.enforceRBFMinimumFee(origin, 1, 1, proposed)
	minWant := decimal.NewFromFloat(0.00001).Add(decimal.New(util.EstimateTxVsize(1, 1), 0).Mul(satPerVByte))
	if got.LessThan(minWant) {
		t.Fatalf("got %s want >= %s", got, minWant)
	}
}

func TestParseSpeedUpOriginTxID(t *testing.T) {
	if _, err := parseSpeedUpOriginTxID(&types.SpeedUp{}); err == nil {
		t.Fatal("expected error for empty nonce")
	}
	id, err := parseSpeedUpOriginTxID(&types.SpeedUp{Nonce: "  deadbeef  "})
	if err != nil || id != "deadbeef" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}
