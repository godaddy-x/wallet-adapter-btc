package scanner

import (
	"fmt"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	adaptscanner "github.com/godaddy-x/wallet-adapter/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

func TestExtractTransactionInternalKeepsPeerReceiveLeg(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	payer := "bcrt1qmainpayer000000000000000000000"
	peer := "bcrt1qpeerrecv0000000000000000000000"
	trx := &models.Transaction{
		TxID:          "internal-peer",
		BlockHash:     "blockhash",
		BlockHeight:   437,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: payer, Value: "0.01001"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: peer, Value: "0.01"},
			{N: 1, Addr: payer, Value: "0.00001"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		for _, addr := range []string{payer, peer} {
			if _, ok := param.ScanTarget[addr]; ok {
				param.ScanTarget[addr] = "account-1"
			}
		}
		return nil
	}

	wireTestTradeOrderLookup(bs, map[string]map[string]string{
		trx.TxID: {payer: "0.01"},
	})

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	var payerInternal *types.Transaction
	var peerReceive bool
	for _, item := range items {
		tx := item.Data[0].Transaction
		if btcVoutOnlyInboundLeg(tx) && normalizeScanAddress(tx.ToAddr[0]) == peer {
			peerReceive = true
		}
		if tx.TxAction == "internal" && len(tx.FromAddr) > 0 {
			payerInternal = tx
		}
	}
	if !peerReceive {
		t.Fatal("peer must have its own receive row")
	}
	if payerInternal == nil {
		t.Fatalf("missing payer internal row, items=%d", len(items))
	}
	hasPeer := false
	for _, to := range payerInternal.ToAddr {
		if normalizeScanAddress(to) == peer {
			hasPeer = true
			break
		}
	}
	if !hasPeer {
		t.Fatalf("payer internal must show external target in ToAddr: %v", payerInternal.ToAddr)
	}
}

func TestExtractTransactionNonFeeLegFeesCanonical(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	addr := "bcrt1qrecv000000000000000000000000000"
	trx := &models.Transaction{
		TxID:          "recv-fees",
		BlockHash:     "blockhash",
		BlockHeight:   100,
		Confirmations: 1,
		Vouts: []*models.Vout{
			{N: 0, Addr: addr, Value: "1.0"},
		},
	}
	scanFn := func(param *types.ScanTargetParam) error {
		if _, ok := param.ScanTarget[addr]; ok {
			param.ScanTarget[addr] = "account-1"
		}
		return nil
	}
	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		tx := item.Data[0].Transaction
		if tx.TxAction == "fee" {
			if tx.Fees == "" || tx.Fees == btcNonFeeLegFees {
				t.Fatalf("fee leg must carry miner fee, got fees=%q", tx.Fees)
			}
			continue
		}
		if tx.Fees != btcNonFeeLegFees {
			t.Fatalf("non-fee leg action=%s outputIndex=%d fees=%q, want %q",
				tx.TxAction, tx.OutputIndex, tx.Fees, btcNonFeeLegFees)
		}
	}
}

func TestExtractTransactionPerVoutOutputIndex(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	targetAddr := "bc1qdep0sitaddr00000000000000000000000"
	trx := &models.Transaction{
		TxID:          "abc123",
		BlockHash:     "blockhash",
		BlockHeight:   100,
		Confirmations: 1,
		Vouts: []*models.Vout{
			{N: 0, Addr: targetAddr, Value: "0.1"},
			{N: 1, Addr: targetAddr, Value: "0.2"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		for k := range param.ScanTarget {
			param.ScanTarget[k] = "account-1"
		}
		return nil
	}

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1 merged item per address", len(items))
	}
	item := items[0]
	if item.SourceKey != "account-1" {
		t.Fatalf("sourceKey = %q", item.SourceKey)
	}
	tx := item.Data[0].Transaction
	if tx.OutputIndex != btcAddressNetOutputIndex {
		t.Fatalf("OutputIndex = %d, want %d", tx.OutputIndex, btcAddressNetOutputIndex)
	}
	if len(tx.ToAddr) != 2 || len(tx.ToAmt) != 2 {
		t.Fatalf("expected 2 vout legs merged, got toAddr=%v toAmt=%v", tx.ToAddr, tx.ToAmt)
	}
}

func TestExtractTransactionMergesVinVoutPerAddress(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	targetAddr := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	trx := &models.Transaction{
		TxID:          "selfchange",
		BlockHash:     "blockhash",
		BlockHeight:   100,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: targetAddr, Value: "1.0"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: "bcrt1qexternal000000000000000000000", Value: "0.9"},
			{N: 1, Addr: targetAddr, Value: "0.09989"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		if _, ok := param.ScanTarget[targetAddr]; ok {
			param.ScanTarget[targetAddr] = "account-1"
		}
		return nil
	}

	wireTestTradeOrderLookup(bs, map[string]map[string]string{
		trx.TxID: {targetAddr: "0.9"},
	})

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 (send + fee)", len(items))
	}
	tx := items[0].Data[0].Transaction
	if tx.OutputIndex != btcAddressNetOutputIndex {
		t.Fatalf("OutputIndex = %d, want %d", tx.OutputIndex, btcAddressNetOutputIndex)
	}
	if tx.TxAction != "send" {
		t.Fatalf("TxAction = %q, want send", tx.TxAction)
	}
	if len(tx.FromAddr) != 1 || tx.FromAmt[0] != "0.9" {
		t.Fatalf("FromAddr/FromAmt = %v %v, want sendOut 0.9", tx.FromAddr, tx.FromAmt)
	}
	if tx.ToAmt[0] != "0.9" {
		t.Fatalf("ToAddr/ToAmt = %v %v, want external only", tx.ToAddr, tx.ToAmt)
	}
	if tx.ToAddr[0] != "bcrt1qexternal000000000000000000000" {
		t.Fatalf("send targets = %v", tx.ToAddr)
	}
	feeTx := items[1].Data[0].Transaction
	if feeTx.TxAction != "fee" || feeTx.OutputIndex != btcFeeOutputIndex {
		t.Fatalf("fee item = %+v", feeTx)
	}
	if feeTx.Fees != "0.00011" {
		t.Fatalf("Fees = %q, want 0.00011", feeTx.Fees)
	}
}

func TestExtractTransactionFeeSplitAcrossTwoPayerAddresses(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	addrA := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	addrB := "bcrt1qrya899pyx3pwu8xeu0am037a260hr7s2w5utqh"
	trx := &models.Transaction{
		TxID:          "summary-fee",
		BlockHash:     "blockhash",
		BlockHeight:   100,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: addrA, Value: "0.6"},
			{N: 1, Addr: addrB, Value: "0.4"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: "bcrt1qexternal000000000000000000000", Value: "0.9"},
			{N: 1, Addr: addrA, Value: "0.09989"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		if _, ok := param.ScanTarget[addrA]; ok {
			param.ScanTarget[addrA] = "account-1"
		}
		if _, ok := param.ScanTarget[addrB]; ok {
			param.ScanTarget[addrB] = "account-1"
		}
		return nil
	}

	wireTestTradeOrderLookup(bs, map[string]map[string]string{
		trx.TxID: {
			addrA: "0.50004889",
			addrB: "0.39995111",
		},
	})

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("len(items) = %d, want 4 (2 send + 2 fee legs)", len(items))
	}
	var sendA, sendB *types.Transaction
	for _, item := range items {
		tx := item.Data[0].Transaction
		if tx.TxAction == "fee" {
			continue
		}
		if len(tx.FromAddr) == 0 {
			continue
		}
		switch tx.FromAddr[0] {
		case addrA:
			sendA = tx
		case addrB:
			sendB = tx
		}
	}
	if sendB == nil || len(sendB.ToAddr) != 1 || sendB.ToAddr[0] != "bcrt1qexternal000000000000000000000" || sendB.ToAmt[0] != "0.39995111" {
		t.Fatalf("sendB = ToAddr:%v ToAmt:%v", sendB.ToAddr, sendB.ToAmt)
	}
	if sendA == nil || len(sendA.ToAddr) != 1 || sendA.ToAmt[0] != "0.50004889" {
		t.Fatalf("sendA = ToAddr:%v ToAmt:%v", sendA.ToAddr, sendA.ToAmt)
	}
	feeByAddr := map[string]string{}
	feeTotal := decimal.Zero
	for _, item := range items {
		tx := item.Data[0].Transaction
		if tx.TxAction != "fee" {
			continue
		}
		feeByAddr[tx.FromAddr[0]] = tx.Fees
		f, err := decimal.NewFromString(tx.Fees)
		if err != nil {
			t.Fatal(err)
		}
		feeTotal = feeTotal.Add(f)
	}
	if !feeTotal.Equal(decimal.RequireFromString("0.00011")) {
		t.Fatalf("fee total = %s want 0.00011 (by addr %v)", feeTotal, feeByAddr)
	}
}

func TestExtractTransactionAddressCaseInsensitive(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	trx := &models.Transaction{
		TxID: "abc123",
		Vouts: []*models.Vout{
			{N: 0, Addr: "1BvBMSEYstWetqTFn5Au4m4GFg7xJaNVN2", Value: "0.1"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		for k := range param.ScanTarget {
			param.ScanTarget[k] = "account-legacy"
		}
		return nil
	}

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
}

func TestExtractTransactionSkipsZeroAmount(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	targetAddr := "bc1qdep0sitaddr00000000000000000000000"
	trx := &models.Transaction{
		TxID: "abc123",
		Vouts: []*models.Vout{
			{N: 0, Addr: targetAddr, Value: "0"},
			{N: 1, Addr: targetAddr, Value: "0.1"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		for k := range param.ScanTarget {
			param.ScanTarget[k] = "account-1"
		}
		return nil
	}

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Data[0].Transaction.OutputIndex != 1 {
		t.Fatalf("OutputIndex = %d, want 1", items[0].Data[0].Transaction.OutputIndex)
	}
}

func TestExtractTransactionSkipsAddressScriptMismatch(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	targetAddr := "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
	wrongAddr := "bc1qdep0sitaddr00000000000000000000000"
	scriptHex := "0014751e76e8199196d454941c45d1b3a323f1433bd6"

	trx := &models.Transaction{
		TxID: "abc123",
		Vouts: []*models.Vout{
			{
				N:            0,
				Addr:         wrongAddr,
				Value:        "0.1",
				ScriptPubKey: scriptHex,
				Type:         "witness_v0_keyhash",
			},
			{
				N:            1,
				Addr:         targetAddr,
				Value:        "0.2",
				ScriptPubKey: scriptHex,
				Type:         "witness_v0_keyhash",
			},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		for k := range param.ScanTarget {
			param.ScanTarget[k] = "account-1"
		}
		return nil
	}

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Data[0].Transaction.OutputIndex != 1 {
		t.Fatalf("OutputIndex = %d, want 1", items[0].Data[0].Transaction.OutputIndex)
	}
}

func TestExtractTransactionSummaryVinOnlyWithSameAccountChange(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	mainAddr := "bcrt1qmainpayer000000000000000000000"
	changeAddr := "bcrt1qchangepayee000000000000000000"
	external := "bcrt1qexternal000000000000000000000"
	trx := &models.Transaction{
		TxID:          "summary-same-acct",
		BlockHash:     "blockhash",
		BlockHeight:   278,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: changeAddr, Value: "0.0001"},
			{N: 1, Addr: mainAddr, Value: "25"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "0.003"},
			{N: 1, Addr: changeAddr, Value: "24.9969"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		if _, ok := param.ScanTarget[mainAddr]; ok {
			param.ScanTarget[mainAddr] = "account-1"
		}
		if _, ok := param.ScanTarget[changeAddr]; ok {
			param.ScanTarget[changeAddr] = "account-1"
		}
		return nil
	}

	wireTestTradeOrderLookup(bs, map[string]map[string]string{
		trx.TxID: {
			mainAddr:   "0.0029",
			changeAddr: "0.0002", // sum 0.0031 != external 0.003 → fail-stop
		},
	})

	_, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err == nil {
		t.Fatal("expected business accounting error when sendOut sum != external")
	}
}

func TestExtractTransactionFailsUnresolvedVin(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	trx := &models.Transaction{
		TxID: "unresolved-vin",
		Vins: []*models.Vin{{TxID: "missing-prev", Vout: 0}},
		Vouts: []*models.Vout{
			{N: 0, Addr: "bcrt1qexternal000000000000000000000", Value: "1"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error { return nil }

	_, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err == nil {
		t.Fatal("expected error for unresolved vin address")
	}
}

func TestExtractTransactionManyVinSummary(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	payer := "bcrt1qpayer0000000000000000000000000"
	external := "bcrt1qexternal000000000000000000000"
	vins := make([]*models.Vin, 0, 101)
	txIndex := make(map[string]*models.Transaction, 101)
	for i := 0; i < 101; i++ {
		prevID := fmt.Sprintf("prev-%03d", i)
		txIndex[prevID] = &models.Transaction{
			TxID: prevID,
			Vouts: []*models.Vout{
				{N: 0, Addr: payer, Value: "25"},
			},
		}
		vins = append(vins, &models.Vin{TxID: prevID, Vout: 0, N: uint64(i)})
	}
	trx := &models.Transaction{
		TxID:          "many-vin-summary",
		BlockHash:     "blockhash",
		BlockHeight:   737,
		Confirmations: 1,
		Vins:          vins,
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "2500"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		if _, ok := param.ScanTarget[payer]; ok {
			param.ScanTarget[payer] = "account-1"
		}
		return nil
	}

	wireTestTradeOrderLookup(bs, map[string]map[string]string{
		trx.TxID: {payer: "2500"},
	})

	items, err := bs.extractTransaction(trx, txIndex, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Fatalf("len(items)=%d want send+fee at minimum", len(items))
	}
	tx := items[0].Data[0].Transaction
	if tx.TxAction != "send" {
		t.Fatalf("TxAction=%q want send", tx.TxAction)
	}
	if len(tx.FromAddr) != 1 || tx.FromAmt[0] != "2500" {
		t.Fatalf("from=%v %v want sendOut 2500", tx.FromAddr, tx.FromAmt)
	}
}

func TestExtractTransactionManyVinSummaryWithEmbeddedPrevout(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	payer := "bcrt1qpayer0000000000000000000000000"
	external := "bcrt1qexternal000000000000000000000"
	vins := make([]*models.Vin, 0, 101)
	for i := 0; i < 101; i++ {
		vins = append(vins, &models.Vin{N: uint64(i), Addr: payer, Value: "25"})
	}
	trx := &models.Transaction{
		TxID:          "many-vin-prevout",
		BlockHash:     "blockhash",
		BlockHeight:   649,
		Confirmations: 1,
		Vins:          vins,
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "2524.99998"},
		},
	}

	scanFn := func(param *types.ScanTargetParam) error {
		if _, ok := param.ScanTarget[payer]; ok {
			param.ScanTarget[payer] = "account-1"
		}
		return nil
	}

	wireTestTradeOrderLookup(bs, map[string]map[string]string{
		trx.TxID: {payer: "2524.99998"},
	})

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Fatalf("len(items)=%d want send+fee", len(items))
	}
	sendTx := items[0].Data[0].Transaction
	if sendTx.TxAction != "send" {
		t.Fatalf("TxAction=%q want send", sendTx.TxAction)
	}
	if len(sendTx.ToAddr) != 1 || sendTx.ToAddr[0] != external {
		t.Fatalf("ToAddr=%v want [%s]", sendTx.ToAddr, external)
	}
	if !hasTransactionFeeItem(items) {
		t.Fatal("missing fee row")
	}
}
