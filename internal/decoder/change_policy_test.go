package decoder

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

func testVsizeFeeEstimator(inputs, outputs int64, rate decimal.Decimal) (decimal.Decimal, error) {
	vsize := util.EstimateTxVsize(inputs, outputs)
	fee := decimal.New(vsize, 0).Div(decimal.New(1000, 0)).Mul(rate).Round(8)
	return fee, nil
}

func dustLimitBTC() decimal.Decimal {
	return decimal.New(config.DefaultDustLimitSats, -8)
}

func TestEstimateMarginalOutputFeeRoundUp(t *testing.T) {
	rate := decimal.RequireFromString("0.00001001")
	got := EstimateMarginalOutputFee(rate, 8)
	want := decimal.RequireFromString("0.00000032")
	if !got.Equal(want) {
		t.Fatalf("marginal fee = %s want %s (32 sats ceil)", got, want)
	}
	rounded := EstimateMarginalOutputFee(rate, 8).Round(8)
	if !rounded.Equal(got) {
		t.Fatalf("unexpected rounding change: %s", rounded)
	}
}

func TestApplyChangePolicyKeepsLargeChange(t *testing.T) {
	inputTotal := decimal.RequireFromString("1.0")
	totalSend := decimal.RequireFromString("0.4")
	rate := decimal.RequireFromString("0.00001")
	fees, err := testVsizeFeeEstimator(1, 2, rate)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := applyChangePolicy(ChangePolicyParams{
		InputTotal:          inputTotal,
		TotalSend:           totalSend,
		Fees:                fees,
		FeeRate:             rate,
		NumInputs:           1,
		OutputSlots:         2,
		Estimate:            testVsizeFeeEstimator,
		DustLimit:           dustLimitBTC(),
		OmitChangeBelowDust: true,
		Decimals:            8,
		ChangeAddr:          "change-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.ChangeAmount.GreaterThan(decimal.Zero) {
		t.Fatalf("expected change, got %s", policy.ChangeAmount)
	}
	if policy.DustDonatedSats != 0 {
		t.Fatalf("dust donated = %d want 0", policy.DustDonatedSats)
	}
}

func TestApplyChangePolicyDonatesDustChange(t *testing.T) {
	totalSend := decimal.RequireFromString("0.4")
	rate := decimal.RequireFromString("0.00001")
	fees, err := testVsizeFeeEstimator(1, 2, rate)
	if err != nil {
		t.Fatal(err)
	}
	inputTotal := totalSend.Add(fees).Add(decimal.RequireFromString("0.00000400")) // 400 sats change
	change := inputTotal.Sub(totalSend).Sub(fees)
	if !change.Equal(decimal.RequireFromString("0.00000400")) {
		t.Fatalf("precondition change = %s", change)
	}
	if !change.LessThan(dustLimitBTC()) {
		t.Fatalf("precondition: change %s should be below dust", change)
	}
	policy, err := applyChangePolicy(ChangePolicyParams{
		InputTotal:          inputTotal,
		TotalSend:           totalSend,
		Fees:                fees,
		FeeRate:             rate,
		NumInputs:           1,
		OutputSlots:         2,
		Estimate:            testVsizeFeeEstimator,
		DustLimit:           dustLimitBTC(),
		OmitChangeBelowDust: true,
		Decimals:            8,
		ChangeAddr:          "change-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.ChangeAmount.IsZero() {
		t.Fatalf("change = %s want 0", policy.ChangeAmount)
	}
	if policy.DustDonatedSats <= 0 {
		t.Fatal("expected dust_donated_sats > 0")
	}
	if !policy.Fees.Equal(inputTotal.Sub(totalSend)) {
		t.Fatalf("fees = %s want all leftover %s", policy.Fees, inputTotal.Sub(totalSend))
	}
}

func TestApplyChangePolicyRefinePseudoEffectiveChange(t *testing.T) {
	// change = 600 sats after 2-output fee; outputCost = 310 sats; netChange = 290 < dust
	totalSend := decimal.RequireFromString("0.4")
	rate := decimal.RequireFromString("0.0001")
	fees, err := testVsizeFeeEstimator(1, 2, rate)
	if err != nil {
		t.Fatal(err)
	}
	inputTotal := totalSend.Add(fees).Add(decimal.RequireFromString("0.000006"))
	change := inputTotal.Sub(totalSend).Sub(fees)
	if !change.Equal(decimal.RequireFromString("0.000006")) {
		t.Fatalf("precondition change = %s", change)
	}
	outputCost := EstimateMarginalOutputFee(rate, 8)
	if !outputCost.Equal(decimal.RequireFromString("0.0000031")) {
		t.Fatalf("output cost = %s", outputCost)
	}
	policy, err := applyChangePolicy(ChangePolicyParams{
		InputTotal:          inputTotal,
		TotalSend:           totalSend,
		Fees:                fees,
		FeeRate:             rate,
		NumInputs:           1,
		OutputSlots:         2,
		Estimate:            testVsizeFeeEstimator,
		DustLimit:           dustLimitBTC(),
		OmitChangeBelowDust: true,
		Decimals:            8,
		ChangeAddr:          "change-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.ChangeAmount.IsZero() {
		t.Fatalf("expected donate pseudo-effective change, got %s", policy.ChangeAmount)
	}
	if policy.DustDonatedSats <= 0 {
		t.Fatal("expected dust donation")
	}
}

func TestApplyChangePolicyCancelKeepsSmallChange(t *testing.T) {
	inputTotal := decimal.RequireFromString("0.00001000")
	fees := decimal.RequireFromString("0.00000950")
	policy, err := applyChangePolicy(ChangePolicyParams{
		InputTotal:          inputTotal,
		TotalSend:           decimal.Zero,
		Fees:                fees,
		FeeRate:             decimal.RequireFromString("0.00001"),
		NumInputs:           1,
		OutputSlots:         1,
		Estimate:            testVsizeFeeEstimator,
		DustLimit:           dustLimitBTC(),
		OmitChangeBelowDust: true,
		Decimals:            8,
		IsCancel:            true,
		ChangeAddr:          "self",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantChange := decimal.RequireFromString("0.00000050")
	if !policy.ChangeAmount.Equal(wantChange) {
		t.Fatalf("cancel change = %s want %s", policy.ChangeAmount, wantChange)
	}
	if policy.DustDonatedSats != 0 {
		t.Fatalf("cancel must not donate dust, got %d sats", policy.DustDonatedSats)
	}
}

func satsBTC(sats int64) decimal.Decimal {
	return decimal.New(sats, -8)
}

func applyPolicyWithNominalChange(
	t *testing.T,
	changeSats int64,
	rate decimal.Decimal,
	omitDust bool,
	isCancel bool,
) (*ChangePolicyResult, decimal.Decimal) {
	t.Helper()
	totalSend := decimal.RequireFromString("0.4")
	outputSlots := 2
	if isCancel {
		outputSlots = 1
		totalSend = decimal.Zero
	}
	fees, err := testVsizeFeeEstimator(1, int64(outputSlots), rate)
	if err != nil {
		t.Fatal(err)
	}
	inputTotal := totalSend.Add(fees).Add(satsBTC(changeSats))
	policy, err := applyChangePolicy(ChangePolicyParams{
		InputTotal:          inputTotal,
		TotalSend:           totalSend,
		Fees:                fees,
		FeeRate:             rate,
		NumInputs:           1,
		OutputSlots:         outputSlots,
		Estimate:            testVsizeFeeEstimator,
		DustLimit:           dustLimitBTC(),
		OmitChangeBelowDust: omitDust,
		Decimals:            8,
		IsCancel:            isCancel,
		ChangeAddr:          "change-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy, fees
}

func TestApplyChangePolicyDonates545Sats(t *testing.T) {
	rate := decimal.RequireFromString("0.00001")
	policy, fees := applyPolicyWithNominalChange(t, 545, rate, true, false)
	if !policy.ChangeAmount.IsZero() {
		t.Fatalf("change = %s want 0", policy.ChangeAmount)
	}
	if policy.DustDonatedSats < 545 {
		t.Fatalf("dust_donated_sats = %d want >= 545", policy.DustDonatedSats)
	}
	if policy.OutputSlots != 1 {
		t.Fatalf("outputSlots = %d want 1", policy.OutputSlots)
	}
	feePrime := policy.Fees.Sub(satsBTC(policy.DustDonatedSats))
	if !feePrime.LessThan(fees) {
		t.Fatalf("fee' %s not less than 2-output fee %s", feePrime, fees)
	}
}

func TestApplyChangePolicyDonates546SatsAfterRefine(t *testing.T) {
	// change == dustLimit; netChange = 546 - 310 = 236 < 546 → donate
	rate := decimal.RequireFromString("0.0001")
	policy, _ := applyPolicyWithNominalChange(t, 546, rate, true, false)
	if !policy.ChangeAmount.IsZero() {
		t.Fatalf("change = %s want 0 (546 at refine boundary)", policy.ChangeAmount)
	}
	if policy.DustDonatedSats <= 0 {
		t.Fatal("expected dust donation at 546 sats boundary")
	}
}

func TestApplyChangePolicyKeeps900SatsAfterRefine(t *testing.T) {
	// netChange = 900 - 310 = 590 >= 546 → keep change
	rate := decimal.RequireFromString("0.0001")
	policy, _ := applyPolicyWithNominalChange(t, 900, rate, true, false)
	if !policy.ChangeAmount.Equal(satsBTC(900)) {
		t.Fatalf("change = %s want 900 sats", policy.ChangeAmount)
	}
	if policy.DustDonatedSats != 0 {
		t.Fatalf("dust_donated_sats = %d want 0", policy.DustDonatedSats)
	}
}

func TestApplyChangePolicySpeedUpPathDonatesSubDust(t *testing.T) {
	// SpeedUp uses IsCancel=false; same dust policy as normal transfer.
	rate := decimal.RequireFromString("0.00001")
	policy, _ := applyPolicyWithNominalChange(t, 400, rate, true, false)
	if !policy.ChangeAmount.IsZero() {
		t.Fatalf("speed-up path change = %s want 0", policy.ChangeAmount)
	}
	if policy.DustDonatedSats <= 0 {
		t.Fatal("speed-up path expected dust donation")
	}
}

func TestApplyChangePolicyRespectsOmitChangeBelowDustOff(t *testing.T) {
	rate := decimal.RequireFromString("0.00001")
	policy, _ := applyPolicyWithNominalChange(t, 400, rate, false, false)
	if !policy.ChangeAmount.Equal(satsBTC(400)) {
		t.Fatalf("change = %s want 400 sats when OmitChangeBelowDust=false", policy.ChangeAmount)
	}
	if policy.DustDonatedSats != 0 {
		t.Fatalf("dust_donated_sats = %d want 0", policy.DustDonatedSats)
	}
}

func TestWriteDustDonatedExtParam(t *testing.T) {
	raw := &types.RawTransaction{}
	writeDustDonatedExtParam(raw, 545)
	if raw.ExtParam == nil || raw.ExtParam[extparam.KeyDustDonatedSats] != "545" {
		t.Fatalf("extParam = %v want dust_donated_sats=545", raw.ExtParam)
	}
	writeDustDonatedExtParam(nil, 100)
	writeDustDonatedExtParam(raw, 0)
	if raw.ExtParam[extparam.KeyDustDonatedSats] != "545" {
		t.Fatal("zero donation must not overwrite extParam")
	}
}

func TestApplyChangePolicyDonatesDustMultipleRecipients(t *testing.T) {
	recvA := decimal.RequireFromString("0.2")
	recvB := decimal.RequireFromString("0.2")
	totalSend := recvA.Add(recvB)
	rate := decimal.RequireFromString("0.00001")
	fees, err := testVsizeFeeEstimator(1, 3, rate)
	if err != nil {
		t.Fatal(err)
	}
	inputTotal := totalSend.Add(fees).Add(satsBTC(400))
	policy, err := applyChangePolicy(ChangePolicyParams{
		InputTotal:          inputTotal,
		TotalSend:           totalSend,
		Fees:                fees,
		FeeRate:             rate,
		NumInputs:           1,
		OutputSlots:         3,
		Estimate:            testVsizeFeeEstimator,
		DustLimit:           dustLimitBTC(),
		OmitChangeBelowDust: true,
		Decimals:            8,
		ChangeAddr:          "change-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.ChangeAmount.IsZero() {
		t.Fatalf("multi-recipient change = %s want 0", policy.ChangeAmount)
	}
	if policy.DustDonatedSats <= 0 {
		t.Fatal("expected dust donation with multiple recipients")
	}
	if policy.OutputSlots != 2 {
		t.Fatalf("outputSlots = %d want 2 recipient outputs only", policy.OutputSlots)
	}
}

func TestApplyChangePolicyKeepsChangeMultipleRecipients(t *testing.T) {
	recvA := decimal.RequireFromString("0.2")
	recvB := decimal.RequireFromString("0.2")
	totalSend := recvA.Add(recvB)
	rate := decimal.RequireFromString("0.0001")
	fees, err := testVsizeFeeEstimator(1, 3, rate)
	if err != nil {
		t.Fatal(err)
	}
	inputTotal := totalSend.Add(fees).Add(satsBTC(900))
	policy, err := applyChangePolicy(ChangePolicyParams{
		InputTotal:          inputTotal,
		TotalSend:           totalSend,
		Fees:                fees,
		FeeRate:             rate,
		NumInputs:           1,
		OutputSlots:         3,
		Estimate:            testVsizeFeeEstimator,
		DustLimit:           dustLimitBTC(),
		OmitChangeBelowDust: true,
		Decimals:            8,
		ChangeAddr:          "change-addr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.ChangeAmount.Equal(satsBTC(900)) {
		t.Fatalf("multi-recipient change = %s want 900 sats", policy.ChangeAmount)
	}
	if policy.DustDonatedSats != 0 {
		t.Fatalf("dust_donated_sats = %d want 0", policy.DustDonatedSats)
	}
}
