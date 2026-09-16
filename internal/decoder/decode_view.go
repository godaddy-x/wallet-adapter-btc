package decoder

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
	"github.com/godaddy-x/wallet-adapter-btc/internal/chainparams"
)

// IOView one transaction input or output.
type IOView struct {
	TxID    string `json:"txID,omitempty"`
	Vout    uint32 `json:"vout,omitempty"`
	Address string `json:"address,omitempty"`
	Amount  int64  `json:"amount,omitempty"`
}

// RawHexView decoded unsigned/signed BTC transaction structure.
type RawHexView struct {
	Inputs  []IOView `json:"inputs,omitempty"`
	Outputs []IOView `json:"outputs,omitempty"`
}

// DecodeRawHexView parses wire transaction hex into inputs/outputs (addresses when inferable).
func DecodeRawHexView(txHex string, network string) (*RawHexView, error) {
	txHex = strings.TrimSpace(txHex)
	if txHex == "" {
		return nil, fmt.Errorf("tx hex is empty")
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(txHex, "0x"))
	if err != nil {
		return nil, fmt.Errorf("decode tx hex: %w", err)
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	if err := tx.Deserialize(bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("deserialize btc tx: %w", err)
	}
	view := &RawHexView{}
	for _, in := range tx.TxIn {
		view.Inputs = append(view.Inputs, IOView{
			TxID: in.PreviousOutPoint.Hash.String(),
			Vout: in.PreviousOutPoint.Index,
		})
	}
	paramsList := networkParamsCandidates(network)
	for _, out := range tx.TxOut {
		io := IOView{Amount: out.Value}
		if addr := scriptToAddress(out.PkScript, paramsList); addr != "" {
			io.Address = addr
		}
		view.Outputs = append(view.Outputs, io)
	}
	return view, nil
}

func networkParamsCandidates(network string) []*chaincfg.Params {
	network = strings.TrimSpace(network)
	if network != "" {
		return []*chaincfg.Params{btcaddr.ParamsForNetwork(network)}
	}
	return []*chaincfg.Params{
		btcaddr.ParamsForNetwork(chainparams.NetworkMainnet),
		btcaddr.ParamsForNetwork(chainparams.NetworkTestnet),
		btcaddr.ParamsForNetwork(chainparams.NetworkRegtest),
	}
}

func scriptToAddress(pkScript []byte, paramsList []*chaincfg.Params) string {
	for _, params := range paramsList {
		if addr := scriptToAddressForParams(pkScript, params); addr != "" {
			return addr
		}
	}
	return ""
}

func scriptToAddressForParams(pkScript []byte, params *chaincfg.Params) string {
	if len(pkScript) == 0 {
		return ""
	}
	switch {
	case len(pkScript) == 22 && pkScript[0] == txscript.OP_0 && pkScript[1] == 0x14:
		addr, err := btcutil.NewAddressWitnessPubKeyHash(pkScript[2:], params)
		if err == nil {
			return addr.EncodeAddress()
		}
	case len(pkScript) == 25 && pkScript[0] == txscript.OP_DUP:
		addr, err := btcutil.NewAddressPubKeyHash(pkScript[3:23], params)
		if err == nil {
			return addr.EncodeAddress()
		}
	}
	_, addrs, _, err := txscript.ExtractPkScriptAddrs(pkScript, params)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0].EncodeAddress()
}
