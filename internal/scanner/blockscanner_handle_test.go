package scanner

import (
	"sync"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
)

func TestScanBlockPrioritizeEnqueuesOnly(t *testing.T) {
	bs := NewBlockScanner(&manager.WalletManager{Config: config.NewConfig("BTC")})
	bs.scanLoopRunning.Store(true)

	if err := bs.ScanBlockPrioritize([]uint64{10, 11, 10}); err != nil {
		t.Fatal(err)
	}
	bs.priorityScanMu.Lock()
	got := append([]uint64(nil), bs.priorityHeights...)
	bs.priorityScanMu.Unlock()
	if len(got) != 2 || got[0] != 10 || got[1] != 11 {
		t.Fatalf("queue = %v, want [10 11]", got)
	}
}

func TestScanBlockMuSerializesWithNestedOnce(t *testing.T) {
	bs := NewBlockScanner(&manager.WalletManager{Config: config.NewConfig("BTC")})

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bs.scanBlockMu.Lock()
			defer bs.scanBlockMu.Unlock()
			if !bs.ensureAccountTargetCacheForCall() {
				errCh <- "expected exclusive account target cache ownership"
				return
			}
			defer bs.clearAccountTargetCache()
		}()
	}
	wg.Wait()
	close(errCh)
	for msg := range errCh {
		t.Fatal(msg)
	}
}
