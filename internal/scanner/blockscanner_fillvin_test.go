package scanner

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

func TestFillVinAddressesRefreshesPresetAddr(t *testing.T) {
	wm := &manager.WalletManager{Config: config.NewConfig("BTC")}
	bs := NewBlockScanner(wm)

	wrongAddr := "bcrt1qwrong000000000000000000000000000"
	correctAddr := "bcrt1qcorrect00000000000000000000000"
	prevTx := &models.Transaction{
		TxID: "prevtx",
		Vouts: []*models.Vout{
			{N: 0, Addr: correctAddr, Value: "1.5"},
		},
	}
	trx := &models.Transaction{
		TxID: "spendtx",
		Vins: []*models.Vin{
			{TxID: "prevtx", Vout: 0, Addr: wrongAddr, Value: "9.9"},
		},
	}
	txIndex := map[string]*models.Transaction{"prevtx": prevTx}

	bs.fillVinAddresses(trx, txIndex)

	if trx.Vins[0].Addr != correctAddr {
		t.Fatalf("Addr = %q, want %q", trx.Vins[0].Addr, correctAddr)
	}
	if trx.Vins[0].Value != "1.5" {
		t.Fatalf("Value = %q, want 1.5", trx.Vins[0].Value)
	}
}
