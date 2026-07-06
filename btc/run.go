package btc

import (
	"encoding/json"
	"fmt"

	"github.com/godaddy-x/wallet-adapter/chain"
	adapterconfig "github.com/godaddy-x/wallet-adapter/config"
)

// NewAdapter creates and registers an adapter from JSON config string.
func NewAdapter(jsonContent, symbol, fullName string, decimals int32) (*BtcAdapter, error) {
	kv := adapterconfig.MapConfig{}
	if err := json.Unmarshal([]byte(jsonContent), &kv); err != nil {
		return nil, err
	}

	adapter, err := chain.GetAdapter(symbol)
	if err != nil {
		adapter = NewBtcAdapter(symbol, fullName, decimals)
		chain.RegAdapter(symbol, adapter)
	}

	btcAdapter, ok := adapter.(*BtcAdapter)
	if !ok {
		return nil, fmt.Errorf("btc adapter fail")
	}
	if err = btcAdapter.LoadAssetsConfig(adapterconfig.MapConfig(kv)); err != nil {
		return nil, fmt.Errorf("btc adapter LoadAssetsConfig error: %w", err)
	}
	return btcAdapter, nil
}
