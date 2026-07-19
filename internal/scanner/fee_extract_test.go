package scanner

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

func testFeeAcctCtx(payerSendOut map[string]string) *btcFeeAccountingContext {
	return &btcFeeAccountingContext{
		lookup: &stubTradeOrderLookup{snap: &types.TradeOrderOutboundSnapshot{
			Found: true,
			Legs:  payerSendOutLegs(payerSendOut),
		}},
		symbol:    "BTC",
		accountID: "account-1",
	}
}

func payerSendOutLegs(payerSendOut map[string]string) []types.TradeOrderPayerLeg {
	legs := make([]types.TradeOrderPayerLeg, 0, len(payerSendOut))
	for addr, amt := range payerSendOut {
		legs = append(legs, types.TradeOrderPayerLeg{PayerAddress: addr, SendOut: amt})
	}
	return legs
}

func mustAppendBTCTransactionFeeItems(t *testing.T, trx *models.Transaction, items *[]*types.ExtractDataItem, acctCtx *btcFeeAccountingContext) {
	t.Helper()
	if err := appendBTCTransactionFeeItems(trx, items, types.Coin{Symbol: "BTC"}, 0, 8, acctCtx); err != nil {
		t.Fatal(err)
	}
}

func wireTestTradeOrderLookup(bs *BtcBlockScanner, byTx map[string]map[string]string) {
	_ = bs.SetTradeOrderLookup(&stubTradeOrderLookup{byTx: byTx})
}

func hasTransactionFeeItem(items []*types.ExtractDataItem) bool {
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx != nil && tx.TxAction == "fee" {
			return true
		}
	}
	return false
}

func computePayerLegFees(trx *models.Transaction, items []*types.ExtractDataItem, decimals int32, acctCtx *btcFeeAccountingContext) []btcPayerLeg {
	legs := computePayerLegAccounting(trx, items, decimals, acctCtx)
	out := make([]btcPayerLeg, 0, len(legs))
	for _, leg := range legs {
		if leg.feeSats > 0 {
			out = append(out, leg)
		}
	}
	return out
}

func TestComputeBTCTransactionFee(t *testing.T) {
	trx := &models.Transaction{
		Vins:  []*models.Vin{{Value: "1.0"}},
		Vouts: []*models.Vout{{Value: "0.9"}, {Value: "0.09989"}},
	}
	fee, ok := computeBTCTransactionFee(trx)
	if !ok {
		t.Fatal("expected fee")
	}
	if fee.String() != "0.00011" {
		t.Fatalf("fee = %s, want 0.00011", fee)
	}
}

func TestComputeBTCTransactionFeeSkipsCoinbase(t *testing.T) {
	trx := &models.Transaction{
		Vins:  []*models.Vin{{Coinbase: "01"}},
		Vouts: []*models.Vout{{Value: "50"}},
	}
	if _, ok := computeBTCTransactionFee(trx); ok {
		t.Fatal("coinbase tx should not emit fee")
	}
}

func TestAppendBTCTransactionFeeItems(t *testing.T) {
	trx := &models.Transaction{
		TxID:          "fee-tx",
		BlockHash:     "hash",
		BlockHeight:   10,
		Confirmations: 1,
		Vins:          []*models.Vin{{N: 0, Addr: "bcrt1qpayer", Value: "1.0"}},
		Vouts: []*models.Vout{
			{N: 0, Addr: "bcrt1qexternal", Value: "0.9"},
			{N: 1, Addr: "bcrt1qpayer", Value: "0.09989"},
		},
	}
	sendTx := &types.Transaction{
		TxID:        "fee-tx",
		FromAddr:    []string{"bcrt1qpayer"},
		FromAmt:     []string{"1.0"},
		ToAddr:      []string{"bcrt1qpayer"},
		ToAmt:       []string{"0.09989"},
		TxAction:    "send",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{{
		SourceKey: "account-1",
		Data:      []*types.TxExtractData{{Transaction: sendTx}},
	}}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{"bcrt1qpayer": "0.9"}))
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (1 send + 1 fee)", len(items))
	}
	if sendTx.ToAmt[0] != "0.9" {
		t.Fatalf("send ToAmt = %v, want [0.9]", sendTx.ToAmt)
	}
	if sendTx.ToAddr[0] != "bcrt1qexternal" {
		t.Fatalf("send ToAddr = %v, want external only", sendTx.ToAddr)
	}
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != "0.9" {
		t.Fatalf("send From = %v %v, want sendOut [0.9]", sendTx.FromAddr, sendTx.FromAmt)
	}
	feeTx := items[1].Data[0].Transaction
	if feeTx.Fees != "0.00011" {
		t.Fatalf("fee = %q, want 0.00011", feeTx.Fees)
	}
	assertFeeSharesSumToTotal(t, trx, items, 8)
}

func TestAppendBTCTransactionFeeItemsDirectFromInputOutput(t *testing.T) {
	addrA := "bcrt1qpayer-a"
	addrB := "bcrt1qpayer-b"
	trx := &models.Transaction{
		TxID:          "multi-fee-tx",
		BlockHash:     "hash",
		BlockHeight:   10,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: addrA, Value: "0.6"},
			{N: 1, Addr: addrB, Value: "0.4"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: "bcrt1qexternal", Value: "0.9"},
			{N: 1, Addr: addrA, Value: "0.09989"},
		},
	}
	sendA := &types.Transaction{
		TxID:        "multi-fee-tx",
		FromAddr:    []string{addrA},
		FromAmt:     []string{"0.6"},
		ToAddr:      []string{addrA},
		ToAmt:       []string{"0.09989"},
		TxAction:    "send",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	sendB := &types.Transaction{
		TxID:        "multi-fee-tx",
		FromAddr:    []string{addrB},
		FromAmt:     []string{"0.4"},
		TxAction:    "send",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: sendA}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: sendB}}},
	}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{
		addrA: "0.50004889",
		addrB: "0.39995111",
	}))
	if len(items) != 4 {
		t.Fatalf("len(items) = %d, want 4 (2 send + 2 fee legs)", len(items))
	}
	if len(sendA.ToAddr) != 1 || sendA.ToAmt[0] != "0.50004889" {
		t.Fatalf("sendA = ToAddr:%v ToAmt:%v", sendA.ToAddr, sendA.ToAmt)
	}
	if sendA.ToAddr[0] != "bcrt1qexternal" {
		t.Fatalf("sendA targets = %v", sendA.ToAddr)
	}
	if len(sendB.ToAddr) != 1 || sendB.ToAddr[0] != "bcrt1qexternal" || sendB.ToAmt[0] != "0.39995111" {
		t.Fatalf("sendB = ToAddr:%v ToAmt:%v", sendB.ToAddr, sendB.ToAmt)
	}
	feeTotal := decimal.Zero
	for _, item := range items {
		tx := item.Data[0].Transaction
		if tx.TxAction != "fee" {
			continue
		}
		f, err := decimal.NewFromString(tx.Fees)
		if err != nil {
			t.Fatal(err)
		}
		feeTotal = feeTotal.Add(f)
	}
	if !feeTotal.Equal(decimal.RequireFromString("0.00011")) {
		t.Fatalf("fee total = %s want 0.00011", feeTotal)
	}
	assertFeeSharesSumToTotal(t, trx, items, 8)

	acctCtx := testFeeAcctCtx(map[string]string{
		addrA: "0.50004889",
		addrB: "0.39995111",
	})
	legs := computePayerLegFees(trx, []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrA}, FromAmt: []string{"0.6"},
			ToAddr: []string{addrA}, ToAmt: []string{"0.09989"},
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrB}, FromAmt: []string{"0.4"},
		}}}},
	}, 8, acctCtx)
	if len(legs) != 2 {
		t.Fatalf("fee legs = %d, want 2", len(legs))
	}
	feeSum := int64(0)
	for _, leg := range legs {
		feeSum += leg.feeSats
	}
	if feeSum != 11000 {
		t.Fatalf("fee sum = %d want 11000", feeSum)
	}
}

func TestComputePayerLegFeesVinOnlyLegSendEqualsVin(t *testing.T) {
	addrA := "bcrt1qpayer-a"
	addrB := "bcrt1qpayer-b"
	trx := &models.Transaction{
		Vins: []*models.Vin{
			{Addr: addrA, Value: "0.6"},
			{Addr: addrB, Value: "0.4"},
		},
		Vouts: []*models.Vout{
			{Addr: "bcrt1qexternal", Value: "0.9"},
			{Addr: addrA, Value: "0.09989"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrA}, FromAmt: []string{"0.6"},
			ToAddr: []string{addrA}, ToAmt: []string{"0.09989"},
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrB}, FromAmt: []string{"0.4"},
		}}}},
	}
	acctCtx := testFeeAcctCtx(map[string]string{
		addrA: "0.50004889",
		addrB: "0.39995111",
	})
	allLegs := collectBTCPayerLegs(items, 8)
	if len(allLegs) != 2 {
		t.Fatalf("payer legs = %d, want 2", len(allLegs))
	}
	feeLegs := computePayerLegFees(trx, items, 8, acctCtx)
	if len(feeLegs) != 2 {
		t.Fatalf("fee legs = %+v", feeLegs)
	}
	feeSum := int64(0)
	for _, leg := range feeLegs {
		feeSum += leg.feeSats
	}
	if feeSum != 11000 {
		t.Fatalf("fee sum = %d want 11000", feeSum)
	}
	for _, leg := range allLegs {
		if leg.payerAddr == addrB && leg.vinSats != 40000000 {
			t.Fatalf("B vin = %d", leg.vinSats)
		}
	}
}

func TestAppendBTCTransactionFeeItemsInternalTransfer(t *testing.T) {
	addrA := "bcrt1qpayer-a"
	addrB := "bcrt1qpeer-b"
	trx := &models.Transaction{
		TxID:          "internal-ab",
		BlockHash:     "hash",
		BlockHeight:   100,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: addrA, Value: "1.0"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: addrB, Value: "0.0001"},
			{N: 1, Addr: addrA, Value: "0.99989"},
		},
	}
	internalTx := &types.Transaction{
		TxID:        "internal-ab",
		FromAddr:    []string{addrA},
		FromAmt:     []string{"1.0"},
		ToAddr:      []string{addrA},
		ToAmt:       []string{"0.99989"},
		TxAction:    "internal",
		OutputIndex: -3,
		Decimal:     8,
	}
	receiveTx := &types.Transaction{
		TxID:        "internal-ab",
		ToAddr:      []string{addrB},
		ToAmt:       []string{"0.0001"},
		Amount:      "0.0001",
		TxAction:    "receive",
		OutputIndex: 0,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: internalTx}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: receiveTx}}},
	}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{addrA: "0.0001"}))
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3 (internal + fee + peer receive)", len(items))
	}
	out := internalTx
	if out.TxAction != "internal" {
		t.Fatalf("TxAction = %q, want internal", out.TxAction)
	}
	if out.Amount != "0.0001" {
		t.Fatalf("Amount = %q, want 0.0001", out.Amount)
	}
	if out.Fees != "0" {
		t.Fatalf("Fees = %q, want 0 on transfer leg", out.Fees)
	}
	if len(out.FromAddr) != 1 || out.FromAmt[0] != "0.0001" {
		t.Fatalf("From = %v %v, want sendOut 0.0001", out.FromAddr, out.FromAmt)
	}
	if len(out.ToAddr) != 1 || out.ToAddr[0] != addrB {
		t.Fatalf("ToAddr = %v, want external peer only", out.ToAddr)
	}
	if out.ToAmt[0] != "0.0001" {
		t.Fatalf("ToAmt = %v", out.ToAmt)
	}
	var feeTx *types.Transaction
	var keptReceive bool
	for _, item := range items {
		tx := item.Data[0].Transaction
		if tx.TxAction == "fee" {
			feeTx = tx
		}
		if tx.TxAction == "receive" && normalizeScanAddress(tx.ToAddr[0]) == addrB {
			keptReceive = true
		}
	}
	if feeTx == nil || feeTx.Fees != "0.00001" {
		t.Fatalf("fee row = %+v", feeTx)
	}
	if !keptReceive {
		t.Fatal("peer receive row must remain for balance credit")
	}
}

func TestFilterReceiveItemsCoveredBySendMultiPayerFundPartialCredit(t *testing.T) {
	const dec = int32(8)
	dust := "bcrt1qdustfund000000000000000000000"
	main := "bcrt1qmainfund000000000000000000000"
	fund := "bcrt1qfundrecv000000000000000000000"
	txid := "multi-payer-fund-tx"

	dustOut := &types.Transaction{
		TxID: txid, TxAction: "internal", Decimal: 8,
		FromAddr: []string{dust},
		FromAmt:  []string{"0.0000369"},
		ToAddr:   []string{fund},
		ToAmt:    []string{"0.00000001"},
	}
	mainOut := &types.Transaction{
		TxID: txid, TxAction: "internal", Decimal: 8,
		FromAddr: []string{main},
		FromAmt:  []string{"24.96694326"},
		ToAddr:   []string{dust, fund},
		ToAmt:    []string{"24.96397016", "0.00000036"},
	}
	fundIn := &types.Transaction{
		TxID: txid, TxAction: "internal", Decimal: 8,
		ToAddr:  []string{fund},
		ToAmt:   []string{"0.003"},
		Amount:  "0.003",
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: dustOut}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: mainOut}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: fundIn}}},
	}
	filtered := filterPayerSelfChangeReceive(items, dec)
	var keptFund bool
	for _, item := range filtered {
		tx := item.Data[0].Transaction
		if tx.TxAction == "internal" && len(tx.FromAddr) == 0 && normalizeScanAddress(tx.ToAddr[0]) == fund {
			keptFund = true
			if tx.ToAmt[0] != "0.003" {
				t.Fatalf("fund inbound toAmt=%q want 0.003", tx.ToAmt[0])
			}
		}
	}
	if !keptFund {
		t.Fatal("expected fund vout-only inbound leg to remain when outbound credits only partially cover")
	}
}

func TestFilterReceiveDropsCancelSelfChange(t *testing.T) {
	// Regtest 2bf8f484…: fee-only cancel left orphan receive of change ≈50 BTC.
	// Payer already holds the vin; change receive must be dropped.
	dec := int32(8)
	main := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	txid := "2bf8f4849b7bb6e7249a76ad8647324e84e52bcb7f32a29878091552b71c272d"
	fee := &types.Transaction{
		TxID: txid, TxAction: "fee", Decimal: dec,
		FromAddr: []string{main}, FromAmt: []string{"0.0000111"},
		Fees: "0.0000111", OutputIndex: btcFeeOutputIndex,
	}
	changeIn := &types.Transaction{
		TxID: txid, TxAction: "receive", Decimal: dec,
		ToAddr: []string{main}, ToAmt: []string{"49.9999889"}, Amount: "49.9999889",
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: fee}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: changeIn}}},
	}
	filtered := filterPayerSelfChangeReceive(items, dec)
	for _, item := range filtered {
		tx := item.Data[0].Transaction
		if btcVoutOnlyInboundLeg(tx) && normalizeScanAddress(tx.ToAddr[0]) == normalizeScanAddress(main) {
			t.Fatalf("cancel self-change receive must be dropped when fee payer matches")
		}
	}
	if len(filtered) != 1 || filtered[0].Data[0].Transaction.TxAction != "fee" {
		t.Fatalf("want only fee leg, got %d items", len(filtered))
	}
}

func TestAppendBTCTransactionFeeItemsSummaryVinOnlyLeg(t *testing.T) {
	addr := "bcrt1qpayer"
	external := "bcrt1qexternal"
	trx := &models.Transaction{
		TxID:          "summary-tx",
		BlockHash:     "hash",
		BlockHeight:   166,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: addr, Value: "50"},
			{N: 1, Addr: addr, Value: "50"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "99.99998"},
		},
	}
	sendTx := &types.Transaction{
		TxID:        "summary-tx",
		FromAddr:    []string{addr, addr},
		FromAmt:     []string{"50", "50"},
		TxAction:    "send",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{{
		SourceKey: "account-1",
		Data:      []*types.TxExtractData{{Transaction: sendTx}},
	}}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{addr: "99.99998"}))
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (send + fee)", len(items))
	}
	if len(sendTx.ToAddr) != 1 || sendTx.ToAddr[0] != external {
		t.Fatalf("send ToAddr = %v, want [%s]", sendTx.ToAddr, external)
	}
	if sendTx.ToAmt[0] != "99.99998" {
		t.Fatalf("send ToAmt = %v", sendTx.ToAmt)
	}
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != "99.99998" {
		t.Fatalf("send From = %v %v, want sendOut 99.99998", sendTx.FromAddr, sendTx.FromAmt)
	}
	feeTx := items[1].Data[0].Transaction
	if feeTx.TxAction != "fee" || feeTx.Fees != "0.00002" {
		t.Fatalf("fee row = action:%q fees:%q", feeTx.TxAction, feeTx.Fees)
	}
}

func TestAppendBTCTransactionFeeItemsLargeSinglePayerSummary(t *testing.T) {
	addr := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	external := "bcrt1qrya899pyx3pwu8xeu0am037a260hr7s2w5utqh"
	const (
		utxoCount  = 101
		utxoAmount = "6.25"
		lastUTXO   = "6.24954326"
		fee        = "0.00001"
	)
	vinSum := decimal.RequireFromString("631.24954326")
	outSum := vinSum.Sub(decimal.RequireFromString(fee))

	trx := &models.Transaction{
		TxID:          "large-summary",
		BlockHash:     "hash",
		BlockHeight:   200,
		Confirmations: 1,
		Vins:          make([]*models.Vin, 0, utxoCount),
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: outSum.String()},
		},
	}
	fromAddrs := make([]string, 0, utxoCount)
	fromAmts := make([]string, 0, utxoCount)
	for i := 0; i < utxoCount; i++ {
		amt := utxoAmount
		if i == utxoCount-1 {
			amt = lastUTXO
		}
		trx.Vins = append(trx.Vins, &models.Vin{N: uint64(i), Addr: addr, Value: amt})
		fromAddrs = append(fromAddrs, addr)
		fromAmts = append(fromAmts, amt)
	}
	sendTx := &types.Transaction{
		TxID:        trx.TxID,
		FromAddr:    fromAddrs,
		FromAmt:     fromAmts,
		TxAction:    "send",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{{
		SourceKey: "account-1",
		Data:      []*types.TxExtractData{{Transaction: sendTx}},
	}}
	wantOut := outSum.String()
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{addr: wantOut}))
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (send + fee)", len(items))
	}
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != wantOut {
		t.Fatalf("From = %v %v, want sendOut %s", sendTx.FromAddr, sendTx.FromAmt, wantOut)
	}
	if len(sendTx.ToAddr) != 1 || sendTx.ToAddr[0] != external {
		t.Fatalf("ToAddr = %v, want [%s]", sendTx.ToAddr, external)
	}
	if len(sendTx.ToAmt) != 1 || sendTx.ToAmt[0] != wantOut {
		t.Fatalf("ToAmt = %v, want [%s]", sendTx.ToAmt, wantOut)
	}
	feeTx := items[1].Data[0].Transaction
	if feeTx.TxAction != "fee" || feeTx.Fees != fee {
		t.Fatalf("fee row = action:%q fees:%q want %s", feeTx.TxAction, feeTx.Fees, fee)
	}
}

func TestAppendBTCTransactionFeeItemsSummaryWithChangeBack(t *testing.T) {
	main := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	peer := "bcrt1qg5xunuluw78tv3mh6vvhmpcrpe2gx3ygawzpcu"
	external := "bcrt1qrya899pyx3pwu8xeu0am037a260hr7s2w5utqh"
	const fee = "0.00001"
	mainVin := decimal.RequireFromString("3950")
	change := decimal.RequireFromString("199.89973652")
	peerVin := decimal.RequireFromString("49.96397016")
	externalOut := mainVin.Add(peerVin).Sub(decimal.RequireFromString(fee)).Sub(change)

	trx := &models.Transaction{
		TxID:          "summary-change",
		BlockHash:     "hash",
		BlockHeight:   678,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: main, Value: mainVin.String()},
			{N: 1, Addr: peer, Value: peerVin.String()},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: externalOut.String()},
			{N: 1, Addr: main, Value: change.String()},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{main}, FromAmt: []string{mainVin.String()},
			ToAddr: []string{main}, ToAmt: []string{change.String()},
			TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{peer}, FromAmt: []string{peerVin.String()},
			TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
	}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{
		main: "3750.10025362",
		peer: "49.96397002",
	}))
	sendByPayer := map[string]*types.Transaction{}
	feeSum := decimal.Zero
	for _, item := range items {
		tx := item.Data[0].Transaction
		if len(tx.FromAddr) == 0 {
			continue
		}
		switch tx.TxAction {
		case "send":
			sendByPayer[tx.FromAddr[0]] = tx
		case "fee":
			f, err := decimal.NewFromString(tx.Fees)
			if err != nil {
				t.Fatal(err)
			}
			feeSum = feeSum.Add(f)
		}
	}
	if len(sendByPayer) != 2 {
		t.Fatalf("send rows = %d want 2", len(sendByPayer))
	}
	if !feeSum.Equal(decimal.RequireFromString(fee)) {
		t.Fatalf("fee sum = %s want %s", feeSum, fee)
	}
	mainSend := sendByPayer[main]
	if mainSend == nil || len(mainSend.ToAmt) != 1 {
		t.Fatalf("main send = %+v", mainSend)
	}
	mainOut, err := decimal.NewFromString(mainSend.ToAmt[0])
	if err != nil {
		t.Fatal(err)
	}
	peerSend := sendByPayer[peer]
	if peerSend == nil || len(peerSend.ToAmt) != 1 {
		t.Fatalf("peer send = %+v", peerSend)
	}
	peerOut, err := decimal.NewFromString(peerSend.ToAmt[0])
	if err != nil {
		t.Fatal(err)
	}
	if !peerOut.LessThan(peerVin) {
		t.Fatalf("peer send %s must be less than vin %s (peer also pays fee share)", peerSend.ToAmt[0], peerVin)
	}
	totalSend := mainOut.Add(peerOut)
	if !totalSend.Equal(externalOut) {
		t.Fatalf("send total %s != external out %s", totalSend, externalOut)
	}
	if mainOut.LessThan(decimal.RequireFromString("3700")) {
		t.Fatalf("main send out %s too small; change payer sendOut = netOut - chainFee", mainSend.ToAmt[0])
	}
}

func TestCollapseSamePayerFromLegsWhenFeeAccountingSkipped(t *testing.T) {
	addr := "bcrt1qpayer"
	sendTx := &types.Transaction{
		TxID:        "skip-fee-tx",
		FromAddr:    []string{addr, addr, addr},
		FromAmt:     []string{"1", "1", "1"},
		TxAction:    "send",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{{
		SourceKey: "account-1",
		Data:      []*types.TxExtractData{{Transaction: sendTx}},
	}}
	trx := &models.Transaction{
		TxID:  "skip-fee-tx",
		Vins:  []*models.Vin{{N: 0, Addr: addr, Value: "1"}},
		Vouts: []*models.Vout{{N: 0, Addr: "bcrt1qother", Value: "2.5"}},
	}
	if err := appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8, nil); err == nil {
		t.Fatal("expected business accounting error without trade order sendOut")
	}
	collapseSamePayerFromLegs(items, 8)
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != "3" {
		t.Fatalf("From collapsed = %v %v, want single payer total 3", sendTx.FromAddr, sendTx.FromAmt)
	}
}

func TestAppendBTCTransactionFeeItemsThreePayerSummary(t *testing.T) {
	// Mirrors SummaryMulti production: first payers have txFrom=sendOut (fee=0),
	// last payer absorbs chainFee. Only fee>0 rows are emitted/stored.
	addrA := "bcrt1qpayer-a"
	addrB := "bcrt1qpayer-b"
	addrC := "bcrt1qpayer-c"
	external := "bcrt1qsummary-dest"
	trx := &models.Transaction{
		TxID:          "summary-abc",
		BlockHash:     "hash",
		BlockHeight:   200,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: addrA, Value: "3.125"},
			{N: 1, Addr: addrB, Value: "3.125"},
			{N: 2, Addr: addrC, Value: "3.125"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "9.37499"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{addrA}, FromAmt: []string{"3.125"}, TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{addrB}, FromAmt: []string{"3.125"}, TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{addrC}, FromAmt: []string{"3.125"}, TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
	}
	acctCtx := &btcFeeAccountingContext{
		lookup: &stubTradeOrderLookup{snap: &types.TradeOrderOutboundSnapshot{
			Found: true,
			Legs: []types.TradeOrderPayerLeg{
				{PayerAddress: addrA, SendOut: "3.125", TxFromAmount: "3.125"},
				{PayerAddress: addrB, SendOut: "3.125", TxFromAmount: "3.125"},
				{PayerAddress: addrC, SendOut: "3.12499", TxFromAmount: "3.125"},
			},
		}},
		symbol:    "BTC",
		accountID: "account-1",
	}
	mustAppendBTCTransactionFeeItems(t, trx, &items, acctCtx)
	sendByPayer := map[string]*types.Transaction{}
	feeByPayer := map[string]*types.Transaction{}
	for _, item := range items {
		tx := item.Data[0].Transaction
		if len(tx.FromAddr) == 0 {
			continue
		}
		payer := tx.FromAddr[0]
		switch tx.TxAction {
		case "send":
			sendByPayer[payer] = tx
		case "fee":
			feeByPayer[payer] = tx
		}
	}
	for _, addr := range []string{addrA, addrB, addrC} {
		send := sendByPayer[addr]
		if send == nil {
			t.Fatalf("missing send row for %s", addr)
		}
		if len(send.FromAddr) != 1 || len(send.ToAddr) != 1 || send.ToAddr[0] != external {
			t.Fatalf("send %s: from=%v to=%v", addr, send.FromAddr, send.ToAddr)
		}
	}
	if len(sendByPayer) != 3 {
		t.Fatalf("send rows = %d, want 3", len(sendByPayer))
	}
	if len(feeByPayer) != 1 {
		t.Fatalf("fee rows = %d, want 1 (only fee>0 payer stored)", len(feeByPayer))
	}
	if feeByPayer[addrA] != nil || feeByPayer[addrB] != nil {
		t.Fatalf("A/B must not have fee rows when fee=0")
	}
	if feeByPayer[addrC] == nil || feeByPayer[addrC].Fees != "0.00001" {
		t.Fatalf("C fee=%v want 0.00001", feeByPayer[addrC])
	}
	assertFeeSharesSumToTotal(t, trx, items, 8)
}

func TestComputePayerLegAccountingExactPerAddress(t *testing.T) {
	addrA := "bcrt1qpayer-a"
	addrB := "bcrt1qpayer-b"
	trx := &models.Transaction{
		Vins: []*models.Vin{
			{Addr: addrA, Value: "0.6"},
			{Addr: addrB, Value: "0.4"},
		},
		Vouts: []*models.Vout{
			{Addr: "bcrt1qexternal", Value: "0.9"},
			{Addr: addrA, Value: "0.09989"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrA}, FromAmt: []string{"0.6"},
			ToAddr: []string{addrA}, ToAmt: []string{"0.09989"},
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrB}, FromAmt: []string{"0.4"},
		}}}},
	}
	if legs := computePayerLegAccounting(trx, items, 8, nil); len(legs) != 0 {
		t.Fatalf("expected nil without business sendOut, got %d legs", len(legs))
	}
	acctCtx := testFeeAcctCtx(map[string]string{
		addrA: "0.50004889",
		addrB: "0.39995111",
	})
	legs := computePayerLegAccounting(trx, items, 8, acctCtx)
	if len(legs) != 2 {
		t.Fatalf("legs = %d want 2", len(legs))
	}
	byPayer := map[string]btcPayerLeg{}
	for _, leg := range legs {
		byPayer[leg.payerAddr] = leg
	}
	if byPayer[addrA].sendOutSats != 50004889 {
		t.Fatalf("A sendOut = %d want 50004889", byPayer[addrA].sendOutSats)
	}
	if byPayer[addrB].sendOutSats != 39995111 {
		t.Fatalf("B sendOut = %d want 39995111", byPayer[addrB].sendOutSats)
	}
	feeTotal, ok := computeBTCTransactionFee(trx)
	if !ok {
		t.Fatal("expected chain fee")
	}
	feeTotalSats, ok := decimalToSats(feeTotal, 8)
	if !ok {
		t.Fatal("fee not whole sats")
	}
	sendSum := int64(0)
	feeSum := int64(0)
	for _, payer := range []string{addrA, addrB} {
		leg, ok := byPayer[payer]
		if !ok {
			t.Fatalf("missing leg for %s", payer)
		}
		if leg.sendOutSats+leg.feeSats != leg.netOutSats {
			t.Fatalf("payer %s sendOut(%d)+fee(%d) != netOut(%d)", payer, leg.sendOutSats, leg.feeSats, leg.netOutSats)
		}
		if payer == addrB {
			if leg.feeSats <= 0 || leg.sendOutSats >= leg.netOutSats {
				t.Fatalf("payer B must contribute fee share: fee=%d sendOut=%d netOut=%d", leg.feeSats, leg.sendOutSats, leg.netOutSats)
			}
		}
		if payer == addrA {
			if leg.feeSats <= 0 || leg.sendOutSats >= leg.netOutSats {
				t.Fatalf("payer A must contribute fee share: fee=%d sendOut=%d netOut=%d", leg.feeSats, leg.sendOutSats, leg.netOutSats)
			}
		}
		sendSum += leg.sendOutSats
		feeSum += leg.feeSats
	}
	externalSats, ok := decimalToSats(decimal.RequireFromString("0.9"), 8)
	if !ok {
		t.Fatal("external not whole sats")
	}
	if sendSum != externalSats {
		t.Fatalf("send sum = %d want external %d", sendSum, externalSats)
	}
	if feeSum != feeTotalSats {
		t.Fatalf("fee sum = %d want chain %d", feeSum, feeTotalSats)
	}
}

func TestBusinessMultiPayerDustSpeedUpFeeAccountingRejected(t *testing.T) {
	dust := "bcrt1qg5xunuluw78tv3mh6vvhmpcrpe2gx3ygawzpcu"
	main := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	ext := "bcrt1qja3mzjkarkvw0z3xaa7wwk8k2z84amx3fzcl67"
	trx := &models.Transaction{
		TxID: "83539e276e172ee89516e484ca5b82cac80ce13dc59027466c6b99113a10a588",
		Vins: []*models.Vin{
			{Addr: dust, Value: "0.003"},
			{Addr: main, Value: "49.96694326"},
		},
		Vouts: []*models.Vout{
			{Addr: ext, Value: "0.00300000"},
			{Addr: dust, Value: "49.96693326"},
		},
	}
	items := []*types.ExtractDataItem{{
		SourceKey: "account-1",
		Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID:     trx.TxID,
			TxAction: "send",
			FromAddr: []string{main},
			FromAmt:  []string{"49.96694326"},
			ToAddr:   []string{ext},
			ToAmt:    []string{"0.00300000"},
		}}},
	}}
	acctCtx := &btcFeeAccountingContext{
		lookup: &stubTradeOrderLookup{snap: &types.TradeOrderOutboundSnapshot{
			Found: true,
			Legs: []types.TradeOrderPayerLeg{
				{PayerAddress: dust, SendOut: "0.003", TxFromAmount: "0.003"},
				{PayerAddress: main, SendOut: "0", TxFromAmount: "49.96694326"},
			},
		}},
		symbol:    "BTC",
		accountID: "account-1",
	}
	err := appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8, acctCtx)
	if err == nil {
		t.Fatal("expected fail-stop: sibling peerIn vout violates self-change-only rule")
	}
}

func TestChainHasIllegalPeerIn(t *testing.T) {
	dust := "bcrt1qdust"
	main := "bcrt1qmain"
	trx := &models.Transaction{
		Vins: []*models.Vin{
			{Addr: dust, Value: "0.003"},
			{Addr: main, Value: "1"},
		},
		Vouts: []*models.Vout{
			{Addr: "bcrt1qext", Value: "0.003"},
			{Addr: dust, Value: "0.99999"},
		},
	}
	payers := map[string]struct{}{dust: {}, main: {}}
	if !chainHasIllegalPeerIn(trx, payers, 8) {
		t.Fatal("expected illegal peerIn when large vout goes to sibling payer")
	}
}

func TestBusinessMultiPayerDualSendOutIllegalPeerInRejected(t *testing.T) {
	dust := "bcrt1qg5xunuluw78tv3mh6vvhmpcrpe2gx3ygawzpcu"
	main := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	ext := "bcrt1qja3mzjkarkvw0z3xaa7wwk8k2z84amx3fzcl67"
	trx := &models.Transaction{
		TxID: "0985c7161bace08767a00fc8791ddca7ee618196e01a994416c2874573e346c8",
		Vins: []*models.Vin{
			{Addr: dust, Value: "0.003"},
			{Addr: main, Value: "49.96694326"},
		},
		Vouts: []*models.Vout{
			{Addr: ext, Value: "0.00300000"},
			{Addr: dust, Value: "49.96693326"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, TxAction: "send",
			FromAddr: []string{dust}, FromAmt: []string{"0.003"},
			ToAddr:   []string{ext}, ToAmt: []string{"0.0000369"},
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, TxAction: "send",
			FromAddr: []string{main}, FromAmt: []string{"49.96694326"},
			ToAddr:   []string{ext}, ToAmt: []string{"0.0029631"},
		}}}},
	}
	acctCtx := &btcFeeAccountingContext{
		lookup: &stubTradeOrderLookup{snap: &types.TradeOrderOutboundSnapshot{
			Found: true,
			Legs: []types.TradeOrderPayerLeg{
				{PayerAddress: dust, SendOut: "0.0000369", TxFromAmount: "0.0000369"},
				{PayerAddress: main, SendOut: "0.0029631", TxFromAmount: "49.96694326"},
			},
		}},
		symbol:    "BTC",
		accountID: "account-1",
	}
	err := appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8, acctCtx)
	if err == nil {
		t.Fatal("expected fail-stop for illegal sibling peerIn vout")
	}
}

type stubTradeOrderLookup struct {
	snap  *types.TradeOrderOutboundSnapshot
	byTx  map[string]map[string]string
}

func (s *stubTradeOrderLookup) GetTradeOrderOutbound(p types.TradeOrderOutboundLookupParams) (*types.TradeOrderOutboundSnapshot, error) {
	if s.byTx != nil {
		payers := s.byTx[p.TxID]
		if len(payers) == 0 {
			return &types.TradeOrderOutboundSnapshot{Found: false}, nil
		}
		return &types.TradeOrderOutboundSnapshot{Found: true, Legs: payerSendOutLegs(payers)}, nil
	}
	return s.snap, nil
}

func TestMulDivSatsLargeValuesNoOverflow(t *testing.T) {
	// Regression: int64(a*b) overflows for summary-scale BTC amounts (~4000 BTC vin).
	a := int64(380006422364) // ~3800 BTC external out in sats
	b := int64(375010026348) // ~3750 BTC netOut in sats
	div := int64(380006422380)
	got := mulDivSats(a, b, div)
	// Must be summary-scale (~3750 BTC), not int64-overflow garbage (~0.13 BTC).
	if got < 374_000_000_000 || got > 376_000_000_000 {
		t.Fatalf("mulDivSats = %d, want ~375010026348 sats (overflow yields ~13474755)", got)
	}
}

func TestCollectOutboundVoutTargetsExcludesChangeWithOneSatDrift(t *testing.T) {
	main := "bcrt1qmain"
	external := "bcrt1qext"
	trx := &models.Transaction{
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "100"},
			{N: 1, Addr: main, Value: "0.99999999"}, // chain change 1 sat below extract sum
		},
	}
	legs := []btcPayerLeg{{
		payerAddr: main,
		voutSats:  100000000, // 1.0 BTC from extract ToAmt sum
	}}
	targets := collectOutboundVoutTargets(trx, legs, 8)
	if len(targets) != 1 || targets[0].addr != external {
		t.Fatalf("targets = %+v, want only external out", targets)
	}
}

func TestDecimalToSatsRejectsFractionalSatoshi(t *testing.T) {
	if _, ok := decimalToSats(decimal.RequireFromString("0.000000005"), 8); ok {
		t.Fatal("expected fractional satoshi to be rejected")
	}
}

func assertFeeSharesSumToTotal(t *testing.T, trx *models.Transaction, items []*types.ExtractDataItem, decimals int32) {
	t.Helper()
	feeTotal, ok := computeBTCTransactionFee(trx)
	if !ok {
		t.Fatal("expected fee total")
	}
	feeTotalSats, ok := decimalToSats(feeTotal, decimals)
	if !ok {
		t.Fatalf("fee total not whole satoshis: %s", feeTotal)
	}
	sumSats := int64(0)
	for _, item := range items {
		if item == nil || len(item.Data) == 0 || item.Data[0] == nil {
			continue
		}
		tx := item.Data[0].Transaction
		if tx == nil || tx.TxAction != "fee" {
			continue
		}
		part, err := decimal.NewFromString(tx.Fees)
		if err != nil {
			t.Fatalf("parse fee %q: %v", tx.Fees, err)
		}
		partSats, ok := decimalToSats(part, decimals)
		if !ok {
			t.Fatalf("fee share not whole satoshis: %s", tx.Fees)
		}
		sumSats += partSats
	}
	if sumSats != feeTotalSats {
		t.Fatalf("fee share sum = %d sats, want %d sats", sumSats, feeTotalSats)
	}
}

func TestAppendBTCTransactionFeeItemsTwoPayerInternalRequiresBusiness(t *testing.T) {
	addrA := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	addrB := "bcrt1qg5xunuluw78tv3mh6vvhmpcrpe2gx3ygawzpcu"
	addrC := "bcrt1qja3mzjkarkvw0z3xaa7wwk8k2z84amx3fzcl67"
	trx := &models.Transaction{
		TxID:          "two-payer-internal",
		BlockHash:     "hash",
		BlockHeight:   204,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: addrB, Value: "0.0000369"},
			{N: 1, Addr: addrA, Value: "49.96694326"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: addrC, Value: "0.003"},
			{N: 1, Addr: addrB, Value: "49.96397016"},
		},
	}
	internalA := &types.Transaction{
		TxID:        trx.TxID,
		FromAddr:    []string{addrA},
		FromAmt:     []string{"49.96694326"},
		ToAddr:      []string{addrC},
		ToAmt:       []string{"0.003"},
		Amount:      "0.003",
		TxAction:    "internal",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	internalB := &types.Transaction{
		TxID:        trx.TxID,
		FromAddr:    []string{addrB},
		FromAmt:     []string{"0.0000369"},
		ToAddr:      []string{addrB},
		ToAmt:       []string{"49.96397016"},
		Amount:      "49.96397016",
		TxAction:    "internal",
		OutputIndex: 1,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: internalA}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: internalB}}},
	}
	if err := appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8, nil); err == nil {
		t.Fatal("expected business accounting error without trade order sendOut")
	}
}

func TestAppendBTCTransactionFeeItemsSummaryMissingToAddrFromBusiness(t *testing.T) {
	payer := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	external := "bcrt1qrya899pyx3pwu8xeu0am037a260hr7s2w5utqh"
	trx := &models.Transaction{
		TxID:          "summary-missing-to",
		BlockHash:     "hash",
		BlockHeight:   649,
		Confirmations: 1,
		Vins:          []*models.Vin{{N: 0, Addr: payer, Value: "100.00001"}},
		Vouts:         []*models.Vout{{N: 0, Addr: external, Value: "100"}},
	}
	sendTx := &types.Transaction{
		TxID:        trx.TxID,
		FromAddr:    []string{payer},
		FromAmt:     []string{"100.00001"},
		TxAction:    "send",
		OutputIndex: btcAddressNetOutputIndex,
		Decimal:     8,
	}
	items := []*types.ExtractDataItem{{
		SourceKey: "account-1",
		Data:      []*types.TxExtractData{{Transaction: sendTx}},
	}}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{payer: "100"}))
	if len(sendTx.ToAddr) != 1 || sendTx.ToAddr[0] != external {
		t.Fatalf("ToAddr = %v, want [%s]", sendTx.ToAddr, external)
	}
	if sendTx.Amount != "100" {
		t.Fatalf("Amount = %q, want 100", sendTx.Amount)
	}
	if !hasTransactionFeeItem(items) {
		t.Fatal("expected fee row")
	}
}

func TestFilterReceiveItemsKeepsPeerInboundLeg(t *testing.T) {
	payer := "bcrt1qpayer-main"
	peer := "bcrt1qpeer-recv"
	trx := &models.Transaction{
		TxID:          "internal-split-leg",
		BlockHash:     "hash",
		BlockHeight:   460,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: payer, Value: "0.01001"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: peer, Value: "0.01"},
			{N: 1, Addr: payer, Value: "0.00001"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{payer}, FromAmt: []string{"0.01001"},
			ToAddr: []string{payer}, ToAmt: []string{"0.00001"},
			TxAction: "internal", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, ToAddr: []string{peer}, ToAmt: []string{"0.01"},
			TxAction: "internal", OutputIndex: 0, Decimal: 8,
		}}}},
	}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{payer: "0.01"}))
	var peerInbound bool
	for _, item := range items {
		tx := item.Data[0].Transaction
		if btcVoutOnlyInboundLeg(tx) && normalizeScanAddress(tx.ToAddr[0]) == peer {
			peerInbound = true
		}
	}
	if !peerInbound {
		t.Fatal("peer vout-only inbound must remain; balance credits via receive row only")
	}
}

func TestFilterReceiveItemsKeepsPeerInboundWithInternalNetLeg(t *testing.T) {
	payer := "bcrt1qpayer-main"
	peer := "bcrt1qpeer-recv"
	trx := &models.Transaction{
		TxID:          "internal-dup",
		BlockHash:     "hash",
		BlockHeight:   460,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: payer, Value: "0.01001"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: peer, Value: "0.01"},
			{N: 1, Addr: payer, Value: "0.00001"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{payer}, FromAmt: []string{"0.01001"},
			ToAddr: []string{peer, payer}, ToAmt: []string{"0.01", "0.00001"},
			TxAction: "internal", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, ToAddr: []string{peer}, ToAmt: []string{"0.01"},
			TxAction: "internal", OutputIndex: 0, Decimal: 8,
		}}}},
	}
	mustAppendBTCTransactionFeeItems(t, trx, &items, testFeeAcctCtx(map[string]string{payer: "0.01"}))
	var peerInbound bool
	for _, item := range items {
		tx := item.Data[0].Transaction
		if btcVoutOnlyInboundLeg(tx) && normalizeScanAddress(tx.ToAddr[0]) == peer {
			peerInbound = true
		}
	}
	if !peerInbound {
		t.Fatalf("peer vout-only inbound to %s must remain for balance credit", peer)
	}
}
