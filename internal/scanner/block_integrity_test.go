package scanner

import (
	"strings"
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

func TestVerifyBlockPackageIntegrityVerboseOK(t *testing.T) {
	block := &models.Block{
		Height: 1,
		Hash:   "0000000000000000000000000000000000000000000000000000000000000000",
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

func TestVerifyBlockPackageIntegrityMissingBlockHash(t *testing.T) {
	block := &models.Block{Height: 1, NTx: 0}
	err := verifyBlockPackageIntegrity("h=1", 1, block)
	if err == nil || !strings.Contains(err.Error(), "block hash missing") {
		t.Fatalf("want missing block hash, got %v", err)
	}
}

func TestVerifyBlockPackageIntegrityHeightMismatch(t *testing.T) {
	block := &models.Block{Height: 2, Hash: "abc", NTx: 0}
	err := verifyBlockPackageIntegrity("h=1", 1, block)
	if err == nil || !strings.Contains(err.Error(), "height mismatch") {
		t.Fatalf("want height mismatch, got %v", err)
	}
}

func TestVerifyBlockPackageIntegrityNTxMismatch(t *testing.T) {
	block := &models.Block{
		Height: 1,
		Hash:   "abc",
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
		Hash:   "abc",
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

func TestVerifyBlockPackageIntegrityDuplicateTxidVerbose(t *testing.T) {
	block := &models.Block{
		Height: 1, Hash: "abc", NTx: 2,
		TxDetails: []*models.Transaction{{TxID: "AA"}, {TxID: "aa"}},
	}
	err := verifyBlockPackageIntegrity("h=1", 1, block)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate txid, got %v", err)
	}
}

func TestVerifyBlockPackageIntegrityDuplicateTxidList(t *testing.T) {
	block := &models.Block{
		Height: 1, Hash: "abc", NTx: 2,
		TxIDs: []string{"txid1", "txid1"},
	}
	err := verifyBlockPackageIntegrity("h=1", 1, block)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("want duplicate txid, got %v", err)
	}
}

func TestVerifyRequestedBlockHash(t *testing.T) {
	if err := verifyRequestedBlockHash("Aa", "aa", "t"); err != nil {
		t.Fatal(err)
	}
	err := verifyRequestedBlockHash("aaa", "bbb", "t")
	if err == nil || !strings.Contains(err.Error(), "block hash mismatch") {
		t.Fatalf("want mismatch, got %v", err)
	}
}

func TestVerifyTxBelongsToBlock(t *testing.T) {
	block := &models.Block{Height: 10, Hash: "blockhash"}
	tx := &models.Transaction{TxID: "Aa", BlockHash: "BLOCKHASH", BlockHeight: 10}
	if err := verifyTxBelongsToBlock(tx, block, "aa", "t"); err != nil {
		t.Fatal(err)
	}
	tx.BlockHash = "other"
	err := verifyTxBelongsToBlock(tx, block, "aa", "t")
	if err == nil || !strings.Contains(err.Error(), "blockhash") {
		t.Fatalf("want blockhash mismatch, got %v", err)
	}
}

func TestLookupTxIndexCaseInsensitive(t *testing.T) {
	idx := map[string]*models.Transaction{}
	storeTxIndex(idx, &models.Transaction{TxID: "AbC"})
	if lookupTxIndex(idx, "abc") == nil {
		t.Fatal("expected case-insensitive lookup")
	}
}
