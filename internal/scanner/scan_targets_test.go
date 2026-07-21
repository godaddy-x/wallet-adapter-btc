package scanner

import (
	"fmt"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	adaptscanner "github.com/godaddy-x/wallet-adapter/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
)

func testScannerWithTargetCache(t *testing.T) *BtcBlockScanner {
	t.Helper()
	bs := NewBlockScanner(&manager.WalletManager{Config: config.NewConfig("BTC")})
	if !bs.ensureAccountTargetCacheForCall() {
		t.Fatal("account target cache already active")
	}
	t.Cleanup(func() { bs.clearAccountTargetCache() })
	return bs
}

func TestBeginBlockAccountTargetCacheSingleCall(t *testing.T) {
	bs := testScannerWithTargetCache(t)

	var calls int
	var batchSize int
	targetFn := adaptscanner.BlockScanTargetFunc(func(param *types.ScanTargetParam) error {
		calls++
		batchSize = len(param.ScanTarget)
		for addr := range param.ScanTarget {
			if addr == "bcrt1qwatched0000000000000000000000" {
				param.ScanTarget[addr] = "acct-1"
			}
		}
		return nil
	})

	const n = 1500
	block := &models.Block{TxDetails: make([]*models.Transaction, 0, n)}
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("bcrt1qbatch%04d000000000000000000", i)
		if i == 0 {
			addr = "bcrt1qwatched0000000000000000000000"
		}
		block.TxDetails = append(block.TxDetails, &models.Transaction{
			TxID: fmt.Sprintf("tx-%d", i),
			Vouts: []*models.Vout{
				{N: 0, Addr: addr, Value: "1"},
			},
		})
	}
	bs.beginBlockAccountTargetCache(block, nil, targetFn)
	if calls != 1 {
		t.Fatalf("calls=%d want single scanTargetFunc invocation", calls)
	}
	if batchSize != n {
		t.Fatalf("batchSize=%d want %d unique addresses passed through", batchSize, n)
	}
	if got := bs.queryAccountTarget(targetFn, "bcrt1qwatched0000000000000000000000"); got != "acct-1" {
		t.Fatalf("cache miss: %q", got)
	}
}

func TestTxTouchesManagedSkipsUnrelated(t *testing.T) {
	bs := testScannerWithTargetCache(t)
	targetFn := adaptscanner.BlockScanTargetFunc(func(param *types.ScanTargetParam) error {
		return nil
	})
	block := &models.Block{
		TxDetails: []*models.Transaction{{
			TxID: "x",
			Vouts: []*models.Vout{
				{N: 0, Addr: "bcrt1qexternal000000000000000000000", Value: "1"},
			},
			Vins: []*models.Vin{
				{TxID: "p", Vout: 0, Addr: "bcrt1qexternal000000000000000000000", Value: "1"},
			},
		}},
	}
	bs.beginBlockAccountTargetCache(block, nil, targetFn)
	if bs.txTouchesManaged(block.TxDetails[0], nil, targetFn) {
		t.Fatal("expected unrelated tx to be skipped")
	}
}

func TestExtractTransactionSkipsWhenNoManagedTargets(t *testing.T) {
	bs := testScannerWithTargetCache(t)
	external := "bcrt1qexternal000000000000000000000"
	trx := &models.Transaction{
		TxID: "unrelated",
		Vins: []*models.Vin{
			{TxID: "missing-prev", Vout: 0, Addr: external, Value: "1"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: "bcrt1qother00000000000000000000000", Value: "0.99"},
		},
	}
	bs.beginBlockAccountTargetCache(&models.Block{TxDetails: []*models.Transaction{trx}}, nil, func(param *types.ScanTargetParam) error {
		return nil
	})

	items, err := bs.extractTransaction(trx, nil, adaptscanner.BlockScanTargetFunc(func(param *types.ScanTargetParam) error {
		return nil
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items)=%d want 0", len(items))
	}
}

func TestExtractTransactionStillMatchesInboundBeforeFill(t *testing.T) {
	bs := NewBlockScanner(&manager.WalletManager{Config: config.NewConfig("BTC")})
	watched := "bcrt1qwatched0000000000000000000000"
	trx := &models.Transaction{
		TxID: "deposit",
		Vins: []*models.Vin{
			{TxID: "prev", Vout: 0, Addr: "bcrt1qsender000000000000000000000", Value: "1"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: watched, Value: "1"},
		},
	}
	scanFn := adaptscanner.BlockScanTargetFunc(func(param *types.ScanTargetParam) error {
		if _, ok := param.ScanTarget[watched]; ok {
			param.ScanTarget[watched] = "acct-1"
		}
		return nil
	})

	items, err := bs.extractTransaction(trx, nil, scanFn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("expected deposit extract without prevout RPC")
	}
}

func TestTxTouchesManagedSkipsUnresolvedVin(t *testing.T) {
	bs := testScannerWithTargetCache(t)
	targetFn := adaptscanner.BlockScanTargetFunc(func(param *types.ScanTargetParam) error {
		return nil
	})
	trx := &models.Transaction{
		TxID: "cross-block-spend",
		Vins: []*models.Vin{{TxID: "external-prev", Vout: 0}},
		Vouts: []*models.Vout{
			{N: 0, Addr: "bcrt1qexternal000000000000000000000", Value: "1"},
		},
	}
	if bs.txTouchesManaged(trx, nil, targetFn) {
		t.Fatal("expected unresolved vin without cache hit to be skipped")
	}
}

func TestBlockHasManagedCandidateSkipsEmptyBlock(t *testing.T) {
	bs := testScannerWithTargetCache(t)
	targetFn := adaptscanner.BlockScanTargetFunc(func(param *types.ScanTargetParam) error {
		return nil
	})
	block := &models.Block{
		TxDetails: []*models.Transaction{{
			TxID: "x",
			Vouts: []*models.Vout{
				{N: 0, Addr: "bcrt1qexternal000000000000000000000", Value: "1"},
			},
		}},
	}
	bs.beginBlockAccountTargetCache(block, nil, targetFn)
	if bs.blockHasManagedCandidate(block, nil, targetFn) {
		t.Fatal("expected no managed candidate in unrelated block")
	}
}

func TestVinNeedsRPCResolution(t *testing.T) {
	trx := &models.Transaction{
		Vins: []*models.Vin{{TxID: "ext", Vout: 0}},
	}
	if !vinNeedsRPCResolution(trx, nil) {
		t.Fatal("expected RPC resolution for empty vin address")
	}
	trx.Vins[0].Addr = "bcrt1qknown00000000000000000000000"
	trx.Vins[0].Value = "1"
	if vinNeedsRPCResolution(trx, nil) {
		t.Fatal("expected no RPC when vin already resolved")
	}
}
