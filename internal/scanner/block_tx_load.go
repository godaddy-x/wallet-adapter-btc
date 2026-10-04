package scanner

import (
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

func blockScanTxTotal(block *models.Block) uint64 {
	if block == nil {
		return 0
	}
	n := block.OnChainTxCount()
	if n > 0 {
		return uint64(n)
	}
	return uint64(block.RpcTxEntryCount())
}

func stampTxBlockContext(tx *models.Transaction, block *models.Block) {
	if tx == nil || block == nil {
		return
	}
	tx.BlockHeight = block.Height
	tx.BlockHash = block.Hash
	tx.Blocktime = int64(block.Time)
}

// resolveBlockTxList returns all transactions for scan (verbose in block or loaded by txid).
func resolveBlockTxList(wm *manager.WalletManager, block *models.Block, txIndex map[string]*models.Transaction) ([]*models.Transaction, error) {
	if block == nil {
		return nil, fmt.Errorf("block is nil")
	}
	if len(block.TxDetails) > 0 {
		return block.TxDetails, nil
	}
	blockTag := fmt.Sprintf("height=%d hash=%s", block.Height, block.Hash)
	txList := make([]*models.Transaction, 0, len(block.TxIDs))
	for _, txid := range block.TxIDs {
		txid = strings.TrimSpace(txid)
		tx, err := wm.GetTransaction(txid)
		if err != nil {
			return nil, fmt.Errorf("missing verbose tx %s: %w", txid, err)
		}
		if err := verifyTxBelongsToBlock(tx, block, txid, blockTag); err != nil {
			return nil, err
		}
		stampTxBlockContext(tx, block)
		storeTxIndex(txIndex, tx)
		txList = append(txList, tx)
	}
	return txList, nil
}
