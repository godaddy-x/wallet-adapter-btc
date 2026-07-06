package scanner

import (
	"sort"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

// btcFeeOutputIndex marks a separate miner-fee extract, aligned with ETH fee rows (OutputIndex=-2).
const btcFeeOutputIndex int64 = -2

func computeBTCTransactionFee(trx *models.Transaction) (decimal.Decimal, bool) {
	if trx == nil || !transactionHasSpendInputs(trx) {
		return decimal.Zero, false
	}
	inSum := decimal.Zero
	for _, in := range trx.Vins {
		if len(in.Coinbase) > 0 {
			continue
		}
		if !validExtractAmount(in.Value) {
			continue
		}
		amount, err := decimal.NewFromString(in.Value)
		if err != nil {
			continue
		}
		inSum = inSum.Add(amount)
	}
	outSum := decimal.Zero
	for _, out := range trx.Vouts {
		if out.Type == "OP_RETURN" {
			continue
		}
		if !validExtractAmount(out.Value) {
			continue
		}
		amount, err := decimal.NewFromString(out.Value)
		if err != nil {
			continue
		}
		outSum = outSum.Add(amount)
	}
	fee := inSum.Sub(outSum)
	if fee.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, false
	}
	return fee, true
}

func transactionHasSpendInputs(trx *models.Transaction) bool {
	for _, in := range trx.Vins {
		if len(in.Coinbase) == 0 {
			return true
		}
	}
	return false
}

type btcPayerLeg struct {
	itemIndex   int
	sourceKey   string
	payerAddr   string
	vinSats     int64
	voutSats    int64
	netOutSats  int64
	sendOutSats int64
	feeSats     int64
}

type outboundTarget struct {
	addr string
	sats int64
}

func sumLegAmounts(addrs, amts []string) decimal.Decimal {
	sum := decimal.Zero
	for i, amt := range amts {
		if i >= len(addrs) {
			break
		}
		amount, err := decimal.NewFromString(amt)
		if err != nil {
			continue
		}
		sum = sum.Add(amount)
	}
	return sum
}

// decimalToSats converts a BTC amount to whole satoshis.
func decimalToSats(d decimal.Decimal, decimals int32) (int64, bool) {
	shifted := d.Shift(decimals)
	if !shifted.Equal(shifted.Truncate(0)) {
		return 0, false
	}
	return shifted.IntPart(), true
}

// satsToAmount formats whole satoshis back to a BTC decimal string (max 8 dp, no rounding).
func satsToAmount(sats int64, decimals int32) string {
	return decimal.New(sats, 0).Shift(-decimals).String()
}

func sumTransactionVoutSats(trx *models.Transaction, decimals int32) (int64, bool) {
	if trx == nil {
		return 0, false
	}
	sum := int64(0)
	for _, out := range trx.Vouts {
		if out.Type == "OP_RETURN" {
			continue
		}
		if !validExtractAmount(out.Value) {
			continue
		}
		amount, err := decimal.NewFromString(out.Value)
		if err != nil {
			continue
		}
		sats, ok := decimalToSats(amount, decimals)
		if !ok {
			return 0, false
		}
		sum += sats
	}
	return sum, true
}

// computePayerLegAccounting derives sendOut/fee per payer from input/output:
//
//	netOut_i  = vin_i - vout_i
//	sendOut_i = outbound transfer attributed to addr i
//	fee_i     = netOut_i - sendOut_i
func computePayerLegAccounting(trx *models.Transaction, items []*types.ExtractDataItem, decimals int32) []btcPayerLeg {
	legs := collectBTCPayerLegs(items, decimals)
	if len(legs) == 0 {
		return nil
	}
	totalVoutSats, ok := sumTransactionVoutSats(trx, decimals)
	if !ok {
		return nil
	}

	payerChangeSats := int64(0)
	vinOnlySendSats := int64(0)
	changeIndexes := make([]int, 0, len(legs))
	for i := range legs {
		legs[i].netOutSats = legs[i].vinSats - legs[i].voutSats
		payerChangeSats += legs[i].voutSats
		if legs[i].voutSats > 0 {
			changeIndexes = append(changeIndexes, i)
			continue
		}
		legs[i].sendOutSats = legs[i].vinSats
		vinOnlySendSats += legs[i].vinSats
	}

	sendOutTotalSats := totalVoutSats - payerChangeSats
	if sendOutTotalSats < vinOnlySendSats {
		feeTotal, ok := computeBTCTransactionFee(trx)
		if !ok {
			return nil
		}
		feeSats, ok := decimalToSats(feeTotal, decimals)
		if !ok || sendOutTotalSats+feeSats < vinOnlySendSats {
			return nil
		}
	}
	remainingSendSats := sendOutTotalSats - vinOnlySendSats
	if remainingSendSats < 0 {
		remainingSendSats = 0
	}

	switch len(changeIndexes) {
	case 0:
		if len(legs) == 1 {
			legs[0].sendOutSats = sendOutTotalSats
			break
		}
		// Multi-payer summary (e.g. A,B,C → D): allocate external out by vin share.
		allocated := int64(0)
		vinOnlyIdx := make([]int, 0, len(legs))
		for i := range legs {
			if legs[i].voutSats == 0 {
				vinOnlyIdx = append(vinOnlyIdx, i)
			}
		}
		for j, idx := range vinOnlyIdx {
			if j == len(vinOnlyIdx)-1 {
				legs[idx].sendOutSats = sendOutTotalSats - allocated
			} else if vinOnlySendSats > 0 {
				part := sendOutTotalSats * legs[idx].vinSats / vinOnlySendSats
				legs[idx].sendOutSats = part
				allocated += part
			}
		}
	case 1:
		legs[changeIndexes[0]].sendOutSats = remainingSendSats
	default:
		best := changeIndexes[0]
		for _, idx := range changeIndexes[1:] {
			if legs[idx].voutSats > legs[best].voutSats {
				best = idx
			}
		}
		for _, idx := range changeIndexes {
			if idx == best {
				legs[idx].sendOutSats = remainingSendSats
			} else {
				legs[idx].sendOutSats = legs[idx].netOutSats
				remainingSendSats -= legs[idx].sendOutSats
			}
		}
	}

	for i := range legs {
		legs[i].feeSats = legs[i].netOutSats - legs[i].sendOutSats
		if legs[i].feeSats < 0 {
			return nil
		}
	}
	return legs
}

func collectOutboundVoutTargets(trx *models.Transaction, payerLegs []btcPayerLeg, decimals int32) []outboundTarget {
	changeByAddr := make(map[string]int64, len(payerLegs))
	for _, leg := range payerLegs {
		if leg.voutSats > 0 {
			changeByAddr[leg.payerAddr] = leg.voutSats
		}
	}
	targets := make([]outboundTarget, 0)
	for _, out := range trx.Vouts {
		if out.Type == "OP_RETURN" {
			continue
		}
		if !validExtractAmount(out.Value) {
			continue
		}
		addr := normalizeScanAddress(out.Addr)
		if addr == "" {
			continue
		}
		amount, err := decimal.NewFromString(out.Value)
		if err != nil {
			continue
		}
		sats, ok := decimalToSats(amount, decimals)
		if !ok || sats <= 0 {
			continue
		}
		if changeSats, ok := changeByAddr[addr]; ok && changeSats == sats {
			continue
		}
		targets = append(targets, outboundTarget{addr: addr, sats: sats})
	}
	return targets
}

func attachSendOutTargets(
	items []*types.ExtractDataItem,
	legs []btcPayerLeg,
	targets []outboundTarget,
	decimals int32,
) {
	if len(targets) == 0 {
		return
	}
	totalTargetSats := int64(0)
	for _, target := range targets {
		totalTargetSats += target.sats
	}
	if totalTargetSats <= 0 {
		return
	}
	for _, leg := range legs {
		if leg.sendOutSats <= 0 {
			continue
		}
		item := items[leg.itemIndex]
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil {
			continue
		}
		allocated := int64(0)
		outAddrs := make([]string, 0, len(targets))
		outAmts := make([]string, 0, len(targets))
		for i, target := range targets {
			var part int64
			if i == len(targets)-1 {
				part = leg.sendOutSats - allocated
			} else {
				part = leg.sendOutSats * target.sats / totalTargetSats
				allocated += part
			}
			if part <= 0 {
				continue
			}
			outAddrs = append(outAddrs, target.addr)
			outAmts = append(outAmts, satsToAmount(part, decimals))
		}
		if len(outAddrs) == 0 {
			continue
		}
		tx.ToAddr = append(outAddrs, tx.ToAddr...)
		tx.ToAmt = append(outAmts, tx.ToAmt...)
	}
}

func filterReceiveItemsCoveredBySend(items []*types.ExtractDataItem, decimals int32) []*types.ExtractDataItem {
	credited := make(map[string]map[string]int64)
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.TxAction == "fee" || len(tx.FromAddr) == 0 {
			continue
		}
		if tx.TxAction != "send" && tx.TxAction != "internal" {
			continue
		}
		payerAddr := normalizeScanAddress(tx.FromAddr[0])
		if credited[item.SourceKey] == nil {
			credited[item.SourceKey] = make(map[string]int64)
		}
		for i, to := range tx.ToAddr {
			if i >= len(tx.ToAmt) {
				break
			}
			toAddr := normalizeScanAddress(to)
			if toAddr == "" || toAddr == payerAddr {
				continue
			}
			amount, err := decimal.NewFromString(tx.ToAmt[i])
			if err != nil {
				continue
			}
			sats, ok := decimalToSats(amount, decimals)
			if !ok {
				continue
			}
			credited[item.SourceKey][toAddr] += sats
		}
	}

	out := make([]*types.ExtractDataItem, 0, len(items))
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			out = append(out, item)
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.TxAction != "receive" || len(tx.ToAddr) == 0 {
			out = append(out, item)
			continue
		}
		recvSats, ok := decimalToSats(sumLegAmounts(tx.ToAddr, tx.ToAmt), decimals)
		if !ok || recvSats <= 0 {
			out = append(out, item)
			continue
		}
		recvAddr := normalizeScanAddress(tx.ToAddr[0])
		if credited[item.SourceKey][recvAddr] >= recvSats {
			continue
		}
		out = append(out, item)
	}
	return out
}

func collectBTCPayerLegs(items []*types.ExtractDataItem, decimals int32) []btcPayerLeg {
	out := make([]btcPayerLeg, 0)
	for i, item := range items {
		if item == nil || item.SourceKey == "" || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.TxAction == "fee" || len(tx.FromAddr) == 0 {
			continue
		}
		vinSum := sumLegAmounts(tx.FromAddr, tx.FromAmt)
		voutSum := sumLegAmounts(tx.ToAddr, tx.ToAmt)
		vinSats, ok := decimalToSats(vinSum, decimals)
		if !ok || vinSats <= 0 {
			continue
		}
		voutSats, ok := decimalToSats(voutSum, decimals)
		if !ok || vinSats <= voutSats {
			continue
		}
		out = append(out, btcPayerLeg{
			itemIndex: i,
			sourceKey: item.SourceKey,
			payerAddr: normalizeScanAddress(tx.FromAddr[0]),
			vinSats:   vinSats,
			voutSats:  voutSats,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].sourceKey != out[j].sourceKey {
			return out[i].sourceKey < out[j].sourceKey
		}
		return out[i].payerAddr < out[j].payerAddr
	})
	return out
}

func btcFeeOutputIndexForLeg(legIndex int) int64 {
	return btcFeeOutputIndex - int64(legIndex*2)
}

func appendBTCTransactionFeeItems(
	trx *models.Transaction,
	items *[]*types.ExtractDataItem,
	coin types.Coin,
	createAt int64,
	decimals int32,
) {
	legs := computePayerLegAccounting(trx, *items, decimals)
	if len(legs) > 0 {
		targets := collectOutboundVoutTargets(trx, legs, decimals)
		attachSendOutTargets(*items, legs, targets, decimals)

		for _, leg := range legs {
			if leg.sendOutSats <= 0 {
				continue
			}
			sendItem := (*items)[leg.itemIndex]
			if sendItem == nil || len(sendItem.Data) == 0 || sendItem.Data[0] == nil {
				continue
			}
			sendTx := sendItem.Data[0].Transaction
			if sendTx == nil || sendTx.TxAction == "fee" {
				continue
			}
			normalizePayerOutboundLeg(sendTx, leg, decimals)
		}

		feeLegIndex := 0
		for _, leg := range legs {
			if leg.feeSats <= 0 {
				continue
			}
			feeStr := satsToAmount(leg.feeSats, decimals)
			*items = append(*items, &types.ExtractDataItem{
				SourceKey: leg.sourceKey,
				Data: []*types.TxExtractData{{
					Transaction: &types.Transaction{
						TxID:        trx.TxID,
						Coin:        coin,
						FromAddr:    []string{leg.payerAddr},
						FromAmt:     []string{feeStr},
						ToAddr:      nil,
						ToAmt:       nil,
						Amount:      "0",
						Fees:        feeStr,
						Decimal:     decimals,
						BlockHash:   trx.BlockHash,
						BlockHeight: trx.BlockHeight,
						Confirm:     int64(trx.Confirmations),
						ConfirmTime: trx.Blocktime,
						Status:      types.TxStatusSuccess,
						OutputIndex: btcFeeOutputIndexForLeg(feeLegIndex),
						TxAction:    "fee",
					},
				}},
			})
			feeLegIndex++
		}
		*items = filterReceiveItemsCoveredBySend(*items, decimals)
	}
	// Always collapse per-UTXO from rows to one payer address when fee accounting is skipped or partial.
	collapseSamePayerFromLegs(*items, decimals)
}

// collapseSamePayerFromLegs merges multiple from legs that share one payer address
// (typical summary / multi-UTXO input) when full fee_extract normalization did not run.
func collapseSamePayerFromLegs(items []*types.ExtractDataItem, decimals int32) {
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.TxAction == "fee" || tx.TxAction == "receive" || len(tx.FromAddr) <= 1 {
			continue
		}
		payer := normalizeScanAddress(tx.FromAddr[0])
		if payer == "" {
			continue
		}
		for _, addr := range tx.FromAddr {
			if normalizeScanAddress(addr) != payer {
				payer = ""
				break
			}
		}
		if payer == "" {
			continue
		}
		vinSum := sumLegAmounts(tx.FromAddr, tx.FromAmt)
		vinSats, ok := decimalToSats(vinSum, decimals)
		if !ok || vinSats <= 0 {
			continue
		}
		tx.FromAddr = []string{payer}
		tx.FromAmt = []string{satsToAmount(vinSats, decimals)}
	}
}

// normalizePayerOutboundLeg normalizes payer outbound rows after fee_extract accounting.
func normalizePayerOutboundLeg(tx *types.Transaction, leg btcPayerLeg, decimals int32) {
	if tx == nil || leg.payerAddr == "" || leg.vinSats <= 0 {
		return
	}
	if tx.TxAction == "internal" {
		normalizePayerInternalLeg(tx, leg, decimals)
		return
	}
	normalizePayerSendLeg(tx, leg, decimals)
}

// normalizePayerInternalLeg keeps change credit on the payer while Amount shows the transfer to peer.
func normalizePayerInternalLeg(tx *types.Transaction, leg btcPayerLeg, decimals int32) {
	payer := normalizeScanAddress(leg.payerAddr)
	tx.FromAddr = []string{payer}
	tx.FromAmt = []string{satsToAmount(leg.vinSats, decimals)}

	outAddrs, outAmts := externalOutboundLegs(tx.ToAddr, tx.ToAmt, payer)
	if leg.voutSats > 0 {
		outAddrs = append(outAddrs, payer)
		outAmts = append(outAmts, satsToAmount(leg.voutSats, decimals))
	}
	tx.ToAddr = outAddrs
	tx.ToAmt = outAmts
	if leg.sendOutSats > 0 {
		tx.Amount = satsToAmount(leg.sendOutSats, decimals)
	} else if len(outAmts) == 1 && normalizeScanAddress(outAddrs[0]) != payer {
		tx.Amount = outAmts[0]
	}
	tx.Fees = "0"
}

// normalizePayerSendLeg collapses UTXO-level from/to into one payer → external target row
// for upstream trade_log: total input per address, outbound to summary/recipient only, fee on fee row.
func normalizePayerSendLeg(tx *types.Transaction, leg btcPayerLeg, decimals int32) {
	if tx == nil || leg.payerAddr == "" || leg.vinSats <= 0 {
		return
	}
	payer := normalizeScanAddress(leg.payerAddr)
	tx.FromAddr = []string{payer}
	tx.FromAmt = []string{satsToAmount(leg.vinSats, decimals)}

	outAddrs, outAmts := externalOutboundLegs(tx.ToAddr, tx.ToAmt, payer)
	tx.ToAddr = outAddrs
	tx.ToAmt = outAmts
	if len(outAmts) == 1 {
		tx.Amount = outAmts[0]
	} else if leg.sendOutSats > 0 {
		tx.Amount = satsToAmount(leg.sendOutSats, decimals)
	}
	tx.Fees = "0"
}

func externalOutboundLegs(toAddr, toAmt []string, payerAddr string) ([]string, []string) {
	payerAddr = normalizeScanAddress(payerAddr)
	outAddrs := make([]string, 0, len(toAddr))
	outAmts := make([]string, 0, len(toAmt))
	for i, to := range toAddr {
		if i >= len(toAmt) {
			break
		}
		to = normalizeScanAddress(to)
		if to == "" || to == payerAddr {
			continue
		}
		outAddrs = append(outAddrs, to)
		outAmts = append(outAmts, toAmt[i])
	}
	return outAddrs, outAmts
}

// computePayerLegFees returns payer legs that emit a fee row (fee > 0).
func computePayerLegFees(trx *models.Transaction, items []*types.ExtractDataItem, decimals int32) []btcPayerLeg {
	legs := computePayerLegAccounting(trx, items, decimals)
	if len(legs) == 0 {
		return nil
	}
	out := make([]btcPayerLeg, 0, len(legs))
	for _, leg := range legs {
		if leg.feeSats > 0 {
			out = append(out, leg)
		}
	}
	return out
}
