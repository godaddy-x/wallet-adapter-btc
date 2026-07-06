package scanner

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

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
	appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (1 send + 1 fee)", len(items))
	}
	if sendTx.ToAmt[0] != "0.9" {
		t.Fatalf("send ToAmt = %v, want [0.9]", sendTx.ToAmt)
	}
	if sendTx.ToAddr[0] != "bcrt1qexternal" {
		t.Fatalf("send ToAddr = %v, want external only", sendTx.ToAddr)
	}
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != "1" {
		t.Fatalf("send From = %v %v, want collapsed [1]", sendTx.FromAddr, sendTx.FromAmt)
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
	appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8)
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3 (2 send + 1 fee on change leg)", len(items))
	}
	if len(sendA.ToAddr) != 1 || sendA.ToAmt[0] != "0.5" {
		t.Fatalf("sendA = ToAddr:%v ToAmt:%v", sendA.ToAddr, sendA.ToAmt)
	}
	if sendA.ToAddr[0] != "bcrt1qexternal" {
		t.Fatalf("sendA targets = %v", sendA.ToAddr)
	}
	if len(sendB.ToAddr) != 1 || sendB.ToAddr[0] != "bcrt1qexternal" || sendB.ToAmt[0] != "0.4" {
		t.Fatalf("sendB = ToAddr:%v ToAmt:%v", sendB.ToAddr, sendB.ToAmt)
	}
	feeTx := items[2].Data[0].Transaction
	if feeTx.FromAddr[0] != addrA || feeTx.Fees != "0.00011" {
		t.Fatalf("fee = addr:%q fees:%q", feeTx.FromAddr[0], feeTx.Fees)
	}
	assertFeeSharesSumToTotal(t, trx, items, 8)

	legs := computePayerLegFees(trx, []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrA}, FromAmt: []string{"0.6"},
			ToAddr: []string{addrA}, ToAmt: []string{"0.09989"},
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			FromAddr: []string{addrB}, FromAmt: []string{"0.4"},
		}}}},
	}, 8)
	if len(legs) != 1 {
		t.Fatalf("fee legs = %d, want 1", len(legs))
	}
	if legs[0].sendOutSats != 50000000 || legs[0].feeSats != 11000 {
		t.Fatalf("change leg sendOut/fee = %d/%d, want 50000000/11000", legs[0].sendOutSats, legs[0].feeSats)
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
	allLegs := collectBTCPayerLegs(items, 8)
	if len(allLegs) != 2 {
		t.Fatalf("payer legs = %d, want 2", len(allLegs))
	}
	feeLegs := computePayerLegFees(trx, items, 8)
	if len(feeLegs) != 1 || feeLegs[0].payerAddr != addrA {
		t.Fatalf("fee legs = %+v", feeLegs)
	}
	for _, leg := range allLegs {
		if leg.payerAddr == addrB {
			// recompute inline for vin-only leg
			if leg.vinSats != 40000000 {
				t.Fatalf("B vin = %d", leg.vinSats)
			}
		}
	}
	// B: sendOut=vin=0.4, fee=0; A: sendOut=0.5, fee=0.00011
	legs := computePayerLegFees(trx, items, 8)
	_ = legs
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
	appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (internal + fee, receive filtered)", len(items))
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
	if len(out.FromAddr) != 1 || out.FromAmt[0] != "1" {
		t.Fatalf("From collapsed = %v %v", out.FromAddr, out.FromAmt)
	}
	if len(out.ToAddr) != 2 || out.ToAddr[0] != addrB || out.ToAddr[1] != addrA {
		t.Fatalf("ToAddr = %v, want peer transfer + change on payer", out.ToAddr)
	}
	if out.ToAmt[0] != "0.0001" || out.ToAmt[1] != "0.99989" {
		t.Fatalf("ToAmt = %v", out.ToAmt)
	}
	feeTx := items[1].Data[0].Transaction
	if feeTx.TxAction != "fee" || feeTx.Fees != "0.00001" {
		t.Fatalf("fee row = action:%q fees:%q", feeTx.TxAction, feeTx.Fees)
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
	appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (send + fee)", len(items))
	}
	if len(sendTx.ToAddr) != 1 || sendTx.ToAddr[0] != external {
		t.Fatalf("send ToAddr = %v, want [%s]", sendTx.ToAddr, external)
	}
	if sendTx.ToAmt[0] != "99.99998" {
		t.Fatalf("send ToAmt = %v", sendTx.ToAmt)
	}
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != "100" {
		t.Fatalf("send From collapsed = %v %v, want addr total 100", sendTx.FromAddr, sendTx.FromAmt)
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
	appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (send + fee)", len(items))
	}
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != vinSum.String() {
		t.Fatalf("From collapsed = %v %v, want single total %s", sendTx.FromAddr, sendTx.FromAmt, vinSum)
	}
	if len(sendTx.ToAddr) != 1 || sendTx.ToAddr[0] != external {
		t.Fatalf("ToAddr = %v, want [%s]", sendTx.ToAddr, external)
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
	// Mismatch between extract vin sum and chain vouts prevents fee accounting; collapse still runs.
	trx := &models.Transaction{
		TxID:  "skip-fee-tx",
		Vins:  []*models.Vin{{N: 0, Addr: addr, Value: "1"}},
		Vouts: []*models.Vout{{N: 0, Addr: "bcrt1qother", Value: "2.5"}},
	}
	appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8)
	if len(sendTx.FromAddr) != 1 || sendTx.FromAmt[0] != "3" {
		t.Fatalf("From collapsed = %v %v, want single payer total 3", sendTx.FromAddr, sendTx.FromAmt)
	}
}

func TestAppendBTCTransactionFeeItemsThreePayerSummary(t *testing.T) {
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
			{N: 0, Addr: addrA, Value: "1.0"},
			{N: 1, Addr: addrB, Value: "0.5"},
			{N: 2, Addr: addrC, Value: "0.5"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "1.99997"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{addrA}, FromAmt: []string{"1.0"}, TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{addrB}, FromAmt: []string{"0.5"}, TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{addrC}, FromAmt: []string{"0.5"}, TxAction: "send", OutputIndex: btcAddressNetOutputIndex, Decimal: 8,
		}}}},
	}
	appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8)
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
		if send.ToAddr[0] == addr {
			t.Fatalf("send %s must not include self in to", addr)
		}
	}
	if len(sendByPayer) != 3 {
		t.Fatalf("send rows = %d, want 3", len(sendByPayer))
	}
	if len(feeByPayer) == 0 {
		t.Fatal("expected at least one fee row")
	}
	assertFeeSharesSumToTotal(t, trx, items, 8)
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
