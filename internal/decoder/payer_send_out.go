package decoder

import (
	"encoding/json"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

// writePayerSendOutExtParam records per-payer external sendOut at build time (required for scanner accounting).
// sendOut_i is the external outflow attributed to payer i, in UTXO selection order.
// Fee-only / cancel txs use sendOut "0" for every payer.
func writePayerSendOutExtParam(rawTx *types.RawTransaction, usedUTXO []*models.Unspent, externalOut map[string]decimal.Decimal) {
	if rawTx == nil || len(usedUTXO) == 0 {
		return
	}
	payerOrder := make([]string, 0, len(usedUTXO))
	payerVin := make(map[string]decimal.Decimal)
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
	}
	if len(payerOrder) == 0 {
		return
	}
	remaining := decimal.Zero
	for _, amt := range externalOut {
		remaining = remaining.Add(amt)
	}
	sendOut := make(map[string]string, len(payerOrder))
	if len(payerOrder) == 1 {
		payer := payerOrder[0]
		if remaining.IsZero() {
			sendOut[payer] = "0"
		} else {
			sendOut[payer] = remaining.String()
		}
	} else {
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
