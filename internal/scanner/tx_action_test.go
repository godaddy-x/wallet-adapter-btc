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

func TestInferBTCTxActionSummaryVinOnlyWithSameAccountChange(t *testing.T) {
	mainAddr := "bcrt1qmain"
	changeAddr := "bcrt1qchange"
	external := "bcrt1qexternal"
	trx := &models.Transaction{
		Vins: []*models.Vin{
			{N: 0, Addr: changeAddr, Value: "0.0001"},
			{N: 1, Addr: mainAddr, Value: "25"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: external, Value: "0.003"},
			{N: 1, Addr: changeAddr, Value: "24.964"},
		},
	}
	param := types.ScanTargetParam{
		ScanTarget: map[string]interface{}{
			mainAddr:   "acct-1",
			changeAddr: "acct-1",
		},
	}
	mainLeg := &addrLeg{
		sourceKey: "acct-1",
		vins:      []*models.Vin{trx.Vins[1]},
	}
	if got := inferBTCTxAction(trx, mainLeg, param); got != "send" {
		t.Fatalf("main vin-only leg got %q want send", got)
	}
}

func TestInferBTCTxActionSummarySameAccountMultiRecipient(t *testing.T) {
	mainAddr := "bcrt1qmain"
	peerA := "bcrt1qpeera"
	peerB := "bcrt1qpeerb"
	trx := &models.Transaction{
		Vins: []*models.Vin{
			{N: 0, Addr: mainAddr, Value: "50"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: peerA, Value: "0.003"},
			{N: 1, Addr: peerB, Value: "49.96"},
		},
	}
	param := types.ScanTargetParam{
		ScanTarget: map[string]interface{}{
			mainAddr: "acct-1",
			peerA:    "acct-1",
			peerB:    "acct-1",
		},
	}
	mainLeg := &addrLeg{sourceKey: "acct-1", vins: []*models.Vin{trx.Vins[0]}}
	if got := inferBTCTxAction(trx, mainLeg, param); got != "send" {
		t.Fatalf("same-account multi-recipient summary got %q want send", got)
	}
}
