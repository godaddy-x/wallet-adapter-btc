package decoder

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/txbuild"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/shopspring/decimal"
)

type cachedPrevOut struct {
	TxID         string `json:"txid"`
	Vout         uint32 `json:"vout"`
	ScriptPubKey string `json:"scriptPubKey"`
	Amount       int64  `json:"amount"`
	Address      string `json:"address"`
}

func unspentsFromOriginExt(rawTx *types.RawTransaction) ([]*models.Unspent, bool) {
	if rawTx == nil || rawTx.ExtParam == nil {
		return nil, false
	}
	raw := strings.TrimSpace(rawTx.ExtParam[extparam.KeyOriginPrevOuts])
	if raw == "" {
		return nil, false
	}
	var items []cachedPrevOut
	if err := json.Unmarshal([]byte(raw), &items); err != nil || len(items) == 0 {
		return nil, false
	}
	used := make([]*models.Unspent, 0, len(items))
	for _, item := range items {
		amount := decimal.New(item.Amount, 0).Shift(-8).StringFixed(8)
		used = append(used, &models.Unspent{
			TxID:         item.TxID,
			Vout:         uint64(item.Vout),
			Address:      item.Address,
			ScriptPubKey: item.ScriptPubKey,
			Amount:       amount,
			Spendable:    true,
		})
	}
	return used, true
}

func validateOriginPrevOutCache(cached []*models.Unspent, vins []*models.Vin) error {
	if len(cached) != len(vins) {
		return fmt.Errorf("origin prevout cache count mismatch: cache=%d chain=%d", len(cached), len(vins))
	}
	for i, u := range cached {
		vin := vins[i]
		if vin == nil {
			return fmt.Errorf("origin vin %d is nil", i)
		}
		if vin.Coinbase != "" {
			return fmt.Errorf("coinbase input cannot be replaced")
		}
		if !strings.EqualFold(strings.TrimSpace(u.TxID), strings.TrimSpace(vin.TxID)) || u.Vout != vin.Vout {
			return fmt.Errorf("origin prevout cache vin mismatch at %d", i)
		}
	}
	return nil
}

func storePrevOutsExt(rawTx *types.RawTransaction, vins []txbuild.Vin, prevOuts []txbuild.PrevOut) {
	if rawTx == nil || len(vins) != len(prevOuts) {
		return
	}
	items := make([]cachedPrevOut, 0, len(vins))
	for i, vin := range vins {
		po := prevOuts[i]
		items = append(items, cachedPrevOut{
			TxID:         vin.TxID,
			Vout:         vin.Vout,
			ScriptPubKey: fmt.Sprintf("%x", po.ScriptPubKey),
			Amount:       po.Amount,
			Address:      po.Address,
		})
	}
	b, err := json.Marshal(items)
	if err != nil {
		return
	}
	if rawTx.ExtParam == nil {
		rawTx.ExtParam = make(map[string]string)
	}
	rawTx.ExtParam[extparam.KeyBTCPrevOuts] = string(b)
}

func prevOutsFromExt(rawTx *types.RawTransaction, vins []txbuild.Vin) ([]txbuild.PrevOut, bool) {
	if rawTx == nil || rawTx.ExtParam == nil {
		return nil, false
	}
	raw := strings.TrimSpace(rawTx.ExtParam[extparam.KeyBTCPrevOuts])
	if raw == "" {
		return nil, false
	}
	var items []cachedPrevOut
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, false
	}
	if len(items) != len(vins) {
		return nil, false
	}
	prevOuts := make([]txbuild.PrevOut, 0, len(items))
	for i, item := range items {
		if !strings.EqualFold(item.TxID, vins[i].TxID) || item.Vout != vins[i].Vout {
			return nil, false
		}
		script, err := decodeScriptHex(item.ScriptPubKey)
		if err != nil {
			return nil, false
		}
		prevOuts = append(prevOuts, txbuild.PrevOut{
			ScriptPubKey: script,
			Amount:       item.Amount,
			Address:      item.Address,
		})
	}
	return prevOuts, true
}
