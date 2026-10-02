package scanner

import (
	"context"
	"strings"
	"testing"

	adapter "github.com/godaddy-x/wallet-adapter"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

func TestMatchBTCAnchor(t *testing.T) {
	req := adapter.CrossCheckRequest{
		TxID:        "abc123",
		BlockHeight: 800000,
		BlockHash:   "blockhashA",
	}
	txOK := &models.Transaction{
		TxID:          "abc123",
		BlockHash:     "blockhashA",
		BlockHeight:   800000,
		Confirmations: 6,
	}
	ok, reason, err := matchBTCAnchor(nil, "abc123", req, txOK)
	if err != nil || !ok || reason != "" {
		t.Fatalf("want match ok=%v err=%v reason=%q", ok, err, reason)
	}

	txBadHash := &models.Transaction{TxID: "abc123", BlockHash: "other", Confirmations: 2}
	ok, reason, _ = matchBTCAnchor(nil, "abc123", req, txBadHash)
	if ok || !strings.Contains(reason, "blockhash mismatch") {
		t.Fatalf("want hash mismatch, ok=%v reason=%q", ok, reason)
	}

	txUnconf := &models.Transaction{TxID: "abc123", BlockHash: "", Confirmations: 0}
	ok, reason, _ = matchBTCAnchor(nil, "abc123", req, txUnconf)
	if ok || !strings.Contains(reason, "not included in block") {
		t.Fatalf("want unconfirmed, ok=%v reason=%q", ok, reason)
	}

	txLowConf := &models.Transaction{TxID: "abc123", BlockHash: "blockhashA", Confirmations: 0}
	ok, reason, _ = matchBTCAnchor(nil, "abc123", req, txLowConf)
	if ok || !strings.Contains(reason, "confirmations too low") {
		t.Fatalf("want low conf, ok=%v reason=%q", ok, reason)
	}

	txBadHeight := &models.Transaction{
		TxID: "abc123", BlockHash: "blockhashA", BlockHeight: 799999, Confirmations: 3,
	}
	ok, reason, _ = matchBTCAnchor(nil, "abc123", req, txBadHeight)
	if ok || !strings.Contains(reason, "block height mismatch") {
		t.Fatalf("want height mismatch, ok=%v reason=%q", ok, reason)
	}
}

func TestVerifyBeforePromoteIncompleteAnchorBTC(t *testing.T) {
	bs := &BtcBlockScanner{}
	bs.crossCheckGen = &crossCheckPeerGen{peers: []crossCheckPeer{{url: "http://127.0.0.1:8332"}}}
	res, err := bs.VerifyBeforePromote(context.Background(), adapter.CrossCheckRequest{
		TxID: "abc", BlockHeight: 0, BlockHash: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Reason, "incomplete anchor") {
		t.Fatalf("want incomplete anchor, got %+v", res)
	}
}

func TestRedactPeerURLBTC(t *testing.T) {
	got := redactPeerURL("https://rpc.example/v1/secretkey")
	if strings.Contains(got, "secretkey") {
		t.Fatalf("want redacted path, got %q", got)
	}
}
