package decoder

import (
	"fmt"
	"strconv"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

// ChangePolicyParams inputs for dust / refine change handling.
type ChangePolicyParams struct {
	InputTotal decimal.Decimal // sum of selected inputs (BTC)
	TotalSend  decimal.Decimal // external send amount (BTC); 0 for cancel
	Fees       decimal.Decimal // fee estimated with change output slot (BTC)
	FeeRate    decimal.Decimal // BTC/KB
	NumInputs  int64
	OutputSlots int // destination outputs + change slot when applicable
	Estimate   feeEstimator
	DustLimit  decimal.Decimal // BTC
	OmitChangeBelowDust bool
	Decimals   int32
	IsCancel   bool
	ChangeAddr string
}

// ChangePolicyResult resolved fees and optional change after dust policy.
type ChangePolicyResult struct {
	Fees            decimal.Decimal
	ChangeAmount    decimal.Decimal
	ChangeAddr      string
	OutputSlots     int
	DustDonatedSats int64
}

// EstimateMarginalOutputFee returns fee (BTC) to add one P2WPKH vout at feeRate (BTC/KB).
// P2WPKH only; uses RoundUp to satoshi precision (relay-safe).
func EstimateMarginalOutputFee(feeRate decimal.Decimal, decimals int32) decimal.Decimal {
	if decimals <= 0 {
		decimals = 8
	}
	raw := decimal.New(util.P2WPKHOutputVsize, 0).Div(decimal.New(1000, 0)).Mul(feeRate)
	return raw.RoundUp(decimals)
}

func applyChangePolicy(p ChangePolicyParams) (*ChangePolicyResult, error) {
	if p.OutputSlots <= 0 {
		p.OutputSlots = 1
	}
	change := p.InputTotal.Sub(p.TotalSend).Sub(p.Fees)
	if change.LessThan(decimal.Zero) {
		return nil, fmt.Errorf("inputs cannot cover send and fee")
	}

	res := &ChangePolicyResult{
		Fees:         p.Fees,
		ChangeAmount: decimal.Zero,
		ChangeAddr:   p.ChangeAddr,
		OutputSlots:  p.OutputSlots,
	}

	if p.IsCancel {
		if change.GreaterThan(decimal.Zero) {
			res.ChangeAmount = change
		}
		return res, nil
	}

	if !p.OmitChangeBelowDust || !change.GreaterThan(decimal.Zero) {
		res.ChangeAmount = change
		return res, nil
	}

	if !shouldDonateChangeToFee(change, p.FeeRate, p.DustLimit, p.Decimals) {
		res.ChangeAmount = change
		return res, nil
	}

	return donateChangeToFee(p)
}

func shouldDonateChangeToFee(change, feeRate, dustLimit decimal.Decimal, decimals int32) bool {
	if change.LessThan(dustLimit) {
		return true
	}
	outputCost := EstimateMarginalOutputFee(feeRate, decimals)
	netChange := change.Sub(outputCost)
	return netChange.LessThan(dustLimit)
}

func donateChangeToFee(p ChangePolicyParams) (*ChangePolicyResult, error) {
	change := p.InputTotal.Sub(p.TotalSend).Sub(p.Fees)
	if p.OutputSlots <= 1 {
		return &ChangePolicyResult{
			Fees:            p.Fees.Add(change),
			ChangeAmount:    decimal.Zero,
			ChangeAddr:      p.ChangeAddr,
			OutputSlots:     p.OutputSlots,
			DustDonatedSats: btcAmountToSats(change, p.Decimals),
		}, nil
	}
	outputSlots := p.OutputSlots - 1
	feePrime, err := p.Estimate(p.NumInputs, int64(outputSlots), p.FeeRate)
	if err != nil {
		return nil, err
	}
	leftover := p.InputTotal.Sub(p.TotalSend).Sub(feePrime)
	if leftover.LessThan(decimal.Zero) {
		return nil, fmt.Errorf("inputs cannot cover send and fee without change output")
	}
	totalFee := feePrime.Add(leftover)
	return &ChangePolicyResult{
		Fees:            totalFee,
		ChangeAmount:    decimal.Zero,
		ChangeAddr:      p.ChangeAddr,
		OutputSlots:     outputSlots,
		DustDonatedSats: btcAmountToSats(leftover, p.Decimals),
	}, nil
}

func btcAmountToSats(amount decimal.Decimal, decimals int32) int64 {
	if decimals <= 0 {
		decimals = 8
	}
	sats := amount.Shift(decimals)
	if !sats.IsInteger() {
		sats = sats.RoundUp(0)
	}
	return sats.IntPart()
}

func writeDustDonatedExtParam(rawTx *types.RawTransaction, dustSats int64) {
	if rawTx == nil || dustSats <= 0 {
		return
	}
	if rawTx.ExtParam == nil {
		rawTx.ExtParam = make(map[string]string)
	}
	rawTx.ExtParam[extparam.KeyDustDonatedSats] = strconv.FormatInt(dustSats, 10)
}
