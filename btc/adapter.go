package btc

import (
	adapter "github.com/godaddy-x/wallet-adapter"
	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/decoder"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	btcscanner "github.com/godaddy-x/wallet-adapter-btc/internal/scanner"
	"github.com/godaddy-x/wallet-adapter/chain"
	adapterconfig "github.com/godaddy-x/wallet-adapter/config"
	"github.com/godaddy-x/wallet-adapter/scanner"
	"github.com/godaddy-x/wallet-adapter/types"
)

// BtcAdapter implements github.com/godaddy-x/wallet-adapter ChainAdapter for Bitcoin.
type BtcAdapter struct {
	chain.ChainAdapterBase
	wm        *manager.WalletManager
	txDec     *decoder.BtcTransactionDecoder
	addrDec   *decoder.BtcAddressDecoder
	blockScan scanner.BlockScanner
	symbol    string
	fullName  string
	decimals  int32
}

// NewBtcAdapter creates a Bitcoin chain adapter.
func NewBtcAdapter(symbol, fullName string, decimals int32) *BtcAdapter {
	cfg := config.NewConfig(symbol)
	wm := &manager.WalletManager{Config: cfg}
	txDec := decoder.NewTransactionDecoder(wm)
	addrDec := decoder.NewAddressDecoder(wm)
	blockScan := btcscanner.NewBlockScanner(wm)
	return &BtcAdapter{
		wm:        wm,
		txDec:     txDec,
		addrDec:   addrDec,
		blockScan: blockScan,
		symbol:    symbol,
		fullName:  fullName,
		decimals:  decimals,
	}
}

func (a *BtcAdapter) Symbol() string {
	if a.symbol != "" {
		return a.symbol
	}
	return a.wm.Symbol()
}

func (a *BtcAdapter) FullName() string { return a.fullName }

func (a *BtcAdapter) Decimal() int32 {
	if a.decimals > 0 {
		return a.decimals
	}
	return a.wm.Decimal()
}

func (a *BtcAdapter) CurveType() uint32 { return config.DefaultCurveType }

func (a *BtcAdapter) BalanceModelType() types.BalanceModelType {
	return types.BalanceModelTypeAddress
}

func (a *BtcAdapter) GetTransactionDecoder() adapter.TransactionDecoder { return a.txDec }

func (a *BtcAdapter) GetBlockScanner() scanner.BlockScanner { return a.blockScan }

func (a *BtcAdapter) GetAddressDecoder() adapter.AddressDecoder { return a.addrDec }

func (a *BtcAdapter) GetSmartContractDecoder() adapter.SmartContractDecoder { return nil }

func (a *BtcAdapter) LoadAssetsConfig(cfg interface{}) error {
	var c adapterconfig.Configer
	switch v := cfg.(type) {
	case adapterconfig.Configer:
		c = v
	case map[string]string:
		c = adapterconfig.MapConfig(v)
	default:
		return nil
	}
	if err := a.wm.LoadAssetsConfig(c); err != nil {
		return err
	}
	a.addrDec = decoder.NewAddressDecoder(a.wm)
	return nil
}

func (a *BtcAdapter) InitAssetsConfig() (interface{}, error) {
	return map[string]string{}, nil
}

func (a *BtcAdapter) Config() *config.WalletConfig { return a.wm.Config }

// GetTransaction loads a transaction by txid (mempool or confirmed).
func (a *BtcAdapter) GetTransaction(txid string) (*models.Transaction, error) {
	return a.wm.GetTransaction(txid)
}
