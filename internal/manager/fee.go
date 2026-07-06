package manager

import (
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

const (
	FeeEstimateModeUnset        = ""
	FeeEstimateModeEconomical   = "economical"
	FeeEstimateModeConservative = "conservative"
)

// FeeRateResolver resolves feerate from node policy (estimatesmartfee) with dynamic bump hooks.
type FeeRateResolver interface {
	EstimateSmartFee(targetBlocks int, estimateMode string) (decimal.Decimal, error)
	BumpFeeRate(base decimal.Decimal, multiplier decimal.Decimal) decimal.Decimal
}

// CPFPFeeEstimator estimates child feerate to accelerate an unconfirmed parent (CPFP).
type CPFPFeeEstimator interface {
	EstimateChildFeeRate(parentPaidFee decimal.Decimal, parentVsize, childVsize int64, targetBlocks int) (decimal.Decimal, error)
}

// EstimateSmartFee queries estimatesmartfee (Core) or explorer fallback with target blocks + mode.
func (wm *WalletManager) EstimateSmartFee(targetBlocks int, estimateMode string) (decimal.Decimal, error) {
	if wm == nil || wm.Config == nil {
		return decimal.Zero, fmt.Errorf("wallet config is nil")
	}
	if targetBlocks <= 0 {
		targetBlocks = wm.Config.FeeTargetBlocks
	}
	mode := normalizeFeeEstimateMode(estimateMode)
	if wm.Config.RPCServerType == config.RPCServerExplorer {
		return wm.estimateFeeRateByExplorer()
	}
	return wm.estimateSmartFeeByCore(targetBlocks, mode)
}

// EstimateFeeRate returns the configured default feerate (BTC/KB).
func (wm *WalletManager) EstimateFeeRate() (decimal.Decimal, error) {
	if wm == nil || wm.Config == nil {
		return decimal.Zero, fmt.Errorf("wallet config is nil")
	}
	return wm.EstimateSmartFee(wm.Config.FeeTargetBlocks, wm.Config.FeeEstimateMode)
}

// EstimateFeeRateWithBump applies a multiplier on top of the current smart fee (RBF resubmit / manual bump).
func (wm *WalletManager) EstimateFeeRateWithBump(multiplier decimal.Decimal) (decimal.Decimal, error) {
	base, err := wm.EstimateFeeRate()
	if err != nil {
		return decimal.Zero, err
	}
	return wm.BumpFeeRate(base, multiplier), nil
}

// BumpFeeRate scales feerate and enforces configured minimum (BTC/KB).
func (wm *WalletManager) BumpFeeRate(base, multiplier decimal.Decimal) decimal.Decimal {
	if multiplier.LessThanOrEqual(decimal.Zero) {
		multiplier = decimal.NewFromInt(1)
	}
	rate := base.Mul(multiplier)
	return wm.enforceMinFeeRate(rate)
}

// EstimateChildFeeRate suggests CPFP child feerate (BTC/KB) to pull an unconfirmed parent through mempool.
// parentPaidFee and sizes are in BTC and vbytes respectively; uses smart fee as floor then solves CPFP gap.
func (wm *WalletManager) EstimateChildFeeRate(parentPaidFee decimal.Decimal, parentVsize, childVsize int64, targetBlocks int) (decimal.Decimal, error) {
	if childVsize <= 0 {
		return decimal.Zero, fmt.Errorf("child vsize must be positive")
	}
	base, err := wm.EstimateSmartFee(targetBlocks, wm.Config.FeeEstimateMode)
	if err != nil {
		return decimal.Zero, err
	}
	base = wm.BumpFeeRate(base, wm.Config.FeeBumpMultiplier)

	// Required total fee at base rate for combined package (parent + child).
	combinedVsize := decimal.New(parentVsize+childVsize, 0)
	requiredTotal := combinedVsize.Div(decimal.New(1000, 0)).Mul(base)
	gap := requiredTotal.Sub(parentPaidFee)
	if gap.LessThanOrEqual(decimal.Zero) {
		return base, nil
	}
	childRate := gap.Mul(decimal.New(1000, 0)).Div(decimal.New(childVsize, 0))
	if childRate.LessThan(base) {
		return base, nil
	}
	return wm.enforceMinFeeRate(childRate), nil
}

func (wm *WalletManager) estimateSmartFeeByCore(targetBlocks int, estimateMode string) (decimal.Decimal, error) {
	if wm.Client == nil {
		return wm.fallbackFeeRate(), nil
	}
	var result *gjson.Result
	var err error
	if estimateMode != "" {
		result, err = wm.Client.Call("estimatesmartfee", []interface{}{targetBlocks, estimateMode})
	} else {
		result, err = wm.Client.Call("estimatesmartfee", []interface{}{targetBlocks})
	}
	if err == nil && result != nil {
		if rate, ok := parseSmartFeeResult(result); ok {
			return wm.enforceMinFeeRate(rate), nil
		}
	}
	// Legacy fallback; regtest often lacks fee history.
	fallback, err2 := wm.Client.Call("estimatefee", []interface{}{targetBlocks})
	if err2 != nil {
		return wm.fallbackFeeRate(), nil
	}
	rate, parseErr := decimal.NewFromString(fallback.String())
	if parseErr != nil || rate.LessThanOrEqual(decimal.Zero) {
		return wm.fallbackFeeRate(), nil
	}
	return wm.enforceMinFeeRate(rate), nil
}

func (wm *WalletManager) estimateFeeRateByExplorer() (decimal.Decimal, error) {
	if wm.ExplorerClient == nil {
		return wm.fallbackFeeRate(), nil
	}
	target := 2
	if wm.Config != nil && wm.Config.FeeTargetBlocks > 0 {
		target = wm.Config.FeeTargetBlocks
	}
	result, err := wm.ExplorerClient.Call(fmt.Sprintf("utils/estimatefee?nbBlocks=%d", target), nil, "GET")
	if err != nil {
		return wm.fallbackFeeRate(), nil
	}
	feeRate, _ := decimal.NewFromString(result.Get(fmt.Sprintf("%d", target)).String())
	if feeRate.LessThanOrEqual(decimal.Zero) {
		return wm.fallbackFeeRate(), nil
	}
	return wm.enforceMinFeeRate(feeRate), nil
}

func parseSmartFeeResult(result *gjson.Result) (decimal.Decimal, bool) {
	if result == nil {
		return decimal.Zero, false
	}
	if errs := gjson.Get(result.Raw, "errors"); errs.Exists() && len(errs.Array()) > 0 {
		return decimal.Zero, false
	}
	feeStr := gjson.Get(result.Raw, "feerate").String()
	if feeStr == "" {
		return decimal.Zero, false
	}
	rate, err := decimal.NewFromString(feeStr)
	if err != nil || rate.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, false
	}
	return rate, true
}

func (wm *WalletManager) fallbackFeeRate() decimal.Decimal {
	if wm == nil || wm.Config == nil {
		return decimal.NewFromFloat(0.00001)
	}
	if wm.Config.MinFeeRate.GreaterThan(decimal.Zero) {
		return wm.Config.MinFeeRate
	}
	if wm.Config.MinFees.GreaterThan(decimal.Zero) {
		return wm.Config.MinFees
	}
	return decimal.NewFromFloat(0.00001)
}

func (wm *WalletManager) enforceMinFeeRate(rate decimal.Decimal) decimal.Decimal {
	min := wm.fallbackFeeRate()
	if rate.LessThan(min) {
		return min
	}
	return rate
}

func normalizeFeeEstimateMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case FeeEstimateModeEconomical, FeeEstimateModeConservative:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return FeeEstimateModeUnset
	}
}
