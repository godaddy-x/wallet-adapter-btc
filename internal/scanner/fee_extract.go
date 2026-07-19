package scanner

import (
	"fmt"
	"sort"
	"strings"

	adaptscanner "github.com/godaddy-x/wallet-adapter/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/shopspring/decimal"
)

// btcFeeOutputIndex marks a separate miner-fee extract, aligned with ETH fee rows (OutputIndex=-2).
const btcFeeOutputIndex int64 = -2

// btcNonFeeLegFees is the canonical fees field on non-fee extract legs (never empty string).
const btcNonFeeLegFees = "0"

// feeAccountingSatTolerance allows 1-sat rounding drift across many-input summaries.
const feeAccountingSatTolerance int64 = 1

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

// btcFeeAccountingContext carries optional business trade order lookup (one Mongo query per tx).
type btcFeeAccountingContext struct {
	lookup    adaptscanner.TradeOrderOutboundQuerier
	symbol    string
	accountID string
}

func mulDivSats(a, b, divisor int64) int64 {
	if divisor == 0 || a == 0 || b == 0 {
		return 0
	}
	prod := decimal.NewFromInt(a).Mul(decimal.NewFromInt(b))
	quo := prod.Div(decimal.NewFromInt(divisor)).Truncate(0)
	return quo.IntPart()
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

func amountStringToSats(amount string, decimals int32) (int64, bool) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return 0, false
	}
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return 0, false
	}
	return decimalToSats(d, decimals)
}

func amountStringMatchesSats(amount string, sats int64, decimals int32) bool {
	got, ok := amountStringToSats(amount, decimals)
	return ok && got == sats
}

func resolveFeeAccountingAccountID(items []*types.ExtractDataItem) string {
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.SourceKey) == "" {
			continue
		}
		if item.Data == nil || item.Data[0] == nil || item.Data[0].Transaction == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx.TxAction == "fee" || tx.TxAction == "receive" {
			continue
		}
		if len(tx.FromAddr) == 0 {
			continue
		}
		return strings.TrimSpace(item.SourceKey)
	}
	return ""
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

func chainVinSatsByPayer(trx *models.Transaction, payers map[string]struct{}, decimals int32) (map[string]int64, bool) {
	if trx == nil {
		return nil, false
	}
	out := make(map[string]int64, len(payers))
	for _, in := range trx.Vins {
		if len(in.Coinbase) > 0 {
			continue
		}
		addr := normalizeScanAddress(in.Addr)
		if addr == "" {
			continue
		}
		if _, ok := payers[addr]; !ok {
			continue
		}
		if !validExtractAmount(in.Value) {
			return nil, false
		}
		amount, err := decimal.NewFromString(in.Value)
		if err != nil {
			return nil, false
		}
		sats, ok := decimalToSats(amount, decimals)
		if !ok || sats <= 0 {
			return nil, false
		}
		out[addr] += sats
	}
	for payer := range payers {
		if out[payer] <= 0 {
			return nil, false
		}
	}
	return out, true
}

func chainSelfChangeSats(voutSats, vinSats int64) int64 {
	if voutSats <= 0 || voutSats > vinSats {
		return 0
	}
	return voutSats
}

func chainSelfChangeByPayer(trx *models.Transaction, payers map[string]struct{}, vin map[string]int64, decimals int32) map[string]int64 {
	out := make(map[string]int64, len(payers))
	for payer := range payers {
		out[payer] = 0
	}
	if trx == nil {
		return out
	}
	for _, vout := range trx.Vouts {
		if vout.Type == "OP_RETURN" {
			continue
		}
		if !validExtractAmount(vout.Value) {
			continue
		}
		addr := normalizeScanAddress(vout.Addr)
		if addr == "" {
			continue
		}
		if _, ok := payers[addr]; !ok {
			continue
		}
		amount, err := decimal.NewFromString(vout.Value)
		if err != nil {
			continue
		}
		sats, ok := decimalToSats(amount, decimals)
		if !ok || sats <= 0 {
			continue
		}
		out[addr] += chainSelfChangeSats(sats, vin[addr])
	}
	return out
}

// chainHasIllegalPeerIn reports vouts to a managed payer that exceed self-change.
// Change must only return to the same payer; sibling credits require a separate tx.
func chainHasIllegalPeerIn(trx *models.Transaction, payers map[string]struct{}, decimals int32) bool {
	vin, ok := chainVinSatsByPayer(trx, payers, decimals)
	if !ok {
		return true
	}
	selfChange := chainSelfChangeByPayer(trx, payers, vin, decimals)
	if trx == nil {
		return false
	}
	for _, vout := range trx.Vouts {
		if vout.Type == "OP_RETURN" || !validExtractAmount(vout.Value) {
			continue
		}
		addr := normalizeScanAddress(vout.Addr)
		if addr == "" {
			continue
		}
		if _, isPayer := payers[addr]; !isPayer {
			continue
		}
		amount, err := decimal.NewFromString(vout.Value)
		if err != nil {
			continue
		}
		sats, ok := decimalToSats(amount, decimals)
		if !ok || sats <= 0 {
			continue
		}
		if sats-selfChange[addr] > 0 {
			return true
		}
	}
	return false
}
func chainExternalOutSatsExcludingPayers(trx *models.Transaction, payerSet map[string]int64, decimals int32) int64 {
	if trx == nil {
		return 0
	}
	total := int64(0)
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
		if _, isPayer := payerSet[addr]; isPayer {
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
		total += sats
	}
	return total
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
		if !ok {
			voutSats = 0
		}
		if vinSats <= voutSats {
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

func preparePayerLegNetOut(legs []btcPayerLeg) bool {
	totalNetOutSats := int64(0)
	for i := range legs {
		legs[i].netOutSats = legs[i].vinSats - legs[i].voutSats
		if legs[i].netOutSats < 0 {
			return false
		}
		totalNetOutSats += legs[i].netOutSats
	}
	return totalNetOutSats > 0
}

func indexExtractLegsByPayer(items []*types.ExtractDataItem, decimals int32) map[string]btcPayerLeg {
	legs := collectBTCPayerLegs(items, decimals)
	out := make(map[string]btcPayerLeg, len(legs))
	for _, leg := range legs {
		payer := normalizeScanAddress(leg.payerAddr)
		if payer == "" {
			continue
		}
		leg.netOutSats = leg.vinSats - leg.voutSats
		out[payer] = leg
	}
	return out
}

func findExtractItemForPayer(items []*types.ExtractDataItem, payer string) (int, string, bool) {
	payer = normalizeScanAddress(payer)
	for i, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.TxAction == "fee" || len(tx.FromAddr) == 0 {
			continue
		}
		if normalizeScanAddress(tx.FromAddr[0]) == payer {
			return i, item.SourceKey, true
		}
	}
	return 0, "", false
}

func findAnyOutboundExtractItem(items []*types.ExtractDataItem) (int, string, bool) {
	for i, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.TxAction == "fee" || len(tx.FromAddr) == 0 {
			continue
		}
		return i, item.SourceKey, true
	}
	return 0, "", false
}

// buildMultiPayerBusinessLegs resolves per-payer netOut using chain vin/self-change and business sendOut.
func buildMultiPayerBusinessLegs(
	trx *models.Transaction,
	items []*types.ExtractDataItem,
	snap *types.TradeOrderOutboundSnapshot,
	decimals int32,
	chainFeeSats int64,
	sendOutByPayer map[string]int64,
) ([]btcPayerLeg, bool) {
	if trx == nil || snap == nil || len(snap.Legs) == 0 {
		return nil, false
	}
	payers := make(map[string]struct{}, len(sendOutByPayer))
	for payer := range sendOutByPayer {
		payers[payer] = struct{}{}
	}
	vin, ok := chainVinSatsByPayer(trx, payers, decimals)
	if !ok {
		return nil, false
	}
	if chainHasIllegalPeerIn(trx, payers, decimals) {
		return nil, false
	}
	selfChange := chainSelfChangeByPayer(trx, payers, vin, decimals)
	grossOut := make(map[string]int64, len(payers))
	for payer := range payers {
		grossOut[payer] = vin[payer] - selfChange[payer]
		if grossOut[payer] < 0 {
			return nil, false
		}
	}
	extractByPayer := indexExtractLegsByPayer(items, decimals)
	feeOnlyCount := 0
	for _, sendOutSats := range sendOutByPayer {
		if sendOutSats == 0 {
			feeOnlyCount++
		}
	}
	out := make([]btcPayerLeg, 0, len(snap.Legs))
	for _, bl := range snap.Legs {
		payer := normalizeScanAddress(bl.PayerAddress)
		sendOutSats := sendOutByPayer[payer]
		itemIdx, sourceKey, ok := findExtractItemForPayer(items, payer)
		if !ok {
			itemIdx, sourceKey, ok = findAnyOutboundExtractItem(items)
			if !ok {
				return nil, false
			}
		}
		vinSats := vin[payer]
		voutSats := selfChange[payer]
		if el, ok := extractByPayer[payer]; ok {
			vinSats = el.vinSats
			voutSats = el.voutSats
			itemIdx = el.itemIndex
			sourceKey = el.sourceKey
		}
		g := grossOut[payer]
		var feeSats int64
		switch {
		case sendOutSats == 0:
			// fee-only payer: exactly one such payer on the tx → fee = chainFee
			if feeOnlyCount != 1 || chainFeeSats <= 0 {
				return nil, false
			}
			feeSats = chainFeeSats
		case amountStringMatchesSats(bl.TxFromAmount, sendOutSats, decimals):
			// txFrom_i = sendOut_i → fee_i = 0 (整 UTXO 外送 / dust 业务份额等于 txFrom)
			feeSats = 0
		default:
			if sendOutSats > g {
				return nil, false
			}
			feeSats = g - sendOutSats
		}
		netOutSats := sendOutSats + feeSats
		out = append(out, btcPayerLeg{
			itemIndex:  itemIdx,
			sourceKey:  sourceKey,
			payerAddr:  payer,
			vinSats:    vinSats,
			voutSats:   voutSats,
			netOutSats: netOutSats,
		})
	}
	if len(out) != len(snap.Legs) {
		return nil, false
	}
	return out, true
}

func tryBusinessPayerLegAccounting(trx *models.Transaction, items []*types.ExtractDataItem, extractLegs []btcPayerLeg, decimals int32, acctCtx *btcFeeAccountingContext) ([]btcPayerLeg, bool) {
	if acctCtx == nil || acctCtx.lookup == nil || trx == nil {
		return nil, false
	}
	accountID := strings.TrimSpace(acctCtx.accountID)
	symbol := strings.TrimSpace(acctCtx.symbol)
	if accountID == "" || symbol == "" {
		return nil, false
	}
	snap, err := acctCtx.lookup.GetTradeOrderOutbound(types.TradeOrderOutboundLookupParams{
		TxID:      trx.TxID,
		Symbol:    symbol,
		AccountID: accountID,
	})
	if err != nil || snap == nil || !snap.Found || len(snap.Legs) == 0 {
		return nil, false
	}
	for _, bl := range snap.Legs {
		if strings.TrimSpace(bl.SendOut) == "" {
			return nil, false
		}
	}
	sendOutByPayer := make(map[string]int64, len(snap.Legs))
	for _, bl := range snap.Legs {
		payer := normalizeScanAddress(bl.PayerAddress)
		if payer == "" {
			return nil, false
		}
		sendOutSats, ok := amountStringToSats(bl.SendOut, decimals)
		if !ok || sendOutSats < 0 {
			return nil, false
		}
		sendOutByPayer[payer] = sendOutSats
	}
	payerSet := make(map[string]struct{}, len(sendOutByPayer))
	for payer := range sendOutByPayer {
		payerSet[payer] = struct{}{}
	}
	if chainHasIllegalPeerIn(trx, payerSet, decimals) {
		return nil, false
	}
	multiPayerBusiness := len(snap.Legs) > 1
	chainFeeSats := int64(0)
	if feeTotal, ok := computeBTCTransactionFee(trx); ok {
		var sok bool
		chainFeeSats, sok = decimalToSats(feeTotal, decimals)
		if !sok {
			return nil, false
		}
	}
	legs := extractLegs
	if multiPayerBusiness {
		var ok bool
		legs, ok = buildMultiPayerBusinessLegs(trx, items, snap, decimals, chainFeeSats, sendOutByPayer)
		if !ok {
			return nil, false
		}
	}
	sendOutTotalSats := int64(0)
	for _, sendOutSats := range sendOutByPayer {
		sendOutTotalSats += sendOutSats
	}
	feeTotalSats := int64(0)
	for i := range legs {
		payer := normalizeScanAddress(legs[i].payerAddr)
		sendOutSats, ok := sendOutByPayer[payer]
		if !ok {
			return nil, false
		}
		if sendOutSats > legs[i].netOutSats {
			return nil, false
		}
		feeSats := legs[i].netOutSats - sendOutSats
		legs[i].sendOutSats = sendOutSats
		legs[i].feeSats = feeSats
		feeTotalSats += feeSats
	}
	externalSats := int64(0)
	if multiPayerBusiness {
		externalSats = chainExternalOutSatsExcludingPayers(trx, sendOutByPayer, decimals)
	} else {
		targets := collectOutboundVoutTargets(trx, legs, decimals)
		for _, target := range targets {
			externalSats += target.sats
		}
	}
	if externalSats > 0 {
		sendDiff := sendOutTotalSats - externalSats
		if sendDiff < 0 {
			sendDiff = -sendDiff
		}
		if sendDiff > feeAccountingSatTolerance {
			return nil, false
		}
	}
	if chainFeeSats > 0 || feeTotalSats > 0 {
		feeDiff := feeTotalSats - chainFeeSats
		if feeDiff < 0 {
			feeDiff = -feeDiff
		}
		if feeDiff > feeAccountingSatTolerance {
			return nil, false
		}
	}
	return legs, true
}

// computePayerLegAccounting derives sendOut/fee per payer from business trade order snapshot.
func computePayerLegAccounting(trx *models.Transaction, items []*types.ExtractDataItem, decimals int32, acctCtx *btcFeeAccountingContext) []btcPayerLeg {
	extractLegs := collectBTCPayerLegs(items, decimals)
	if len(extractLegs) == 0 {
		return nil
	}
	if !preparePayerLegNetOut(extractLegs) {
		return nil
	}
	if business, ok := tryBusinessPayerLegAccounting(trx, items, extractLegs, decimals, acctCtx); ok {
		return business
	}
	return nil
}

func collectOutboundVoutTargets(trx *models.Transaction, payerLegs []btcPayerLeg, decimals int32) []outboundTarget {
	// Exclude every payer address: self-change vouts are not business external sendOut targets.
	payerSet := make(map[string]struct{}, len(payerLegs))
	for _, leg := range payerLegs {
		if payer := normalizeScanAddress(leg.payerAddr); payer != "" {
			payerSet[payer] = struct{}{}
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
		if _, isPayer := payerSet[addr]; isPayer {
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
				part = mulDivSats(leg.sendOutSats, target.sats, totalTargetSats)
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

func btcVoutOnlyInboundLeg(tx *types.Transaction) bool {
	if tx == nil || len(tx.FromAddr) > 0 {
		return false
	}
	if tx.TxAction == "receive" {
		return len(tx.ToAddr) > 0
	}
	return tx.TxAction == "internal" && len(tx.ToAddr) > 0
}

func filterPayerSelfChangeReceive(items []*types.ExtractDataItem, decimals int32) []*types.ExtractDataItem {
	// Payers on this tx must not get a second receive credit for self-change / cancel change.
	payers := make(map[string]map[string]struct{})
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || len(tx.FromAddr) == 0 {
			continue
		}
		action := tx.TxAction
		if action != "send" && action != "internal" && action != "fee" {
			continue
		}
		if payers[item.SourceKey] == nil {
			payers[item.SourceKey] = make(map[string]struct{})
		}
		for _, from := range tx.FromAddr {
			if a := normalizeScanAddress(from); a != "" {
				payers[item.SourceKey][a] = struct{}{}
			}
		}
	}

	out := make([]*types.ExtractDataItem, 0, len(items))
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			out = append(out, item)
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil {
			out = append(out, item)
			continue
		}
		if tx.TxAction != "receive" && !btcVoutOnlyInboundLeg(tx) {
			out = append(out, item)
			continue
		}
		if len(tx.ToAddr) == 0 {
			out = append(out, item)
			continue
		}
		recvAddr := normalizeScanAddress(tx.ToAddr[0])
		if _, isPayer := payers[item.SourceKey][recvAddr]; isPayer {
			continue
		}
		out = append(out, item)
	}
	return out
}

func samePayerFromAddrs(fromAddrs []string) (string, bool) {
	if len(fromAddrs) == 0 {
		return "", false
	}
	payer := normalizeScanAddress(fromAddrs[0])
	if payer == "" {
		return "", false
	}
	for _, addr := range fromAddrs[1:] {
		if normalizeScanAddress(addr) != payer {
			return "", false
		}
	}
	return payer, true
}

// dedupeBTCPayerOutboundExtractItems drops per-UTXO send/internal rows when a net payer row exists.
func dedupeBTCPayerOutboundExtractItems(items []*types.ExtractDataItem) []*types.ExtractDataItem {
	hasNetOutbound := make(map[string]struct{})
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || len(tx.FromAddr) == 0 {
			continue
		}
		if tx.TxAction != "send" && tx.TxAction != "internal" {
			continue
		}
		if tx.OutputIndex != btcAddressNetOutputIndex {
			continue
		}
		payer, ok := samePayerFromAddrs(tx.FromAddr)
		if !ok {
			continue
		}
		hasNetOutbound[item.SourceKey+"\x00"+payer] = struct{}{}
	}
	out := make([]*types.ExtractDataItem, 0, len(items))
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			out = append(out, item)
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.OutputIndex == btcAddressNetOutputIndex {
			out = append(out, item)
			continue
		}
		if tx.TxAction != "send" && tx.TxAction != "internal" {
			out = append(out, item)
			continue
		}
		if len(tx.FromAddr) == 0 {
			out = append(out, item)
			continue
		}
		payer, ok := samePayerFromAddrs(tx.FromAddr)
		if !ok {
			out = append(out, item)
			continue
		}
		if _, exists := hasNetOutbound[item.SourceKey+"\x00"+payer]; exists {
			continue
		}
		out = append(out, item)
	}
	return out
}

func requiresBusinessPayerAccounting(items []*types.ExtractDataItem, decimals int32) bool {
	legs := collectBTCPayerLegs(items, decimals)
	if len(legs) == 0 {
		return false
	}
	return preparePayerLegNetOut(legs)
}

func diagnoseBusinessPayerAccountingFailure(
	trx *models.Transaction,
	items []*types.ExtractDataItem,
	decimals int32,
	acctCtx *btcFeeAccountingContext,
) string {
	if trx == nil {
		return "no transaction"
	}
	extractLegs := collectBTCPayerLegs(items, decimals)
	if len(extractLegs) == 0 {
		return "no payer extract legs"
	}
	if !preparePayerLegNetOut(extractLegs) {
		return "payer leg netOut invalid"
	}
	if acctCtx != nil && acctCtx.lookup != nil {
		accountID := strings.TrimSpace(acctCtx.accountID)
		symbol := strings.TrimSpace(acctCtx.symbol)
		if accountID != "" && symbol != "" {
			snap, err := acctCtx.lookup.GetTradeOrderOutbound(types.TradeOrderOutboundLookupParams{
				TxID: trx.TxID, Symbol: symbol, AccountID: accountID,
			})
			if err != nil {
				return "trade order lookup error: " + err.Error()
			}
			if snap == nil || !snap.Found || len(snap.Legs) == 0 {
				return "trade order outbound snapshot missing"
			}
		}
	}
	payers := chainPayersFromAllVins(trx, decimals)
	if len(payers) > 0 && chainHasIllegalPeerIn(trx, payers, decimals) {
		return "illegal peerIn vout: change must return to same payer only (sibling credit requires separate tx)"
	}
	return "sendOut/fee reconciliation failed (check trade order legs vs chain vouts)"
}

func chainPayersFromAllVins(trx *models.Transaction, decimals int32) map[string]struct{} {
	out := make(map[string]struct{})
	if trx == nil {
		return out
	}
	for _, in := range trx.Vins {
		if len(in.Coinbase) > 0 {
			continue
		}
		addr := normalizeScanAddress(in.Addr)
		if addr == "" || !validExtractAmount(in.Value) {
			continue
		}
		out[addr] = struct{}{}
	}
	return out
}

func appendBTCTransactionFeeItems(
	trx *models.Transaction,
	items *[]*types.ExtractDataItem,
	coin types.Coin,
	createAt int64,
	decimals int32,
	acctCtx *btcFeeAccountingContext,
) error {
	if requiresBusinessPayerAccounting(*items, decimals) {
		legs := computePayerLegAccounting(trx, *items, decimals, acctCtx)
		if len(legs) == 0 {
			txid := ""
			if trx != nil {
				txid = trx.TxID
			}
			detail := diagnoseBusinessPayerAccountingFailure(trx, *items, decimals, acctCtx)
			return fmt.Errorf("business payer accounting failed: txid=%s: %s", txid, detail)
		}
	}
	legs := computePayerLegAccounting(trx, *items, decimals, acctCtx)
	if len(legs) > 0 {
		targets := collectOutboundVoutTargets(trx, legs, decimals)
		attachSendOutTargets(*items, legs, targets, decimals)

		for _, leg := range legs {
			if leg.sendOutSats <= 0 && leg.feeSats <= 0 {
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
	}
	collapseSamePayerFromLegs(*items, decimals)
	*items = dedupeBTCPayerOutboundExtractItems(*items)
	*items = filterPayerSelfChangeReceive(*items, decimals)
	return nil
}

func btcFeeOutputIndexForLeg(legIndex int) int64 {
	return btcFeeOutputIndex - int64(legIndex*2)
}

// collapseSamePayerFromLegs merges multiple from legs that share one payer address.
// Sums FromAmt (balance sendOut after fee_extract normalization).
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
		fromSats, ok := decimalToSats(vinSum, decimals)
		if !ok || fromSats < 0 {
			continue
		}
		tx.FromAddr = []string{payer}
		tx.FromAmt = []string{satsToAmount(fromSats, decimals)}
	}
}

// normalizePayerOutboundLeg sets balance-ready From/To (same semantics as ETH extract rows).
// FromAmt = sendOut; To = external recipients only; fee on separate fee row; no self-change in To.
func normalizePayerOutboundLeg(tx *types.Transaction, leg btcPayerLeg, decimals int32) {
	if tx == nil || leg.payerAddr == "" {
		return
	}
	payer := normalizeScanAddress(leg.payerAddr)
	tx.FromAddr = []string{payer}
	if leg.sendOutSats > 0 {
		tx.FromAmt = []string{satsToAmount(leg.sendOutSats, decimals)}
	} else {
		tx.FromAmt = []string{"0"}
	}

	outAddrs, outAmts := externalOutboundLegs(tx.ToAddr, tx.ToAmt, payer)
	tx.ToAddr = outAddrs
	tx.ToAmt = outAmts
	if leg.sendOutSats > 0 {
		tx.Amount = satsToAmount(leg.sendOutSats, decimals)
	} else if len(outAmts) == 1 {
		tx.Amount = outAmts[0]
	}
	tx.Fees = btcNonFeeLegFees
	tx.OutputIndex = btcAddressNetOutputIndex
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
