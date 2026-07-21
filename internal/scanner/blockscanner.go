package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	adaptscanner "github.com/godaddy-x/wallet-adapter/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

const maxBlocksPerScanRound = 128

// btcAddressNetOutputIndex marks a combined per-address extract (vin+vout or multi-leg)
// so open_scanner nets balance in one tradeFlowPrep pass instead of separate debits/credits.
const btcAddressNetOutputIndex int64 = -3

// BtcBlockScanner implements wallet-adapter BlockScanner for Bitcoin UTXO chains.
type BtcBlockScanner struct {
	*adaptscanner.Base
	wm *manager.WalletManager

	scanLoopRunning    atomic.Bool
	scanLoopCursor     atomic.Uint64
	scanLoopHandleFunc func(*types.BlockScanResult)
	scanLoopMu          sync.Mutex
	scanLoopCond        *sync.Cond
	scanLoopPaused      bool
	scanLoopPauseCh     chan struct{}

	scanBlockMu sync.Mutex // serializes ScanBlockWithResult (main loop vs nested Once)

	priorityScanMu  sync.Mutex
	priorityHeights []uint64 // drained by RunScanLoop only (RPC enqueues)

	accountCacheMu       sync.RWMutex
	accountTargetCache   *sync.Map // address → accountID ("" = miss), block-scoped
}

// NewBlockScanner creates BTC block scanner.
func NewBlockScanner(wm *manager.WalletManager) *BtcBlockScanner {
	bs := &BtcBlockScanner{
		Base: adaptscanner.NewBlockScannerBase(),
		wm:   wm,
	}
	bs.scanLoopCond = sync.NewCond(&bs.scanLoopMu)
	return bs
}

func (bs *BtcBlockScanner) ScanBlockOnce(height uint64) (*types.BlockScanResult, error) {
	return bs.ScanBlockWithResult(height)
}

func (bs *BtcBlockScanner) ScanBlockWithResult(height uint64) (*types.BlockScanResult, error) {
	bs.scanBlockMu.Lock()
	defer bs.scanBlockMu.Unlock()

	res := &types.BlockScanResult{
		Height:           height,
		Success:          false,
		FailedTxIDs:      make([]string, 0),
		ExtractData:      make([]*types.ExtractDataItem, 0),
		ContractReceipts: make([]*types.ContractReceiptItem, 0),
	}
	if bs.wm == nil || bs.wm.Config == nil {
		res.ErrorReason = "wallet manager is nil"
		return res, fmt.Errorf("wallet manager is nil")
	}
	res.Symbol = bs.wm.Symbol()

	hash, err := bs.wm.GetBlockHash(height)
	if err != nil {
		res.ErrorReason = err.Error()
		return res, err
	}
	block, err := bs.wm.GetBlock(hash)
	if err != nil {
		res.ErrorReason = err.Error()
		return res, err
	}
	latest, _ := bs.wm.GetBlockHeight()
	res.BlockHash = block.Hash
	res.NetworkBlockHeight = latest
	res.Header = &types.BlockHeader{
		Hash:              block.Hash,
		Merkleroot:        block.Merkleroot,
		Previousblockhash: block.Previousblockhash,
		Height:            block.Height,
		Version:           block.Version,
		Time:              block.Time,
		Symbol:            bs.wm.Symbol(),
	}
	if latest >= height {
		res.Header.Confirmations = latest - height + 1
	}

	txIndex := buildBlockTxIndex(block)
	prevoutCache := make(map[string]*models.Transaction)
	targetFunc := bs.ScanTargetFunc
	ownedCache := bs.ensureAccountTargetCacheForCall()
	if ownedCache {
		defer bs.clearAccountTargetCache()
	}

	txList := block.TxDetails
	if len(txList) > 0 {
		res.TxTotal = uint64(len(txList))
	} else {
		res.TxTotal = uint64(len(block.TxIDs))
		txList = make([]*models.Transaction, 0, len(block.TxIDs))
		for _, txid := range block.TxIDs {
			tx, err := bs.wm.GetTransaction(txid)
			if err != nil {
				res.FailedTxIDs = append(res.FailedTxIDs, txid)
				continue
			}
			tx.BlockHeight = block.Height
			tx.BlockHash = block.Hash
			tx.Blocktime = int64(block.Time)
			txIndex[txid] = tx
			txList = append(txList, tx)
		}
	}
	bs.beginBlockAccountTargetCache(block, txIndex, targetFunc)
	if !bs.blockHasManagedCandidate(block, txIndex, targetFunc) {
		finalizeBlockScanResult(res)
		return res, nil
	}

	for _, tx := range txList {
		if tx == nil {
			continue
		}
		if !bs.txTouchesManaged(tx, txIndex, targetFunc) {
			continue
		}
		bs.appendBlockTxExtract(res, tx, tx.TxID, txIndex, prevoutCache)
	}
	finalizeBlockScanResult(res)
	return res, nil
}

func buildBlockTxIndex(block *models.Block) map[string]*models.Transaction {
	index := make(map[string]*models.Transaction, len(block.TxDetails))
	for _, tx := range block.TxDetails {
		if tx != nil && tx.TxID != "" {
			index[tx.TxID] = tx
		}
	}
	return index
}

func finalizeBlockScanResult(res *types.BlockScanResult) {
	res.TxFailed = uint64(len(res.FailedTxIDs))
	if len(res.FailedTxIDs) > 0 {
		res.Success = false
		res.ErrorReason = formatBlockScanExtractionError(res)
		return
	}
	res.Success = true
}

func formatBlockScanExtractionError(res *types.BlockScanResult) string {
	if res == nil {
		return "transaction extraction failed"
	}
	n := len(res.FailedTxIDs)
	msg := fmt.Sprintf("block %d: %d transaction(s) failed extraction", res.Height, n)
	if n == 0 {
		return msg
	}
	const maxShow = 5
	show := res.FailedTxIDs
	if len(show) > maxShow {
		show = show[:maxShow]
	}
	msg += ": " + strings.Join(show, ", ")
	if n > maxShow {
		msg += fmt.Sprintf(" (+%d more)", n-maxShow)
	}
	if res.FailedTxDetail != "" {
		msg += "; " + res.FailedTxDetail
	}
	return msg
}

func (bs *BtcBlockScanner) GetCurrentBlockHeader() (*types.BlockHeader, error) {
	height, err := bs.wm.GetBlockHeight()
	if err != nil {
		return nil, err
	}
	return bs.wm.GetBlockHeaderByHeight(height)
}

func (bs *BtcBlockScanner) GetBlockHash(height uint64) (string, error) {
	return bs.wm.GetBlockHash(height)
}

func (bs *BtcBlockScanner) GetGlobalMaxBlockHeight() uint64 {
	h, _ := bs.GetGlobalMaxBlockHeightWithError()
	return h
}

// GetGlobalMaxBlockHeightWithError returns latest block height with RPC error details.
func (bs *BtcBlockScanner) GetGlobalMaxBlockHeightWithError() (uint64, error) {
	if bs.wm == nil || bs.wm.Config == nil {
		return 0, fmt.Errorf("wallet manager config is nil")
	}
	return bs.wm.GetBlockHeight()
}

func (bs *BtcBlockScanner) tipRPCFailureResult(phase, errMsg string) *types.BlockScanResult {
	symbol := ""
	if bs.wm != nil && bs.wm.Config != nil {
		symbol = bs.wm.Config.Symbol
	}
	cursor := bs.scanLoopCursor.Load()
	next := cursor + 1
	return &types.BlockScanResult{
		Symbol:           symbol,
		Height:           next,
		Success:          false,
		ErrorReason:      fmt.Sprintf("%s (phase=%s cursor=%d nextScanHeight=%d rpc=getblockcount)", errMsg, phase, cursor, next),
		ExtractData:      make([]*types.ExtractDataItem, 0),
		ContractReceipts: make([]*types.ContractReceiptItem, 0),
		FailedTxIDs:      make([]string, 0),
	}
}

func (bs *BtcBlockScanner) notifyTipRPCFailure(handle func(*types.BlockScanResult), phase string, rpcErr error) {
	if handle == nil {
		return
	}
	errMsg := ""
	if rpcErr != nil {
		errMsg = rpcErr.Error()
	} else {
		errMsg = "cannot get latest block height (returned 0)"
	}
	handle(bs.tipRPCFailureResult(phase, errMsg))
}

func (bs *BtcBlockScanner) ExtractTransactionAndReceiptData(txid string, scanTargetFunc adaptscanner.BlockScanTargetFunc) ([]*types.ExtractDataItem, []*types.ContractReceiptItem, error) {
	tx, err := bs.wm.GetTransaction(txid)
	if err != nil {
		return nil, nil, err
	}
	items, err := bs.extractTransaction(tx, nil, scanTargetFunc, nil)
	if err != nil {
		return nil, nil, err
	}
	return items, nil, nil
}

func (bs *BtcBlockScanner) GetBalanceByAddress(address ...string) ([]*types.Balance, error) {
	return bs.QueryBalancesConcurrent(bs.wm.Symbol(), address, func(addr string) (string, string, string, error) {
		balances, err := bs.wm.GetBalanceByAddresses(addr)
		if err != nil || len(balances) == 0 {
			return "0", "0", "0", err
		}
		b := balances[0]
		return b.ConfirmBalance, b.UnconfirmBalance, b.Balance, nil
	}, 20)
}

func (bs *BtcBlockScanner) VerifyTransactionByTxID(txid string, scanTargetFunc adaptscanner.BlockScanTargetFunc, minConfirmations uint64) (*types.TxVerifyResult, error) {
	tx, err := bs.wm.GetTransaction(txid)
	if err != nil {
		return nil, err
	}
	result := &types.TxVerifyResult{
		Symbol:           bs.wm.Symbol(),
		TxID:             txid,
		Verified:         false,
		BlockHash:        tx.BlockHash,
		BlockHeight:      tx.BlockHeight,
		Confirmations:    tx.Confirmations,
		Status:           types.TxStatusSuccess,
		ExtractData:      make([]*types.ExtractDataItem, 0),
		ContractReceipts: make([]*types.ContractReceiptItem, 0),
	}
	if tx.Confirmations < minConfirmations {
		result.Reason = fmt.Sprintf("confirmations %d < required %d", tx.Confirmations, minConfirmations)
		return result, nil
	}
	items, err := bs.extractTransaction(tx, nil, scanTargetFunc, nil)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		result.Reason = "no matched extract data"
		return result, nil
	}
	result.Verified = true
	result.ExtractData = items
	return result, nil
}

func (bs *BtcBlockScanner) VerifyTransactionMatch(txid string, expected *types.TxVerifyExpected, scanTargetFunc adaptscanner.BlockScanTargetFunc, minConfirmations uint64) (*types.TxVerifyMatchResult, error) {
	verify, err := bs.VerifyTransactionByTxID(txid, scanTargetFunc, minConfirmations)
	if err != nil {
		return nil, err
	}
	out := &types.TxVerifyMatchResult{
		TxID:     txid,
		Verified: verify.Verified,
		Reason:   verify.Reason,
	}
	if !verify.Verified {
		return out, nil
	}
	if expected != nil && expected.BlockHash != "" && expected.BlockHash != verify.BlockHash {
		out.Verified = false
		out.Reason = "block hash mismatch"
	}
	return out, nil
}

func (bs *BtcBlockScanner) ResetScanHeight(height uint64) error {
	if height == 0 {
		bs.scanLoopCursor.Store(0)
		return nil
	}
	bs.scanLoopCursor.Store(height - 1)
	return nil
}

func (bs *BtcBlockScanner) RunScanLoop(params adaptscanner.ScanLoopParams) error {
	if bs.wm == nil {
		return fmt.Errorf("wallet manager is nil")
	}
	if params.Interval <= 0 {
		params.Interval = 5 * time.Second
	}
	if !bs.scanLoopRunning.CompareAndSwap(false, true) {
		return fmt.Errorf("RunScanLoop already running")
	}
	defer bs.scanLoopRunning.Store(false)

	bs.scanLoopHandleFunc = params.HandleBlock
	if params.StartHeight == 0 {
		bs.scanLoopCursor.Store(0)
	} else {
		bs.scanLoopCursor.Store(params.StartHeight - 1)
	}

	for {
		bs.blockIfPaused()
		bs.processPriorityScan()

		latest, rpcErr := bs.GetGlobalMaxBlockHeightWithError()
		if rpcErr != nil || latest == 0 {
			bs.notifyTipRPCFailure(params.HandleBlock, "main_loop", rpcErr)
			bs.sleep(params.Interval)
			continue
		}
		cursor := bs.scanLoopCursor.Load()
		scanFrom := cursor + 1
		if scanFrom > latest {
			bs.sleep(params.Interval)
			continue
		}
		scanTo := latest
		if blocks := scanTo - scanFrom + 1; blocks > maxBlocksPerScanRound {
			scanTo = scanFrom + maxBlocksPerScanRound - 1
		}
		for h := scanFrom; h <= scanTo; h++ {
			bs.blockIfPaused()
			bs.processPriorityScan()

			res, err := bs.ScanBlockWithResult(h)
			if params.HandleBlock != nil && res != nil {
				params.HandleBlock(res)
			}
			if err != nil || res == nil || !res.Success {
				break
			}
			bs.scanLoopCursor.Store(h)
		}
		// Catch up aggressively while blocks remain; sleep only when at chain tip.
		if scanTo < latest {
			continue
		}
		bs.sleep(params.Interval)
	}
}

// ScanBlockPrioritize enqueues heights for RunScanLoop (same goroutine as HandleBlock).
func (bs *BtcBlockScanner) ScanBlockPrioritize(heights []uint64) error {
	if !bs.scanLoopRunning.Load() {
		return fmt.Errorf("RunScanLoop is not running")
	}
	if len(heights) == 0 {
		return nil
	}
	bs.priorityScanMu.Lock()
	defer bs.priorityScanMu.Unlock()
	seen := make(map[uint64]struct{}, len(bs.priorityHeights)+len(heights))
	for _, h := range bs.priorityHeights {
		seen[h] = struct{}{}
	}
	for _, h := range heights {
		if _, ok := seen[h]; ok {
			continue
		}
		bs.priorityHeights = append(bs.priorityHeights, h)
		seen[h] = struct{}{}
	}
	return nil
}

func (bs *BtcBlockScanner) processPriorityScan() {
	bs.priorityScanMu.Lock()
	heights := append([]uint64(nil), bs.priorityHeights...)
	bs.priorityHeights = nil
	bs.priorityScanMu.Unlock()
	if len(heights) == 0 {
		return
	}
	handle := bs.scanLoopHandleFunc
	for _, h := range heights {
		bs.blockIfPaused()
		res, err := bs.ScanBlockWithResult(h)
		if err != nil {
			if res == nil {
				res = &types.BlockScanResult{
					Height:           h,
					Success:          false,
					ErrorReason:      err.Error(),
					ExtractData:      make([]*types.ExtractDataItem, 0),
					ContractReceipts: make([]*types.ContractReceiptItem, 0),
					FailedTxIDs:      make([]string, 0),
				}
				if bs.wm != nil {
					res.Symbol = bs.wm.Symbol()
				}
			}
		}
		if res != nil {
			res.Once = true
			if handle != nil {
				handle(res)
			}
		}
	}
}

func (bs *BtcBlockScanner) blockIfPaused() {
	bs.scanLoopMu.Lock()
	for bs.scanLoopPaused {
		bs.scanLoopCond.Wait()
	}
	bs.scanLoopMu.Unlock()
}

func (bs *BtcBlockScanner) sleep(interval time.Duration) {
	select {
	case <-time.After(interval):
	case <-bs.getPauseCh():
	}
}

func (bs *BtcBlockScanner) getPauseCh() <-chan struct{} {
	bs.scanLoopMu.Lock()
	defer bs.scanLoopMu.Unlock()
	if bs.scanLoopPauseCh == nil {
		bs.scanLoopPauseCh = make(chan struct{})
	}
	return bs.scanLoopPauseCh
}

func (bs *BtcBlockScanner) Run() error {
	bs.scanLoopMu.Lock()
	if bs.scanLoopPaused {
		bs.scanLoopPaused = false
		bs.scanLoopPauseCh = make(chan struct{})
		bs.scanLoopCond.Broadcast()
	}
	bs.scanLoopMu.Unlock()
	return nil
}

func (bs *BtcBlockScanner) Pause() error {
	bs.scanLoopMu.Lock()
	if !bs.scanLoopPaused {
		bs.scanLoopPaused = true
		if bs.scanLoopPauseCh != nil {
			close(bs.scanLoopPauseCh)
		}
	}
	bs.scanLoopMu.Unlock()
	return nil
}

func (bs *BtcBlockScanner) appendBlockTxExtract(res *types.BlockScanResult, tx *models.Transaction, txid string, txIndex map[string]*models.Transaction, prevoutCache map[string]*models.Transaction) {
	items, err := bs.extractTransaction(tx, txIndex, bs.ScanTargetFunc, prevoutCache)
	if err != nil {
		res.FailedTxIDs = append(res.FailedTxIDs, txid)
		if res.FailedTxDetail == "" {
			res.FailedTxDetail = fmt.Sprintf("txid=%s height=%d: %v", txid, res.Height, err)
		}
		return
	}
	res.ExtractData = append(res.ExtractData, items...)
	if len(items) > 0 {
		res.ExtractedTxs++
	}
}

func (bs *BtcBlockScanner) extractTransaction(trx *models.Transaction, txIndex map[string]*models.Transaction, scanTargetFunc adaptscanner.BlockScanTargetFunc, prevoutCache map[string]*models.Transaction) ([]*types.ExtractDataItem, error) {
	if trx == nil {
		return nil, fmt.Errorf("transaction is nil")
	}
	if scanTargetFunc == nil {
		return []*types.ExtractDataItem{}, nil
	}
	if !bs.txTouchesManaged(trx, txIndex, scanTargetFunc) {
		return []*types.ExtractDataItem{}, nil
	}
	blockScan := txIndex != nil
	bs.fillVinAddresses(trx, txIndex, prevoutCache, scanTargetFunc, blockScan)
	param, matched := bs.buildManagedScanTargetParam(trx, txIndex, scanTargetFunc)
	if err := validateVinAddresses(trx, blockScan, param); err != nil {
		return nil, err
	}
	if !matched {
		return []*types.ExtractDataItem{}, nil
	}

	items := make([]*types.ExtractDataItem, 0)
	createAt := time.Now().Unix()
	coin := types.Coin{Symbol: bs.wm.Symbol()}
	network := bs.wm.Config.NetworkName()

	legs := make(map[string]*addrLeg)
	legKey := func(sourceKey, addr string) string { return sourceKey + "\x00" + addr }
	getLeg := func(sourceKey, addr string) *addrLeg {
		key := legKey(sourceKey, addr)
		if legs[key] == nil {
			legs[key] = &addrLeg{sourceKey: sourceKey}
		}
		return legs[key]
	}

	for _, input := range trx.Vins {
		addr := normalizeScanAddress(input.Addr)
		if addr == "" || !validExtractAmount(input.Value) || param.ScanTarget[addr] == nil {
			continue
		}
		sourceKey := resolveSourceKey(param, addr)
		leg := getLeg(sourceKey, addr)
		leg.vins = append(leg.vins, input)
	}
	for _, output := range trx.Vouts {
		if output.Type == "OP_RETURN" {
			continue
		}
		addr := normalizeScanAddress(output.Addr)
		if addr == "" || !validExtractAmount(output.Value) || param.ScanTarget[addr] == nil {
			continue
		}
		if output.ScriptPubKey != "" && !util.VoutAddressMatchesScript(output.Addr, output.ScriptPubKey, output.Type, network) {
			continue
		}
		sourceKey := resolveSourceKey(param, addr)
		leg := getLeg(sourceKey, addr)
		leg.vouts = append(leg.vouts, output)
	}

	for _, leg := range legs {
		if len(leg.vins) == 0 && len(leg.vouts) == 0 {
			continue
		}

		fromAddrs := make([]string, 0, len(leg.vins))
		fromAmts := make([]string, 0, len(leg.vins))
		txInputs := make([]*types.TxInput, 0, len(leg.vins))
		for _, input := range leg.vins {
			a := normalizeScanAddress(input.Addr)
			fromAddrs = append(fromAddrs, a)
			fromAmts = append(fromAmts, input.Value)
			txInputs = append(txInputs, &types.TxInput{
				SourceTxID:  input.TxID,
				SourceIndex: input.Vout,
				Recharge: types.Recharge{
					Sid:         genSID("in", trx.TxID, input.N, a),
					TxID:        trx.TxID,
					Address:     a,
					Symbol:      bs.wm.Symbol(),
					Coin:        coin,
					Amount:      input.Value,
					Confirm:     int64(trx.Confirmations),
					BlockHash:   trx.BlockHash,
					BlockHeight: trx.BlockHeight,
					Index:       input.N,
					CreateAt:    createAt,
				},
			})
		}

		toAddrs := make([]string, 0, len(leg.vouts))
		toAmts := make([]string, 0, len(leg.vouts))
		txOutputs := make([]*types.TxOutPut, 0, len(leg.vouts))
		var amountSum string
		for _, output := range leg.vouts {
			a := normalizeScanAddress(output.Addr)
			toAddrs = append(toAddrs, a)
			toAmts = append(toAmts, output.Value)
			txOutputs = append(txOutputs, &types.TxOutPut{
				Recharge: types.Recharge{
					Sid:         genSID("out", trx.TxID, output.N, a),
					TxID:        trx.TxID,
					Address:     a,
					Symbol:      bs.wm.Symbol(),
					Coin:        coin,
					Amount:      output.Value,
					Confirm:     int64(trx.Confirmations),
					BlockHash:   trx.BlockHash,
					BlockHeight: trx.BlockHeight,
					Index:       output.N,
					CreateAt:    createAt,
				},
				ExtParam: output.ScriptPubKey,
			})
			if amountSum == "" {
				amountSum = output.Value
			}
		}

		outputIndex := resolveBTCExtractOutputIndex(leg.vins, leg.vouts)
		txAction := inferBTCTxAction(trx, leg, param)
		ed := &types.TxExtractData{
			TxInputs:  txInputs,
			TxOutputs: txOutputs,
			Transaction: &types.Transaction{
				TxID:        trx.TxID,
				Coin:        coin,
				FromAddr:    fromAddrs,
				FromAmt:     fromAmts,
				ToAddr:      toAddrs,
				ToAmt:       toAmts,
				Amount:      amountSum,
				Decimal:     bs.wm.Decimal(),
				Fees:        btcNonFeeLegFees,
				BlockHash:   trx.BlockHash,
				BlockHeight: trx.BlockHeight,
				Confirm:     int64(trx.Confirmations),
				ConfirmTime: trx.Blocktime,
				Status:      types.TxStatusSuccess,
				OutputIndex: outputIndex,
				TxAction:    txAction,
			},
		}
		items = append(items, &types.ExtractDataItem{SourceKey: leg.sourceKey, Data: []*types.TxExtractData{ed}})
	}
	acctCtx := &btcFeeAccountingContext{
		lookup:    bs.TradeOrderLookup,
		symbol:    bs.wm.Symbol(),
		accountID: resolveFeeAccountingAccountID(items),
	}
	if err := appendBTCTransactionFeeItems(trx, &items, coin, createAt, bs.wm.Decimal(), acctCtx); err != nil {
		return nil, err
	}
	if managedScanTargets(param) > 0 && len(items) == 0 {
		return nil, fmt.Errorf("managed transaction produced no extract items: txid=%s", trx.TxID)
	}
	return items, nil
}

func managedScanTargets(param types.ScanTargetParam) int {
	n := 0
	for _, v := range param.ScanTarget {
		if v != nil {
			n++
		}
	}
	return n
}

func validateVinAddresses(trx *models.Transaction, blockScan bool, param types.ScanTargetParam) error {
	if trx == nil {
		return fmt.Errorf("transaction is nil")
	}
	for i, input := range trx.Vins {
		if len(input.Coinbase) > 0 {
			continue
		}
		addr := normalizeScanAddress(input.Addr)
		if blockScan {
			if addr == "" {
				continue
			}
			if param.ScanTarget[addr] == nil {
				continue
			}
		}
		if addr == "" || !validExtractAmount(input.Value) {
			return fmt.Errorf("vin %d missing address/amount: txid=%s prev=%s:%d", i, trx.TxID, input.TxID, input.Vout)
		}
	}
	return nil
}

func resolveBTCExtractOutputIndex(vins []*models.Vin, vouts []*models.Vout) int64 {
	if len(vins) > 0 && len(vouts) > 0 {
		return btcAddressNetOutputIndex
	}
	if len(vins) > 1 || len(vouts) > 1 {
		return btcAddressNetOutputIndex
	}
	if len(vins) == 1 {
		return int64(vins[0].N)
	}
	if len(vouts) == 1 {
		return int64(vouts[0].N)
	}
	return btcAddressNetOutputIndex
}

// GetTransactionBlockHeight returns the block height that contains txid.
func (bs *BtcBlockScanner) GetTransactionBlockHeight(txid string) (uint64, error) {
	if bs.wm == nil {
		return 0, fmt.Errorf("wallet manager is nil")
	}
	tx, err := bs.wm.GetTransaction(txid)
	if err != nil {
		return 0, err
	}
	if tx == nil || tx.BlockHeight == 0 {
		return 0, fmt.Errorf("transaction block height unavailable: txid=%s", txid)
	}
	return tx.BlockHeight, nil
}

// FindTransactionBlockHeight locates txid in blocks [1,maxHeight] when getrawtransaction is unavailable.
func (bs *BtcBlockScanner) FindTransactionBlockHeight(txid string, maxHeight uint64) (uint64, error) {
	if bs.wm == nil {
		return 0, fmt.Errorf("wallet manager is nil")
	}
	txid = strings.TrimSpace(txid)
	if txid == "" || maxHeight == 0 {
		return 0, fmt.Errorf("invalid tx lookup: txid=%s maxHeight=%d", txid, maxHeight)
	}
	if height, err := bs.GetTransactionBlockHeight(txid); err == nil && height > 0 && height <= maxHeight {
		return height, nil
	}
	for h := maxHeight; h >= 1; h-- {
		hash, err := bs.wm.GetBlockHash(h)
		if err != nil {
			continue
		}
		block, err := bs.wm.GetBlock(hash)
		if err != nil || block == nil {
			continue
		}
		for _, id := range block.TxIDs {
			if strings.EqualFold(id, txid) {
				return h, nil
			}
		}
		for _, detail := range block.TxDetails {
			if detail != nil && strings.EqualFold(detail.TxID, txid) {
				return h, nil
			}
		}
	}
	return 0, fmt.Errorf("transaction not found in blocks 1..%d: txid=%s", maxHeight, txid)
}

func (bs *BtcBlockScanner) fillVinAddresses(
	trx *models.Transaction,
	txIndex map[string]*models.Transaction,
	prevoutCache map[string]*models.Transaction,
	scanTargetFunc adaptscanner.BlockScanTargetFunc,
	blockScan bool,
) {
	for _, input := range trx.Vins {
		if input == nil || len(input.Coinbase) > 0 {
			continue
		}
		fromBlockIndex := txIndex != nil && txIndex[input.TxID] != nil
		if blockScan && !bs.vinTouchesManagedTarget(input, txIndex, scanTargetFunc) {
			continue
		}
		if !fromBlockIndex && normalizeScanAddress(input.Addr) != "" && validExtractAmount(input.Value) {
			continue
		}
		preTx := bs.resolveVinPrevoutTx(input, txIndex, prevoutCache)
		if preTx == nil || int(input.Vout) >= len(preTx.Vouts) {
			continue
		}
		out := preTx.Vouts[input.Vout]
		if fromBlockIndex || input.Addr == "" || !validExtractAmount(input.Value) {
			if out.Addr != "" {
				input.Addr = out.Addr
			}
			if validExtractAmount(out.Value) {
				input.Value = out.Value
			}
		}
	}
}

func (bs *BtcBlockScanner) resolveVinPrevoutTx(input *models.Vin, txIndex map[string]*models.Transaction, prevoutCache map[string]*models.Transaction) *models.Transaction {
	if input == nil || input.TxID == "" {
		return nil
	}
	if txIndex != nil {
		if preTx := txIndex[input.TxID]; preTx != nil {
			return preTx
		}
	}
	if prevoutCache != nil {
		if preTx := prevoutCache[input.TxID]; preTx != nil {
			return preTx
		}
	}
	if bs.wm == nil {
		return nil
	}
	if bs.wm.Client == nil && bs.wm.ExplorerClient == nil {
		return nil
	}
	preTx, err := bs.wm.GetTransaction(input.TxID)
	if err != nil || preTx == nil {
		return nil
	}
	if prevoutCache != nil {
		prevoutCache[input.TxID] = preTx
	}
	return preTx
}

func resolveSourceKey(param types.ScanTargetParam, addr string) string {
	if sk, ok := param.ScanTarget[addr].(string); ok && sk != "" {
		return sk
	}
	return addr
}

func normalizeScanAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}

func validExtractAmount(value string) bool {
	amount, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return amount.GreaterThan(decimal.Zero)
}

func genSID(prefix, txid string, index uint64, addr string) string {
	plain := fmt.Sprintf("%s_%s_%d_%s", prefix, txid, index, addr)
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}
