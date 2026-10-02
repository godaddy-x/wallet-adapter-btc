package scanner

import (
	"strings"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

func TestVerifyBlockPackageIntegrityVerboseOK(t *testing.T) {
	block := &models.Block{
		Height: 1,
		NTx:    2,
		TxDetails: []*models.Transaction{
			{TxID: "aa"},
			{TxID: "bb"},
		},
	}
	if err := verifyBlockPackageIntegrity("h=1", 1, block); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyBlockPackageIntegrityHeightMismatch(t *testing.T) {
	block := &models.Block{Height: 2, NTx: 0}
	err := verifyBlockPackageIntegrity("h=1", 1, block)
	if err == nil || !strings.Contains(err.Error(), "height mismatch") {
		t.Fatalf("want height mismatch, got %v", err)
	}
}

func TestVerifyBlockPackageIntegrityNTxMismatch(t *testing.T) {
	block := &models.Block{
		Height: 1,
		NTx:    3,
		TxDetails: []*models.Transaction{
			{TxID: "aa"},
		},
	}
	err := verifyBlockPackageIntegrity("h=1", 1, block)
	if err == nil || !strings.Contains(err.Error(), "block_integrity:") {
		t.Fatalf("expected integrity error, got %v", err)
	}
}

func TestVerifyBlockPackageIntegrityMissingTxid(t *testing.T) {
	block := &models.Block{
		Height: 1,
		NTx:    1,
		TxDetails: []*models.Transaction{
			{TxID: ""},
		},
	}
	err := verifyBlockPackageIntegrity("h=1", 1, block)
	if err == nil || !strings.Contains(err.Error(), "missing txid") {
		t.Fatalf("expected missing txid, got %v", err)
	}
}

func TestVerifyLoadedTxCountMismatch(t *testing.T) {
	block := &models.Block{NTx: 2, TxIDs: []string{"a", "b"}}
	err := verifyLoadedTxCount("h=1", block, 1, nil)
	if err == nil || !strings.Contains(err.Error(), "block_integrity:") {
		t.Fatalf("expected loaded count error, got %v", err)
	}
}
