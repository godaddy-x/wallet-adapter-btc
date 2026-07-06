package manager

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/shopspring/decimal"
)

func TestBumpFeeRateEnforcesMinimum(t *testing.T) {
	wm := &WalletManager{Config: config.NewConfig("BTC")}
	wm.Config.MinFeeRate = decimal.RequireFromString("0.00002")

	got := wm.BumpFeeRate(decimal.RequireFromString("0.00001"), decimal.NewFromInt(1))
	if !got.Equal(decimal.RequireFromString("0.00002")) {
		t.Fatalf("got %s want min 0.00002", got)
	}

	got = wm.BumpFeeRate(decimal.RequireFromString("0.00005"), decimal.RequireFromString("1.5"))
	want := decimal.RequireFromString("0.000075")
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestEstimateChildFeeRateCPFP(t *testing.T) {
	wm := &WalletManager{Config: config.NewConfig("BTC")}
	wm.Config.MinFeeRate = decimal.RequireFromString("0.00001")
	wm.Config.FeeBumpMultiplier = decimal.NewFromInt(1)

	// Parent underpaid: 200 vB package needs more fee; child 100 vB must carry gap.
	parentPaid := decimal.RequireFromString("0.000001")
	parentVsize := int64(200)
	childVsize := int64(100)

	rate, err := wm.EstimateChildFeeRate(parentPaid, parentVsize, childVsize, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !rate.GreaterThan(decimal.RequireFromString("0.00001")) {
		t.Fatalf("expected elevated CPFP rate, got %s", rate)
	}
}

func TestFallbackFeeRateRegtest(t *testing.T) {
	wm := &WalletManager{Config: config.NewConfig("BTC")}
	wm.Config.Network = config.NetworkRegtest
	wm.Config.MinFeeRate = decimal.RequireFromString("0.00003")

	got := wm.fallbackFeeRate()
	if !got.Equal(decimal.RequireFromString("0.00003")) {
		t.Fatalf("fallback = %s", got)
	}
}
