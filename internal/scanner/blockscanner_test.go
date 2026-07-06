package scanner

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	adaptscanner "github.com/godaddy-x/wallet-adapter/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
)

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
	if len(tx.FromAddr) != 1 || tx.FromAmt[0] != "1" {
		t.Fatalf("FromAddr/FromAmt = %v %v", tx.FromAddr, tx.FromAmt)
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

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(scanFn))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("len(items) = %d, want 3 (2 send + 1 fee on change leg)", len(items))
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
	if sendB == nil || len(sendB.ToAddr) != 1 || sendB.ToAddr[0] != "bcrt1qexternal000000000000000000000" || sendB.ToAmt[0] != "0.4" {
		t.Fatalf("sendB = ToAddr:%v ToAmt:%v", sendB.ToAddr, sendB.ToAmt)
	}
	if sendA == nil || len(sendA.ToAddr) != 1 || sendA.ToAmt[0] != "0.5" {
		t.Fatalf("sendA = ToAddr:%v ToAmt:%v", sendA.ToAddr, sendA.ToAmt)
	}
	feeByAddr := map[string]string{}
	for _, item := range items {
		tx := item.Data[0].Transaction
		if tx.TxAction != "fee" {
			continue
		}
		feeByAddr[tx.FromAddr[0]] = tx.Fees
	}
	if feeByAddr[addrA] != "0.00011" {
		t.Fatalf("feeByAddr = %v, want fee on change address only", feeByAddr)
	}
	if _, ok := feeByAddr[addrB]; ok {
		t.Fatalf("address B should not emit fee row, got %v", feeByAddr)
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
