package scanner

import (
	"sync"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	adaptscanner "github.com/godaddy-x/wallet-adapter/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
)

func (bs *BtcBlockScanner) getAccountTargetCache() *sync.Map {
	if bs == nil {
		return nil
	}
	bs.accountCacheMu.RLock()
	defer bs.accountCacheMu.RUnlock()
	return bs.accountTargetCache
}

func (bs *BtcBlockScanner) clearAccountTargetCache() {
	bs.accountCacheMu.Lock()
	bs.accountTargetCache = nil
	bs.accountCacheMu.Unlock()
}

// ensureAccountTargetCacheForCall returns true when this call created the cache and must clear it.
func (bs *BtcBlockScanner) ensureAccountTargetCacheForCall() bool {
	bs.accountCacheMu.Lock()
	defer bs.accountCacheMu.Unlock()
	if bs.accountTargetCache != nil {
		return false
	}
	bs.accountTargetCache = &sync.Map{}
	return true
}

func scanTargetAccountID(param *types.ScanTargetParam, addr string) string {
	if param == nil {
		return ""
	}
	v, ok := param.ScanTarget[addr]
	if !ok || v == nil {
		return ""
	}
	id, ok := v.(string)
	if !ok {
		return ""
	}
	return id
}

func storeAccountTargetCache(cache *sync.Map, addrs map[string]struct{}, param *types.ScanTargetParam) {
	if cache == nil || len(addrs) == 0 {
		return
	}
	for addr := range addrs {
		cache.Store(addr, scanTargetAccountID(param, addr))
	}
}

func (bs *BtcBlockScanner) newAccountScanTargetParam(addrs map[string]struct{}) types.ScanTargetParam {
	param := types.ScanTargetParam{
		Symbol:           bs.wm.Symbol(),
		ScanTarget:       make(map[string]interface{}, len(addrs)),
		ScanTargetType:   types.ScanTargetTypeAccountAddress,
	}
	for addr := range addrs {
		param.ScanTarget[addr] = nil
	}
	return param
}

func (bs *BtcBlockScanner) queryAccountTarget(targetFunc adaptscanner.BlockScanTargetFunc, addr string) string {
	addr = normalizeScanAddress(addr)
	if addr == "" {
		return ""
	}
	if cache := bs.getAccountTargetCache(); cache != nil {
		if v, ok := cache.Load(addr); ok {
			if id, ok := v.(string); ok {
				return id
			}
			return ""
		}
	}
	if targetFunc == nil || bs.wm == nil {
		return ""
	}
	param := bs.newAccountScanTargetParam(map[string]struct{}{addr: {}})
	if err := targetFunc(&param); err != nil {
		return ""
	}
	id := scanTargetAccountID(&param, addr)
	if cache := bs.getAccountTargetCache(); cache != nil {
		cache.Store(addr, id)
	}
	return id
}

// beginBlockAccountTargetCache loads all block addresses in one scanTargetFunc call;
// Mongo batching is handled inside the host scanner's scanTargetFunc / GetAddresses.
func (bs *BtcBlockScanner) beginBlockAccountTargetCache(
	block *models.Block,
	txIndex map[string]*models.Transaction,
	targetFunc adaptscanner.BlockScanTargetFunc,
) {
	cache := bs.getAccountTargetCache()
	if cache == nil || targetFunc == nil || bs.wm == nil {
		return
	}
	addrs := blockCandidateAddressSet(block, txIndex)
	if len(addrs) == 0 {
		return
	}
	param := bs.newAccountScanTargetParam(addrs)
	if err := targetFunc(&param); err != nil {
		return
	}
	storeAccountTargetCache(cache, addrs, &param)
}

func (bs *BtcBlockScanner) blockHasManagedCandidate(
	block *models.Block,
	txIndex map[string]*models.Transaction,
	targetFunc adaptscanner.BlockScanTargetFunc,
) bool {
	for addr := range blockCandidateAddressSet(block, txIndex) {
		if bs.queryAccountTarget(targetFunc, addr) != "" {
			return true
		}
	}
	return false
}

func (bs *BtcBlockScanner) txTouchesManaged(trx *models.Transaction, txIndex map[string]*models.Transaction, targetFunc adaptscanner.BlockScanTargetFunc) bool {
	if trx == nil {
		return false
	}
	for addr := range candidateAddressSet(trx, txIndex) {
		if bs.queryAccountTarget(targetFunc, addr) != "" {
			return true
		}
	}
	return false
}

func (bs *BtcBlockScanner) vinTouchesManagedTarget(
	input *models.Vin,
	txIndex map[string]*models.Transaction,
	targetFunc adaptscanner.BlockScanTargetFunc,
) bool {
	if input == nil {
		return false
	}
	if addr := vinCandidateAddress(input, txIndex); addr != "" {
		return bs.queryAccountTarget(targetFunc, addr) != ""
	}
	if addr := normalizeScanAddress(input.Addr); addr != "" {
		return bs.queryAccountTarget(targetFunc, addr) != ""
	}
	return false
}

func (bs *BtcBlockScanner) buildManagedScanTargetParam(
	trx *models.Transaction,
	txIndex map[string]*models.Transaction,
	targetFunc adaptscanner.BlockScanTargetFunc,
) (types.ScanTargetParam, bool) {
	addrs := candidateAddressSet(trx, txIndex)
	param := types.ScanTargetParam{
		Symbol:         bs.wm.Symbol(),
		ScanTarget:     make(map[string]interface{}, len(addrs)),
		ScanTargetType: types.ScanTargetTypeAccountAddress,
	}
	for addr := range addrs {
		if id := bs.queryAccountTarget(targetFunc, addr); id != "" {
			param.ScanTarget[addr] = id
		}
	}
	return param, managedScanTargets(param) > 0
}
