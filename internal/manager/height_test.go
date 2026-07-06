package manager

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
)

func TestGetBlockHeightRequiresClient(t *testing.T) {
	wm := &WalletManager{Config: config.NewConfig("BTC")}
	if _, err := wm.GetBlockHeight(); err == nil {
		t.Fatal("expected error when rpc client is nil")
	}
}
