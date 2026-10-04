package scanner

import (
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

// verifyRequestedBlockHash ensures getblock result matches the hash from getblockhash.
func verifyRequestedBlockHash(requested, got, blockTag string) error {
	requested = strings.TrimSpace(requested)
	got = strings.TrimSpace(got)
	if requested == "" {
		return fmt.Errorf("block_integrity: requested block hash empty at %s", blockTag)
	}
	if got == "" {
		return fmt.Errorf("block_integrity: block hash missing at %s", blockTag)
	}
	if !strings.EqualFold(requested, got) {
		return fmt.Errorf("block_integrity: block hash mismatch requested %s got %s at %s",
			requested, got, blockTag)
	}
	return nil
}

// verifyTxBelongsToBlock checks a loaded tx is anchored to the block being scanned (non-verbose path).
func verifyTxBelongsToBlock(tx *models.Transaction, block *models.Block, requestedTxID, blockTag string) error {
	if tx == nil || block == nil {
		return fmt.Errorf("block_integrity: nil tx or block at %s", blockTag)
	}
	requestedTxID = strings.TrimSpace(requestedTxID)
	gotTxID := strings.TrimSpace(tx.TxID)
	if requestedTxID == "" {
		return fmt.Errorf("block_integrity: empty txid at %s", blockTag)
	}
	if gotTxID == "" {
		return fmt.Errorf("block_integrity: transaction response missing txid for %s at %s", requestedTxID, blockTag)
	}
	if !strings.EqualFold(requestedTxID, gotTxID) {
		return fmt.Errorf("block_integrity: txid mismatch requested %s got %s at %s",
			requestedTxID, gotTxID, blockTag)
	}
	blockHash := strings.TrimSpace(block.Hash)
	txBlockHash := strings.TrimSpace(tx.BlockHash)
	if txBlockHash != "" && !strings.EqualFold(txBlockHash, blockHash) {
		return fmt.Errorf("block_integrity: tx %s blockhash %s != block %s at %s",
			gotTxID, txBlockHash, blockHash, blockTag)
	}
	if tx.BlockHeight > 0 && block.Height > 0 && tx.BlockHeight != block.Height {
		return fmt.Errorf("block_integrity: tx %s height %d != block %d at %s",
			gotTxID, tx.BlockHeight, block.Height, blockTag)
	}
	return nil
}

// Phase-A gate (see open_gateway docs/BLOCK_INTEGRITY_CHECK.md): fail with ErrorReason prefix block_integrity:.
// Call after getblock, before any extract.
func verifyBlockPackageIntegrity(blockTag string, wantHeight uint64, block *models.Block) error {
	if block == nil {
		return fmt.Errorf("block_integrity: nil block at %s", blockTag)
	}
	if wantHeight > 0 && block.Height != wantHeight {
		return fmt.Errorf("block_integrity: block height mismatch got %d want %d at %s",
			block.Height, wantHeight, blockTag)
	}
	if strings.TrimSpace(block.Hash) == "" {
		return fmt.Errorf("block_integrity: block hash missing at %s", blockTag)
	}
	rpcEntries := block.RpcTxEntryCount()
	if rpcEntries == 0 && block.OnChainTxCount() == 0 {
		return nil
	}
	if len(block.TxDetails) > 0 && len(block.TxIDs) > 0 {
		return fmt.Errorf("block_integrity: mixed verbose tx and txid entries at %s", blockTag)
	}
	if block.NTx > 0 && uint64(rpcEntries) != block.NTx {
		return fmt.Errorf("block_integrity: nTx=%d but %d tx entries in rpc response at %s",
			block.NTx, rpcEntries, blockTag)
	}
	declared := block.OnChainTxCount()
	if len(block.TxDetails) > 0 {
		if len(block.TxDetails) != declared {
			return fmt.Errorf("block_integrity: declared %d txs but %d verbose objects at %s",
				declared, len(block.TxDetails), blockTag)
		}
		seen := make(map[string]struct{}, len(block.TxDetails))
		for _, tx := range block.TxDetails {
			if tx == nil {
				return fmt.Errorf("block_integrity: nil tx object at %s", blockTag)
			}
			txid := strings.TrimSpace(tx.TxID)
			if txid == "" {
				return fmt.Errorf("block_integrity: missing txid in verbose block at %s", blockTag)
			}
			if err := markUniqueTxID(seen, txid, blockTag); err != nil {
				return err
			}
		}
		return nil
	}
	if len(block.TxIDs) != declared {
		return fmt.Errorf("block_integrity: declared %d txs but %d txids at %s",
			declared, len(block.TxIDs), blockTag)
	}
	seen := make(map[string]struct{}, len(block.TxIDs))
	for _, raw := range block.TxIDs {
		txid := strings.TrimSpace(raw)
		if txid == "" {
			return fmt.Errorf("block_integrity: empty txid in block at %s", blockTag)
		}
		if err := markUniqueTxID(seen, txid, blockTag); err != nil {
			return err
		}
	}
	return nil
}

func markUniqueTxID(seen map[string]struct{}, txid, blockTag string) error {
	key := strings.ToLower(txid)
	if _, dup := seen[key]; dup {
		return fmt.Errorf("block_integrity: duplicate txid %s at %s", txid, blockTag)
	}
	seen[key] = struct{}{}
	return nil
}

func verifyLoadedTxCount(blockTag string, block *models.Block, loaded int, loadErr error) error {
	if loadErr != nil {
		return fmt.Errorf("block_integrity: %w at %s", loadErr, blockTag)
	}
	expected := block.OnChainTxCount()
	if expected > 0 && loaded != expected {
		return fmt.Errorf("block_integrity: loaded %d txs expected %d at %s", loaded, expected, blockTag)
	}
	return nil
}
