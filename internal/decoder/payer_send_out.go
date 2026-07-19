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

// writePayerSendOutExtParam persists each payer's 应付 (external sendOut) into ExtParam.
// Scanner later closes the loop: fee_i = (vin_i − change_i) − sendOut_i.
//
// Inputs are already fixed after the unsigned tx is built:
//   - usedUTXO → vin per payer
//   - allOutputs → self-change per payer
//   - externalOut → business external total (rawTx.To, change excluded)
func writePayerSendOutExtParam(
	rawTx *types.RawTransaction,
	usedUTXO []*models.Unspent,
	externalOut map[string]decimal.Decimal,
	allOutputs map[string]decimal.Decimal,
) {
	if rawTx == nil || len(usedUTXO) == 0 {
		return
	}
	order, vin := collectPayerVin(usedUTXO)
	if len(order) == 0 {
		return
	}
	external := sumDecimals(externalOut)
	change := selfChangeByPayer(allOutputs, vin)

	sendOut := map[string]string{}
	switch {
	case len(order) == 1:
		p := order[0]
		if external.IsZero() {
			sendOut[p] = "0"
		} else {
			sendOut[p] = external.String()
		}
	case external.IsZero():
		for _, p := range order {
			sendOut[p] = "0"
		}
	default:
		// Dust+main: first payer(s) cover external in UTXO order; leftover payers are fee-only ("0").
		// Multi-funder summary: each payer's 应付 = their grossOut's share of external (vin/change already known).
		sendOut = assignPayable(order, vin, change, external)
		if len(sendOut) == 0 {
			return
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

func collectPayerVin(usedUTXO []*models.Unspent) ([]string, map[string]decimal.Decimal) {
	order := make([]string, 0, len(usedUTXO))
	vin := make(map[string]decimal.Decimal)
	for _, u := range usedUTXO {
		if u == nil {
			continue
		}
		addr := strings.TrimSpace(u.Address)
		if addr == "" {
			continue
		}
		amt, err := decimal.NewFromString(strings.TrimSpace(u.Amount))
		if err != nil || amt.IsZero() {
			continue
		}
		if _, ok := vin[addr]; !ok {
			order = append(order, addr)
		}
		vin[addr] = vin[addr].Add(amt)
	}
	return order, vin
}

func selfChangeByPayer(allOutputs, vin map[string]decimal.Decimal) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal)
	for addr, amt := range allOutputs {
		addr = strings.TrimSpace(addr)
		if addr == "" || amt.IsZero() {
			continue
		}
		if _, isPayer := vin[addr]; !isPayer {
			continue
		}
		out[addr] = out[addr].Add(amt)
	}
	return out
}

func sumDecimals(m map[string]decimal.Decimal) decimal.Decimal {
	s := decimal.Zero
	for _, v := range m {
		s = s.Add(v)
	}
	return s
}

// assignPayable writes 应付 from already-known vin / change / external.
func assignPayable(
	order []string,
	vin, change map[string]decimal.Decimal,
	external decimal.Decimal,
) map[string]string {
	// Prefer UTXO-order cover when only one payer actually funds the external (dust+main).
	if greedy := coverExternalInOrder(order, vin, external); greedy != nil && positiveCount(greedy) <= 1 {
		return greedy
	}
	return payableFromGross(order, vin, change, external)
}

// coverExternalInOrder assigns external in UTXO order (vin caps). Returns nil if underfunded.
func coverExternalInOrder(order []string, vin map[string]decimal.Decimal, external decimal.Decimal) map[string]string {
	out := make(map[string]string, len(order))
	left := external
	for _, p := range order {
		if left.IsZero() {
			out[p] = "0"
			continue
		}
		v := vin[p]
		if v.LessThanOrEqual(left) {
			out[p] = v.String()
			left = left.Sub(v)
			continue
		}
		out[p] = left.String()
		left = decimal.Zero
	}
	if left.GreaterThan(decimal.Zero) {
		return nil
	}
	return out
}

func positiveCount(m map[string]string) int {
	n := 0
	for _, s := range m {
		if a, err := decimal.NewFromString(s); err == nil && a.IsPositive() {
			n++
		}
	}
	return n
}

// payableFromGross sets sendOut_i from each payer's grossOut = vin − change so that
// Σ sendOut = external. Fee is not computed here; scanner uses fee = grossOut − sendOut.
func payableFromGross(
	order []string,
	vin, change map[string]decimal.Decimal,
	external decimal.Decimal,
) map[string]string {
	gross := make([]int64, len(order))
	var totalGross int64
	for i, p := range order {
		g := toSats(vin[p]) - toSats(change[p])
		if g < 0 {
			return nil
		}
		gross[i] = g
		totalGross += g
	}
	ext := toSats(external)
	if totalGross <= 0 || ext < 0 || ext > totalGross {
		return nil
	}
	out := make(map[string]string, len(order))
	var used int64
	for i, p := range order {
		var part int64
		if i == len(order)-1 {
			part = ext - used
		} else {
			part = ext * gross[i] / totalGross
			used += part
		}
		if part < 0 || part > gross[i] {
			return nil
		}
		out[p] = util.Decimal(decimal.New(part, -btcSatsScale), btcSatsScale)
	}
	return out
}

func toSats(d decimal.Decimal) int64 {
	return d.Mul(decimal.New(1, btcSatsScale)).IntPart()
}

func externalOutFromRawTx(rawTx *types.RawTransaction) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal)
	if rawTx == nil {
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
