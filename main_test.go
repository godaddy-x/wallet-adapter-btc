package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/godaddy-x/wallet-adapter-btc/btc"
	"github.com/godaddy-x/wallet-adapter-btc/internal/rpc"
	btcscanner "github.com/godaddy-x/wallet-adapter-btc/internal/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
)

// testConfigJSON matches local regtest bitcoind (see README / WALLET_ADAPTER_BTC_CONFIG_JSON override).
const testConfigJSON = `{
  "serverAPI": "http://127.0.0.1:18443",
  "broadcastAPI": "http://127.0.0.1:18443",
  "rpcUser": "testuser",
  "rpcPassword": "testpass123",
  "rpcServerType": "0",
  "network": "regtest",
  "isRegtest": "true",
  "addressFormat": "p2wpkh",
  "supportSegWit": "true",
  "enableRBF": "true",
  "feeTargetBlocks": "2",
  "feeEstimateMode": "economical",
  "minFeeRate": "0.00001",
  "minFees": "0.00001",
  "maxTxInputs": "18000",
  "dataDir": "data"
}`

func integrationConfigJSON(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("WALLET_ADAPTER_BTC_CONFIG_JSON")); v != "" {
		return v
	}
	return testConfigJSON
}

func newTestAdapter(t *testing.T) *btc.BtcAdapter {
	t.Helper()
	adapter, err := btc.NewAdapter(integrationConfigJSON(t), "BTC", "Bitcoin", 8)
	if err != nil {
		t.Skipf("skip: local BTC node unavailable: %v (set WALLET_ADAPTER_BTC_CONFIG_JSON or start bitcoind)", err)
	}
	return adapter
}

func skipIfNodeUnavailable(t *testing.T, bs *btcscanner.BtcBlockScanner) {
	t.Helper()
	header, err := bs.GetCurrentBlockHeader()
	if err != nil {
		t.Skipf("skip: local BTC node unavailable: %v (set WALLET_ADAPTER_BTC_CONFIG_JSON or start bitcoind)", err)
	}
	if header == nil || header.Height == 0 {
		t.Skip("skip: invalid block header from local node")
	}
}

// TestNewAdapter demonstrates adapter construction from JSON config (no RPC call).
func TestNewAdapter(t *testing.T) {
	a := newTestAdapter(t)
	if a.GetBlockScanner() == nil {
		t.Fatal("block scanner is nil")
	}
	if a.GetTransactionDecoder() == nil {
		t.Fatal("transaction decoder is nil")
	}
}

// TestStartBlockScanner connects to a local node, reads latest header, and scans genesis block once.
func TestStartBlockScanner(t *testing.T) {
	a := newTestAdapter(t)
	rawScanner := a.GetBlockScanner()
	bs, ok := rawScanner.(*btcscanner.BtcBlockScanner)
	if !ok {
		t.Fatalf("unexpected BlockScanner type: %T", rawScanner)
	}
	skipIfNodeUnavailable(t, bs)

	start := time.Now()
	header, err := bs.GetCurrentBlockHeader()
	if err != nil {
		t.Fatalf("GetCurrentBlockHeader: %v", err)
	}
	t.Logf("latest height=%d hash=%s", header.Height, header.Hash)

	res, err := rawScanner.ScanBlockOnce(0)
	if err != nil {
		t.Fatalf("ScanBlockOnce(0): %v", err)
	}
	t.Logf("scan genesis ok: height=%d hash=%s txTotal=%d extracted=%d elapsed=%s",
		res.Height, res.BlockHash, res.TxTotal, res.ExtractedTxs, time.Since(start))
}

// TestScanBlockWithResultFlow scans a fixed height with a permissive ScanTargetFunc.
func TestScanBlockWithResultFlow(t *testing.T) {
	a := newTestAdapter(t)
	rawScanner := a.GetBlockScanner()
	bs, ok := rawScanner.(*btcscanner.BtcBlockScanner)
	if !ok {
		t.Fatalf("unexpected BlockScanner type: %T", rawScanner)
	}
	skipIfNodeUnavailable(t, bs)

	_ = bs.SetBlockScanTargetFunc(func(target *types.ScanTargetParam) error {
		if target == nil {
			return nil
		}
		for scanTarget := range target.ScanTarget {
			target.ScanTarget[scanTarget] = "test"
		}
		return nil
	})

	height := uint64(100)
	res, err := rawScanner.ScanBlockWithResult(height)
	if err != nil {
		t.Fatalf("ScanBlockWithResult(%d): %v", height, err)
	}
	if res == nil || res.Header == nil {
		t.Fatalf("nil result: %+v", res)
	}
	if res.Height != height {
		t.Fatalf("height: got %d want %d", res.Height, height)
	}
	t.Logf("ScanBlockWithResult ok: height=%d txTotal=%d extracted=%d",
		res.Height, res.TxTotal, res.ExtractedTxs)
}

// TestVerifyTransactionByTxID verifies a coinbase tx from a regtest block (not mainnet genesis).
func TestVerifyTransactionByTxID(t *testing.T) {
	a := newTestAdapter(t)
	rawScanner := a.GetBlockScanner()
	bs, ok := rawScanner.(*btcscanner.BtcBlockScanner)
	if !ok {
		t.Fatalf("unexpected BlockScanner type: %T", rawScanner)
	}
	skipIfNodeUnavailable(t, bs)

	const sampleHeight = uint64(100)
	txID, err := firstTxIDAtHeight(a, sampleHeight)
	if err != nil {
		t.Fatalf("firstTxIDAtHeight(%d): %v", sampleHeight, err)
	}

	_ = bs.SetBlockScanTargetFunc(func(target *types.ScanTargetParam) error {
		if target == nil {
			return nil
		}
		for scanTarget := range target.ScanTarget {
			target.ScanTarget[scanTarget] = "test"
		}
		return nil
	})

	vr, err := rawScanner.VerifyTransactionByTxID(txID, bs.ScanTargetFunc, 0)
	if err != nil {
		if strings.Contains(err.Error(), "txindex") {
			t.Skipf("skip: bitcoind needs -txindex for VerifyTransactionByTxID: %v", err)
		}
		t.Fatalf("VerifyTransactionByTxID: %v", err)
	}
	if vr == nil || vr.TxID != txID {
		t.Fatalf("invalid verify result: %+v", vr)
	}
	t.Logf("verify tx: verified=%v reason=%q height=%d conf=%d txID=%s",
		vr.Verified, vr.Reason, vr.BlockHeight, vr.Confirmations, txID)
}

func firstTxIDAtHeight(a *btc.BtcAdapter, height uint64) (string, error) {
	cfg := a.Config()
	client := rpc.NewClient(cfg.ServerAPI, cfg.RpcUser, cfg.RpcPassword)
	hashRes, err := client.Call("getblockhash", []interface{}{height})
	if err != nil {
		return "", err
	}
	blockRes, err := client.Call("getblock", []interface{}{hashRes.String(), 1})
	if err != nil {
		return "", err
	}
	txs := blockRes.Get("tx").Array()
	if len(txs) == 0 {
		return "", fmt.Errorf("block %d has no transactions", height)
	}
	return txs[0].String(), nil
}
