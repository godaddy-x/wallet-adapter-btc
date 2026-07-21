package scanner

import (
	"sync"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
)

func TestScanBlockMuSerializesAccountTargetCache(t *testing.T) {
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
