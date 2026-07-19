package decoder

import (
	"encoding/json"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

const btcSatsScale = int32(8)

// writePayerSendOutExtParam records per-payer external sendOut (应付) at build time.
// Scanner derives fee_i = (vin_i − change_i) − sendOut_i from chain + this snapshot.
// Multi-payer summary: fee_i is each payer's marginal input vsize cost, not proportional to vin amount.
func writePayerSendOutExtParam(
	rawTx *types.RawTransaction,
	usedUTXO []*models.Unspent,
	externalOut map[string]decimal.Decimal,
	allOutputs map[string]decimal.Decimal,
) {
	if rawTx == nil || len(usedUTXO) == 0 {
		return
	}
	payerOrder, payerVin, payerInputCount := collectPayerUTXOStats(usedUTXO)
	if len(payerOrder) == 0 {
		return
	}
	totalExternal := decimal.Zero
	for _, amt := range externalOut {
		totalExternal = totalExternal.Add(amt)
	}
	changeByPayer := payerSelfChange(allOutputs, payerVin)
	sendOut := make(map[string]string, len(payerOrder))
	switch {
	case len(payerOrder) == 1:
		payer := payerOrder[0]
		if totalExternal.IsZero() {
			sendOut[payer] = "0"
		} else {
			sendOut[payer] = totalExternal.String()
		}
	case totalExternal.IsZero():
		for _, payer := range payerOrder {
			sendOut[payer] = "0"
		}
	default:
		greedy := greedyPayerSendOut(payerOrder, payerVin, totalExternal)
		if len(greedy) == 0 {
			return
		}
		contributors := countPositiveSendOutContributors(greedy)
		if contributors > 1 {
			totalFee, ok := resolveBuildTotalFee(rawTx, payerVin, totalExternal)
			if !ok {
				return
			}
			outputCount := int64(len(allOutputs))
			if outputCount <= 0 {
				outputCount = int64(len(externalOut))
			}
			if outputCount <= 0 {
				outputCount = 1
			}
			feeRate, err := util.ParseDecimalAmount(strings.TrimSpace(rawTx.FeeRate))
			if err != nil || feeRate.IsZero() {
				return
			}
			feeSats := marginalInputFeeSatsByPayer(payerOrder, payerInputCount, outputCount, feeRate, decimalToSatsFloor(totalFee))
			sendOut = payerSendOutFromFees(payerOrder, payerVin, changeByPayer, feeSats)
		} else {
			sendOut = greedy
		}
	}
	b, err := json.Marshal(sendOut)
	if err != nil || len(b) == 0 {
		return
	}
	if rawTx.ExtParam == nil {
		rawTx.ExtParam = make(map[string]string)
	}
	rawTx.ExtParam[extparam.KeyPayerSendOut] = string(b)
}

func collectPayerUTXOStats(usedUTXO []*models.Unspent) ([]string, map[string]decimal.Decimal, map[string]int64) {
	payerOrder := make([]string, 0, len(usedUTXO))
	payerVin := make(map[string]decimal.Decimal)
	payerInputCount := make(map[string]int64)
	for _, utxo := range usedUTXO {
		if utxo == nil {
			continue
		}
		addr := strings.TrimSpace(utxo.Address)
		if addr == "" {
			continue
		}
		amt, err := decimal.NewFromString(strings.TrimSpace(utxo.Amount))
		if err != nil || amt.IsZero() {
			continue
		}
		if _, seen := payerVin[addr]; !seen {
			payerOrder = append(payerOrder, addr)
		}
		payerVin[addr] = payerVin[addr].Add(amt)
		payerInputCount[addr]++
	}
	return payerOrder, payerVin, payerInputCount
}

func payerSelfChange(allOutputs map[string]decimal.Decimal, payerVin map[string]decimal.Decimal) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal, len(payerVin))
	for addr, amt := range allOutputs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if _, isPayer := payerVin[addr]; !isPayer {
			continue
		}
		if amt.IsZero() {
			continue
		}
		out[addr] = out[addr].Add(amt)
	}
	return out
}

func countPositiveSendOutContributors(sendOut map[string]string) int {
	n := 0
	for _, amtStr := range sendOut {
		amt, err := decimal.NewFromString(amtStr)
		if err == nil && amt.GreaterThan(decimal.Zero) {
			n++
		}
	}
	return n
}

func resolveBuildTotalFee(rawTx *types.RawTransaction, payerVin map[string]decimal.Decimal, totalExternal decimal.Decimal) (decimal.Decimal, bool) {
	totalVin := decimal.Zero
	for _, vin := range payerVin {
		totalVin = totalVin.Add(vin)
	}
	totalFee := totalVin.Sub(totalExternal)
	if totalFee.LessThan(decimal.Zero) {
		return decimal.Zero, false
	}
	if rawTx != nil {
		if feeFromRaw, err := util.ParseDecimalAmount(strings.TrimSpace(rawTx.Fees)); err == nil && feeFromRaw.GreaterThanOrEqual(decimal.Zero) {
			totalFee = feeFromRaw
		}
	}
	return totalFee, true
}

// marginalInputFeeSatsByPayer assigns chain fee by each payer's marginal input vsize (UTXO order).
// Last payer absorbs rounding remainder so Σ fee_i == totalFeeSats.
func marginalInputFeeSatsByPayer(
	payerOrder []string,
	payerInputCount map[string]int64,
	outputCount int64,
	feeRate decimal.Decimal,
	totalFeeSats int64,
) map[string]int64 {
	out := make(map[string]int64, len(payerOrder))
	if totalFeeSats <= 0 || len(payerOrder) == 0 {
		return out
	}
	var inputCount int64
	var assigned int64
	for i, payer := range payerOrder {
		prev := inputCount
		inputCount += payerInputCount[payer]
		var feeSats int64
		if i == len(payerOrder)-1 {
			feeSats = totalFeeSats - assigned
		} else {
			feeAfter := estimateFeeSatsFromCounts(inputCount, outputCount, feeRate)
			feeBefore := estimateFeeSatsFromCounts(prev, outputCount, feeRate)
			feeSats = feeAfter - feeBefore
			if feeSats < 0 {
				feeSats = 0
			}
			assigned += feeSats
		}
		out[payer] = feeSats
	}
	return out
}

func estimateFeeSatsFromCounts(inputs, outputs int64, feeRate decimal.Decimal) int64 {
	if inputs <= 0 {
		return 0
	}
	vsize := util.SegwitTxVsize(inputs, outputs)
	fee := decimal.New(vsize, 0).Div(decimal.New(1000, 0)).Mul(feeRate)
	return decimalToSatsFloor(fee)
}

func payerSendOutFromFees(
	payerOrder []string,
	payerVin map[string]decimal.Decimal,
	changeByPayer map[string]decimal.Decimal,
	feeSats map[string]int64,
) map[string]string {
	sendOut := make(map[string]string, len(payerOrder))
	for _, payer := range payerOrder {
		vinSats := decimalToSatsFloor(payerVin[payer])
		changeSats := decimalToSatsFloor(changeByPayer[payer])
		outSats := vinSats - changeSats - feeSats[payer]
		if outSats < 0 {
			outSats = 0
		}
		sendOut[payer] = util.Decimal(decimal.New(outSats, -btcSatsScale), btcSatsScale)
	}
	return sendOut
}

// greedyPayerSendOut assigns external amount in UTXO order (dust+main fee-only patterns).
func greedyPayerSendOut(payerOrder []string, payerVin map[string]decimal.Decimal, totalExternal decimal.Decimal) map[string]string {
	sendOut := make(map[string]string, len(payerOrder))
	remaining := totalExternal
	for _, payer := range payerOrder {
		vin := payerVin[payer]
		if remaining.IsZero() {
			sendOut[payer] = "0"
			continue
		}
		if vin.LessThanOrEqual(remaining) {
			sendOut[payer] = vin.String()
			remaining = remaining.Sub(vin)
			continue
		}
		sendOut[payer] = remaining.String()
		remaining = decimal.Zero
	}
	if remaining.GreaterThan(decimal.Zero) {
		return nil
	}
	return sendOut
}

func decimalToSatsFloor(d decimal.Decimal) int64 {
	return d.Mul(decimal.New(1, btcSatsScale)).IntPart()
}

func externalOutFromRawTx(rawTx *types.RawTransaction) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal)
	if rawTx == nil || len(rawTx.To) == 0 {
		return out
	}
	for addr, amt := range rawTx.To {
		addr = strings.TrimSpace(addr)
		amt = strings.TrimSpace(amt)
		if addr == "" || amt == "" {
			continue
		}
		v, err := decimal.NewFromString(amt)
		if err != nil || v.IsZero() {
			continue
		}
		out[addr] = out[addr].Add(v)
	}
	return out
}
