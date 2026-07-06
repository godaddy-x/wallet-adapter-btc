package scanner

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
)

func TestInferBTCTxActionReceive(t *testing.T) {
	trx := &models.Transaction{
		Vouts: []*models.Vout{{N: 0, Addr: "bcrt1qrecv", Value: "1.0"}},
	}
	param := types.ScanTargetParam{
		ScanTarget: map[string]interface{}{"bcrt1qrecv": "acct-1"},
	}
	leg := &addrLeg{sourceKey: "acct-1", vouts: trx.Vouts}
	if got := inferBTCTxAction(trx, leg, param); got != "receive" {
		t.Fatalf("got %q want receive", got)
	}
}

func TestInferBTCTxActionSendWithChange(t *testing.T) {
	sender := "bcrt1qsender"
	external := "bcrt1qexternal"
	trx := &models.Transaction{
		Vins:  []*models.Vin{{N: 0, Addr: sender, Value: "1.0"}},
		Vouts: []*models.Vout{{N: 0, Addr: external, Value: "0.1"}, {N: 1, Addr: sender, Value: "0.899"}},
	}
	param := types.ScanTargetParam{
		ScanTarget: map[string]interface{}{sender: "acct-1", external: "acct-2"},
	}
	leg := &addrLeg{
		sourceKey: "acct-1",
		vins:      []*models.Vin{trx.Vins[0]},
		vouts:     []*models.Vout{trx.Vouts[1]},
	}
	if got := inferBTCTxAction(trx, leg, param); got != "send" {
		t.Fatalf("got %q want send", got)
	}
}

func TestInferBTCTxActionInternal(t *testing.T) {
	addrA := "bcrt1qa"
	addrB := "bcrt1qb"
	trx := &models.Transaction{
		Vins:  []*models.Vin{{N: 0, Addr: addrA, Value: "1.0"}},
		Vouts: []*models.Vout{{N: 0, Addr: addrB, Value: "0.99"}},
	}
	param := types.ScanTargetParam{
		ScanTarget: map[string]interface{}{addrA: "acct-1", addrB: "acct-1"},
	}
	leg := &addrLeg{sourceKey: "acct-1", vouts: []*models.Vout{trx.Vouts[0]}}
	if got := inferBTCTxAction(trx, leg, param); got != "internal" {
		t.Fatalf("got %q want internal", got)
	}
}
