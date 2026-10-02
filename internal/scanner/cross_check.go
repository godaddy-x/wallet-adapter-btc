package scanner

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	adapter "github.com/godaddy-x/wallet-adapter"
	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

const (
	crossCheckPeerTimeout    = 45 * time.Second
	crossCheckPeerCloseGrace = crossCheckPeerTimeout + 15*time.Second
	crossCheckTxNotOnPeer    = "cross_check: transaction not found on peer"
	crossCheckMinConfirmations = uint64(1)
)

type crossCheckPeer struct {
	url string
	wm  *manager.WalletManager
}

type crossCheckPeerGen struct {
	peers []crossCheckPeer
	wg    sync.WaitGroup
}

func peerConfigFromBase(base *config.WalletConfig, peerURL string) adapter.MapConfig {
	m := adapter.MapConfig{
		"serverAPI":    peerURL,
		"broadcastAPI": peerURL,
	}
	if base == nil {
		return m
	}
	if base.RpcUser != "" {
		m["rpcUser"] = base.RpcUser
	}
	if base.RpcPassword != "" {
		m["rpcPassword"] = base.RpcPassword
	}
	if base.Network != "" {
		m["network"] = base.Network
	}
	if base.IsTestNet {
		m["isTestNet"] = "true"
	}
	if base.IsRegtest {
		m["isRegtest"] = "true"
	}
	if base.RPCServerType == config.RPCServerExplorer {
		m["rpcServerType"] = "1"
	}
	return m
}

func dialCrossCheckPeer(symbol string, base *config.WalletConfig, rpcURL string) (*manager.WalletManager, error) {
	wm := &manager.WalletManager{Config: config.NewConfig(symbol)}
	err := wm.LoadAssetsConfig(peerConfigFromBase(base, rpcURL))
	if err != nil {
		return nil, err
	}
	return wm, nil
}

func closeCrossCheckPeerGen(gen *crossCheckPeerGen) {
	if gen == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		gen.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(crossCheckPeerCloseGrace):
	}
}

// SetVerifyAPIs rebuilds peer RPC clients from nodeConfig.verifyAPIs (after LoadAssetsConfig).
func (bs *BtcBlockScanner) SetVerifyAPIs(urls []string) {
	if bs == nil || bs.wm == nil || bs.wm.Config == nil {
		return
	}
	symbol := bs.wm.Config.Symbol
	base := bs.wm.Config

	newPeers := make([]crossCheckPeer, 0, len(urls))
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		wm, err := dialCrossCheckPeer(symbol, base, u)
		if err != nil {
			log.Printf("wallet-adapter-btc: verifyAPI peer dial failed symbol=%s url=%s err=%v",
				symbol, redactPeerURL(u), err)
			continue
		}
		newPeers = append(newPeers, crossCheckPeer{url: u, wm: wm})
	}

	newGen := &crossCheckPeerGen{peers: newPeers}
	bs.crossCheckMu.Lock()
	old := bs.crossCheckGen
	bs.crossCheckGen = newGen
	bs.crossCheckMu.Unlock()
	if old != nil {
		go closeCrossCheckPeerGen(old)
	}
}

func (bs *BtcBlockScanner) CrossCheckEnabled() bool {
	if bs == nil {
		return false
	}
	bs.crossCheckMu.RLock()
	gen := bs.crossCheckGen
	bs.crossCheckMu.RUnlock()
	return gen != nil && len(gen.peers) > 0
}

func (bs *BtcBlockScanner) VerifyBeforePromote(ctx context.Context, req adapter.CrossCheckRequest) (adapter.CrossCheckResult, error) {
	if strings.TrimSpace(req.TxID) == "" {
		return adapter.CrossCheckResult{OK: false, Reason: "cross_check: empty txID"}, nil
	}
	if req.BlockHeight == 0 || strings.TrimSpace(req.BlockHash) == "" {
		return adapter.CrossCheckResult{OK: false, Reason: "cross_check: incomplete anchor"}, nil
	}

	bs.crossCheckMu.RLock()
	gen := bs.crossCheckGen
	if gen != nil {
		gen.wg.Add(1)
	}
	bs.crossCheckMu.RUnlock()
	if gen == nil {
		return adapter.CrossCheckResult{}, fmt.Errorf("cross_check: no usable peer")
	}
	defer gen.wg.Done()

	var lastErr error
	var lastPeerURL string
	for i, peer := range gen.peers {
		if peer.wm == nil {
			continue
		}
		lastPeerURL = peer.url
		pctx, cancel := context.WithTimeout(ctx, crossCheckPeerTimeout)
		ok, reason, err := verifyBTCAnchorOnPeer(pctx, peer.wm, req)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			return peerCrossCheckResult(i, peer.url, true, ""), nil
		}
		return peerCrossCheckResult(i, peer.url, false, reason), nil
	}
	if lastErr != nil {
		return adapter.CrossCheckResult{}, fmt.Errorf("cross_check: all peers failed lastPeer=%s: %w",
			redactPeerURL(lastPeerURL), lastErr)
	}
	return adapter.CrossCheckResult{}, fmt.Errorf("cross_check: no usable peer")
}

func peerCrossCheckResult(index int, peerURL string, ok bool, reason string) adapter.CrossCheckResult {
	return adapter.CrossCheckResult{
		OK: ok, Reason: reason, PeerIndex: index, PeerURL: redactPeerURL(peerURL),
	}
}

func verifyBTCAnchorOnPeer(ctx context.Context, wm *manager.WalletManager, req adapter.CrossCheckRequest) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	txid := strings.TrimSpace(req.TxID)
	tx, err := wm.GetTransaction(txid)
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "not found") || strings.Contains(msg, "no such mempool") {
			return false, crossCheckTxNotOnPeer, nil
		}
		return false, "", err
	}
	if tx == nil {
		return false, crossCheckTxNotOnPeer, nil
	}
	return matchBTCAnchor(wm, txid, req, tx)
}

func matchBTCAnchor(wm *manager.WalletManager, txid string, req adapter.CrossCheckRequest, tx *models.Transaction) (bool, string, error) {
	peerTxID := strings.TrimSpace(tx.TxID)
	if peerTxID != "" && !strings.EqualFold(peerTxID, txid) {
		return false, fmt.Sprintf("cross_check: txid mismatch peer=%s", peerTxID), nil
	}
	blockHash := strings.TrimSpace(tx.BlockHash)
	if blockHash == "" {
		return false, "cross_check: transaction not included in block (unconfirmed or dropped)", nil
	}
	if tx.Confirmations < crossCheckMinConfirmations {
		return false, fmt.Sprintf("cross_check: confirmations too low peer=%d need>=%d",
			tx.Confirmations, crossCheckMinConfirmations), nil
	}
	expectedHash := strings.TrimSpace(req.BlockHash)
	if !strings.EqualFold(blockHash, expectedHash) {
		return false, fmt.Sprintf("cross_check: blockhash mismatch peer=%s expected=%s", blockHash, expectedHash), nil
	}
	if wm != nil && req.BlockHeight > 0 {
		hashAtHeight, err := wm.GetBlockHash(req.BlockHeight)
		if err != nil {
			return false, "", err
		}
		hashAtHeight = strings.TrimSpace(hashAtHeight)
		if hashAtHeight == "" {
			return false, "cross_check: block hash missing at height on peer", nil
		}
		if !strings.EqualFold(hashAtHeight, expectedHash) {
			return false, fmt.Sprintf("cross_check: height %d hash mismatch peer=%s expected=%s",
				req.BlockHeight, hashAtHeight, expectedHash), nil
		}
	}
	if tx.BlockHeight > 0 && tx.BlockHeight != req.BlockHeight {
		return false, fmt.Sprintf("cross_check: block height mismatch peer=%d expected=%d", tx.BlockHeight, req.BlockHeight), nil
	}
	return true, "", nil
}

// redactPeerURL masks userinfo, query, and the last path segment (typical API key).
func redactPeerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err == nil && u.Scheme != "" && u.Host != "" {
		return formatRedactedURL(u)
	}
	return redactPeerURLWithoutParse(raw)
}

func formatRedactedURL(u *url.URL) string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	if u.User != nil {
		b.WriteString("***@")
	}
	b.WriteString(u.Host)
	appendRedactedPath(&b, strings.Trim(u.EscapedPath(), "/"))
	if u.RawQuery != "" {
		b.WriteString("?***")
	}
	return b.String()
}

func redactPeerURLWithoutParse(raw string) string {
	schemeIdx := strings.Index(raw, "://")
	if schemeIdx < 0 {
		if q := strings.Index(raw, "?"); q >= 0 {
			return raw[:q] + "?***"
		}
		return raw
	}
	var b strings.Builder
	b.WriteString(raw[:schemeIdx+3])
	rest := raw[schemeIdx+3:]
	if at := strings.Index(rest, "@"); at >= 0 {
		b.WriteString("***@")
		rest = rest[at+1:]
	}
	hostEnd := strings.IndexAny(rest, "/?#")
	if hostEnd < 0 {
		b.WriteString(rest)
		return b.String()
	}
	b.WriteString(rest[:hostEnd])
	tail := rest[hostEnd:]
	path := tail
	if q := strings.IndexAny(path, "?#"); q >= 0 {
		path = path[:q]
	}
	appendRedactedPath(&b, strings.Trim(path, "/"))
	if strings.Contains(tail, "?") {
		b.WriteString("?***")
	}
	return b.String()
}

func appendRedactedPath(b *strings.Builder, path string) {
	if path == "" {
		return
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		b.WriteByte('/')
		b.WriteString(path[:i])
	}
	b.WriteString("/***")
}
