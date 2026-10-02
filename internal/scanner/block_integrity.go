package scanner

import (
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

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
		for _, tx := range block.TxDetails {
			if tx == nil {
				return fmt.Errorf("block_integrity: nil tx object at %s", blockTag)
			}
			if strings.TrimSpace(tx.TxID) == "" {
				return fmt.Errorf("block_integrity: missing txid in verbose block at %s", blockTag)
			}
		}
		return nil
	}
	if len(block.TxIDs) != declared {
		return fmt.Errorf("block_integrity: declared %d txs but %d txids at %s",
			declared, len(block.TxIDs), blockTag)
	}
	for _, txid := range block.TxIDs {
		if strings.TrimSpace(txid) == "" {
			return fmt.Errorf("block_integrity: empty txid in block at %s", blockTag)
		}
	}
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
