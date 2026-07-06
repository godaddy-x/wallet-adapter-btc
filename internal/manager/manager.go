// Package manager provides WalletManager: RPC, UTXO, fee estimation, block queries, and broadcast.
package manager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/rpc"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	adapterconfig "github.com/godaddy-x/wallet-adapter/config"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/imroc/req"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

// WalletManager aggregates BTC chain capabilities for decoders and scanner.
type WalletManager struct {
	Client         *rpc.Client
	ExplorerClient *rpc.Explorer
	Config         *config.WalletConfig
	parser         *models.BlockParser
}

// LoadAssetsConfig loads config and initializes RPC clients.
func (wm *WalletManager) LoadAssetsConfig(c adapterconfig.Configer) error {
	symbol := config.DefaultSymbol
	if wm.Config != nil && wm.Config.Symbol != "" {
		symbol = wm.Config.Symbol
	}
	cfg := config.BuildConfigFromConfiger(c, symbol)
	wm.Config = cfg
	cfg.MakeDataDir()
	wm.parser = models.NewBlockParser(cfg)

	if cfg.RPCServerType == config.RPCServerExplorer {
		wm.ExplorerClient = rpc.NewExplorer(cfg.ServerAPI)
		wm.Client = nil
	} else {
		if strings.TrimSpace(cfg.ServerAPI) == "" {
			return errors.New("serverAPI is empty")
		}
		wm.Client = rpc.NewClient(cfg.ServerAPI, cfg.RpcUser, cfg.RpcPassword)
		wm.Client.SetWalletURLs(cfg.RpcWallet, cfg.RpcQueryWallet)
		if cfg.BroadcastAPI != "" && cfg.BroadcastAPI != cfg.ServerAPI {
			wm.Client.BroadcastURL = cfg.BroadcastAPI
		}
		wm.ExplorerClient = nil
	}

	height, err := wm.GetBlockHeight()
	if err != nil {
		wm.Client = nil
		wm.ExplorerClient = nil
		return fmt.Errorf("btc node rpc ping (getblockcount) failed: %w", err)
	}
	if cfg.RPCServerType == config.RPCServerExplorer && height == 0 {
		// Explorer may report 0 before sync; still allow connect.
	} else if height == 0 {
		// Regtest/mainnet genesis-only chain: verify block 0 is reachable.
		if _, err := wm.GetBlockHash(0); err != nil {
			wm.Client = nil
			wm.ExplorerClient = nil
			return fmt.Errorf("btc node rpc ping (getblockhash 0) failed: %w", err)
		}
	}
	return nil
}

// Symbol returns coin symbol.
func (wm *WalletManager) Symbol() string {
	if wm.Config != nil && wm.Config.Symbol != "" {
		return wm.Config.Symbol
	}
	return config.DefaultSymbol
}

// Decimal returns native coin decimals.
func (wm *WalletManager) Decimal() int32 {
	if wm.Config != nil && wm.Config.Decimals > 0 {
		return wm.Config.Decimals
	}
	return config.DefaultDecimals
}

// ListUnspent returns UTXOs for addresses.
func (wm *WalletManager) ListUnspent(min uint64, addresses ...string) ([]*models.Unspent, error) {
	const limit = 100
	utxo := make([]*models.Unspent, 0)
	max := len(addresses)
	if max == 0 {
		return utxo, nil
	}
	step := max / limit
	for i := 0; i <= step; i++ {
		begin := i * limit
		end := (i + 1) * limit
		if end > max {
			end = max
		}
		batch := addresses[begin:end]
		if len(batch) == 0 {
			continue
		}
		var piece []*models.Unspent
		var err error
		if wm.Config.RPCServerType == config.RPCServerExplorer {
			piece, err = wm.listUnspentByExplorer(min, batch...)
		} else {
			piece, err = wm.listUnspentByCore(min, batch...)
		}
		if err != nil {
			return nil, err
		}
		utxo = append(utxo, piece...)
	}
	return utxo, nil
}

func (wm *WalletManager) listUnspentByCore(min uint64, addresses ...string) ([]*models.Unspent, error) {
	request := []interface{}{min, 9999999}
	if len(addresses) > 0 {
		request = append(request, addresses)
	}
	result, err := wm.Client.WalletCall("listunspent", request)
	if err != nil {
		return nil, err
	}
	utxos := make([]*models.Unspent, 0)
	for _, a := range result.Array() {
		utxos = append(utxos, models.NewUnspent(&a))
	}
	return utxos, nil
}

func (wm *WalletManager) listUnspentByExplorer(min uint64, addresses ...string) ([]*models.Unspent, error) {
	addrs := strings.Join(addresses, ",")
	result, err := wm.ExplorerClient.Call("addrs/utxo", req.Param{"addrs": addrs}, "POST")
	if err != nil {
		return nil, err
	}
	utxos := make([]*models.Unspent, 0)
	for _, a := range result.Array() {
		u := models.NewUnspent(&a)
		if u.Confirmations >= min {
			utxos = append(utxos, u)
		}
	}
	return utxos, nil
}

// EstimateFee estimates transaction fee from input/output count and fee rate (BTC/KB).
func (wm *WalletManager) EstimateFee(inputs, outputs int64, feeRate decimal.Decimal) (decimal.Decimal, error) {
	trxBytes := decimal.New(util.EstimateTxVsize(inputs, outputs), 0)
	trxFee := trxBytes.Div(decimal.New(1000, 0)).Mul(feeRate)
	trxFee = trxFee.Round(wm.Decimal())
	if trxFee.LessThan(wm.Config.MinFees) {
		trxFee = wm.Config.MinFees
	}
	return trxFee, nil
}

// SendRawTransaction broadcasts signed transaction hex.
func (wm *WalletManager) SendRawTransaction(txHex string) (string, error) {
	if wm.Config.RPCServerType == config.RPCServerExplorer {
		result, err := wm.ExplorerClient.Call("tx/send", req.Param{"rawtx": txHex}, "POST")
		if err != nil {
			return "", err
		}
		return result.Get("txid").String(), nil
	}
	result, err := wm.Client.BroadcastWalletCall("sendrawtransaction", []interface{}{txHex})
	if err != nil {
		return "", err
	}
	return result.String(), nil
}

// GetBlockHeight returns current chain height.
func (wm *WalletManager) GetBlockHeight() (uint64, error) {
	if wm == nil || wm.Config == nil {
		return 0, errors.New("wallet manager config is nil")
	}
	if wm.Config.RPCServerType == config.RPCServerExplorer {
		if wm.ExplorerClient == nil {
			return 0, errors.New("explorer client is nil")
		}
		result, err := wm.ExplorerClient.Call("status?q=getInfo", nil, "GET")
		if err != nil {
			return 0, err
		}
		return result.Get("info.blocks").Uint(), nil
	}
	if wm.Client == nil {
		return 0, errors.New("rpc client is nil")
	}
	result, err := wm.Client.Call("getblockcount", nil)
	if err != nil {
		return 0, err
	}
	return result.Uint(), nil
}

// GetBlockHash returns block hash at height.
func (wm *WalletManager) GetBlockHash(height uint64) (string, error) {
	if wm.Config.RPCServerType == config.RPCServerExplorer {
		result, err := wm.ExplorerClient.Call(fmt.Sprintf("block-index/%d", height), nil, "GET")
		if err != nil {
			return "", err
		}
		return result.Get("blockHash").String(), nil
	}
	result, err := wm.Client.Call("getblockhash", []interface{}{height})
	if err != nil {
		return "", err
	}
	return result.String(), nil
}

// GetBlock returns block by hash (verbosity 3 with prevout, fallback to 2).
func (wm *WalletManager) GetBlock(hash string) (*models.Block, error) {
	if wm.Config.RPCServerType == config.RPCServerExplorer {
		result, err := wm.ExplorerClient.Call("block/"+hash, nil, "GET")
		if err != nil {
			return nil, err
		}
		return models.NewBlockByExplorer(result), nil
	}
	result, err := wm.Client.Call("getblock", []interface{}{hash, 3})
	if err != nil {
		result, err = wm.Client.Call("getblock", []interface{}{hash, 2})
		if err != nil {
			return nil, err
		}
	}
	return wm.parser.NewBlock(result), nil
}

// GetBlockHeaderByHeight fetches block header fields at height.
func (wm *WalletManager) GetBlockHeaderByHeight(height uint64) (*types.BlockHeader, error) {
	hash, err := wm.GetBlockHash(height)
	if err != nil {
		return nil, err
	}
	block, err := wm.GetBlock(hash)
	if err != nil {
		return nil, err
	}
	latest, _ := wm.GetBlockHeight()
	confirmations := uint64(0)
	if latest >= height {
		confirmations = latest - height + 1
	}
	return &types.BlockHeader{
		Hash:              block.Hash,
		Confirmations:     confirmations,
		Merkleroot:        block.Merkleroot,
		Previousblockhash: block.Previousblockhash,
		Height:            block.Height,
		Version:           block.Version,
		Time:              block.Time,
		Symbol:            wm.Symbol(),
	}, nil
}

// GetTransaction returns transaction by txid.
func (wm *WalletManager) GetTransaction(txid string) (*models.Transaction, error) {
	if wm.Config.RPCServerType == config.RPCServerExplorer {
		result, err := wm.ExplorerClient.Call("tx/"+txid, nil, "GET")
		if err != nil {
			return nil, err
		}
		return wm.parser.NewTxByExplorer(result), nil
	}
	result, err := wm.Client.Call("getrawtransaction", []interface{}{txid, true})
	if err != nil {
		result, err = wm.Client.Call("getrawtransaction", []interface{}{txid, 1})
	}
	if err != nil {
		if tx, werr := wm.getTransactionByWallet(txid); werr == nil {
			return tx, nil
		}
		return nil, err
	}
	return wm.parser.NewTxByCore(result), nil
}

func (wm *WalletManager) getTransactionByWallet(txid string) (*models.Transaction, error) {
	if wm == nil || wm.Client == nil {
		return nil, fmt.Errorf("rpc client unavailable")
	}
	result, err := wm.Client.QueryWalletCall("gettransaction", []interface{}{txid, true, true})
	if err != nil {
		return nil, err
	}
	if decoded := gjson.Get(result.Raw, "decoded"); decoded.Exists() && decoded.IsObject() {
		return wm.parser.NewTxByCore(&decoded), nil
	}
	return nil, fmt.Errorf("wallet transaction decode unavailable")
}

// GetTxOut returns spent output details.
func (wm *WalletManager) GetTxOut(txid string, vout uint64) (*models.Vout, error) {
	if wm.Config.RPCServerType == config.RPCServerExplorer {
		tx, err := wm.GetTransaction(txid)
		if err != nil {
			return nil, err
		}
		for i, out := range tx.Vouts {
			if uint64(i) == vout {
				return out, nil
			}
		}
		return nil, fmt.Errorf("output not found")
	}
	result, err := wm.Client.Call("gettxout", []interface{}{txid, vout})
	if err != nil {
		return nil, err
	}
	if result.Exists() && result.Type != gjson.Null {
		out := wm.parser.ParseVoutFromCore(result)
		out.N = vout
		return out, nil
	}
	// Spent or mempool-spent outputs are absent from gettxout; load from parent tx vout.
	parent, err := wm.GetTransaction(txid)
	if err != nil {
		return nil, errors.New("output not found or spent")
	}
	if int(vout) >= len(parent.Vouts) {
		return nil, errors.New("output not found or spent")
	}
	out := parent.Vouts[vout]
	out.N = vout
	return out, nil
}

// GetBalanceByAddresses returns balances computed from UTXOs.
func (wm *WalletManager) GetBalanceByAddresses(addresses ...string) ([]*types.Balance, error) {
	utxos, err := wm.ListUnspent(0, addresses...)
	if err != nil {
		return nil, err
	}
	addrBalanceMap := calculateUnspentBalances(wm.Symbol(), utxos)
	result := make([]*types.Balance, 0, len(addresses))
	for _, addr := range addresses {
		if b, ok := addrBalanceMap[addr]; ok {
			result = append(result, b)
		} else {
			result = append(result, &types.Balance{
				Symbol:           wm.Symbol(),
				Address:          addr,
				Balance:          "0",
				UnconfirmBalance: "0",
				ConfirmBalance:   "0",
			})
		}
	}
	return result, nil
}

func calculateUnspentBalances(symbol string, utxos []*models.Unspent) map[string]*types.Balance {
	addrBalanceMap := make(map[string]*types.Balance)
	for _, utxo := range utxos {
		obj, exist := addrBalanceMap[utxo.Address]
		if !exist {
			obj = &types.Balance{Symbol: symbol, Address: utxo.Address}
		}
		tu, _ := decimal.NewFromString(obj.UnconfirmBalance)
		tb, _ := decimal.NewFromString(obj.ConfirmBalance)
		if utxo.Spendable {
			b, _ := decimal.NewFromString(utxo.Amount)
			if utxo.Confirmations > 0 {
				tb = tb.Add(b)
			} else {
				tu = tu.Add(b)
			}
		}
		obj.ConfirmBalance = tb.String()
		obj.UnconfirmBalance = tu.String()
		obj.Balance = tb.Add(tu).String()
		addrBalanceMap[utxo.Address] = obj
	}
	return addrBalanceMap
}
