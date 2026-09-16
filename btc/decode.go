package btc

import (
	"encoding/json"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/chainparams"
	"github.com/godaddy-x/wallet-adapter-btc/internal/decoder"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/mailru/easyjson"
)

// PendingDecodeView display-oriented decode result for BIP143 pending-sign JSON.
type PendingDecodeView struct {
	OK         bool              `json:"ok"`
	Error      string            `json:"error,omitempty"`
	SignScheme string            `json:"signScheme,omitempty"`
	ChainID    string            `json:"chainId,omitempty"`
	TxType     int64             `json:"txType"`
	Sid        string            `json:"sid,omitempty"`
	Symbol     string            `json:"symbol,omitempty"`
	RawHex     string            `json:"rawHex,omitempty"`
	Display    PendingDisplay    `json:"display"`
	Raw        PendingRawBTC     `json:"raw"`
	SignExt    map[string]string `json:"signExt,omitempty"`
}

// PendingDisplay user-facing fields from pending JSON.
type PendingDisplay struct {
	TxFrom   []string          `json:"txFrom,omitempty"`
	TxTo     []string          `json:"txTo,omitempty"`
	TxAmount string            `json:"txAmount,omitempty"`
	Fees     string            `json:"fees,omitempty"`
	FeeRate  string            `json:"feeRate,omitempty"`
	To       map[string]string `json:"to,omitempty"`
}

// PendingRawBTC wire transaction IO decoded from raw hex.
type PendingRawBTC struct {
	Inputs  []IOView `json:"inputs,omitempty"`
	Outputs []IOView `json:"outputs,omitempty"`
}

// IOView input or output entry.
type IOView struct {
	TxID    string `json:"txID,omitempty"`
	Vout    uint32 `json:"vout,omitempty"`
	Address string `json:"address,omitempty"`
	Amount  int64  `json:"amount,omitempty"`
}

// DecodePendingSignData decodes PendingSignTx.Data for bip143 BTC transfers.
func DecodePendingSignData(data string) (string, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return marshalView(failView(0, "pending data is empty"))
	}
	hdr, err := parsePendingHeader(data)
	if err != nil {
		return marshalView(failView(0, err.Error()))
	}
	if hdr.TxType != 0 && hdr.TxType != 1 {
		return marshalView(failView(hdr.TxType, "unsupported txType for btc decode"))
	}
	var raw types.RawTransaction
	if err := easyjson.Unmarshal([]byte(data), &raw); err != nil {
		return marshalView(failView(hdr.TxType, "raw tx decode: "+err.Error()))
	}
	return marshalView(decodeRawTransaction(&raw))
}

// DecodeRawHexView decodes unsigned BTC transaction hex.
func DecodeRawHexView(rawHex string, network string) (*PendingRawBTC, error) {
	v, err := decoder.DecodeRawHexView(rawHex, network)
	if err != nil {
		return nil, err
	}
	out := &PendingRawBTC{}
	for _, in := range v.Inputs {
		out.Inputs = append(out.Inputs, IOView{
			TxID: in.TxID,
			Vout: in.Vout,
		})
	}
	for _, o := range v.Outputs {
		out.Outputs = append(out.Outputs, IOView{
			Address: o.Address,
			Amount:  o.Amount,
		})
	}
	return out, nil
}

func decodeRawTransaction(raw *types.RawTransaction) PendingDecodeView {
	if raw == nil {
		return failView(0, "rawTx is nil")
	}
	ext, chainID, scheme := parseSignExtMeta(raw.SignExt)
	network := inferNetwork(raw)
	view := PendingDecodeView{
		OK:         true,
		SignScheme: scheme,
		ChainID:    chainID,
		TxType:     raw.TxType,
		Sid:        raw.Sid,
		Symbol:     raw.Coin.Symbol,
		RawHex:     strings.TrimSpace(raw.RawHex),
		Display: PendingDisplay{
			TxFrom:   append([]string(nil), raw.TxFrom...),
			TxTo:     append([]string(nil), raw.TxTo...),
			TxAmount: raw.TxAmount,
			Fees:     raw.Fees,
			FeeRate:  raw.FeeRate,
			To:       raw.To,
		},
		SignExt: ext,
	}
	if view.RawHex != "" {
		if btcRaw, err := DecodeRawHexView(view.RawHex, network); err == nil {
			view.Raw = *btcRaw
		} else {
			view.OK = false
			view.Error = err.Error()
		}
	}
	return view
}

func inferNetwork(raw *types.RawTransaction) string {
	for _, addr := range raw.TxFrom {
		if n := inferNetworkFromAddr(addr); n != "" {
			return n
		}
	}
	for _, addr := range raw.TxTo {
		if n := inferNetworkFromAddr(addr); n != "" {
			return n
		}
	}
	for _, addr := range raw.To {
		if n := inferNetworkFromAddr(addr); n != "" {
			return n
		}
	}
	return chainparams.NetworkMainnet
}

func inferNetworkFromAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	lower := strings.ToLower(addr)
	switch {
	case strings.HasPrefix(lower, "bcrt1"):
		return chainparams.NetworkRegtest
	case strings.HasPrefix(lower, "tb1"), strings.HasPrefix(addr, "m"), strings.HasPrefix(addr, "n"), strings.HasPrefix(addr, "2"):
		return chainparams.NetworkTestnet
	case strings.HasPrefix(lower, "bc1"), strings.HasPrefix(addr, "1"), strings.HasPrefix(addr, "3"):
		return chainparams.NetworkMainnet
	default:
		return ""
	}
}

func parseSignExtMeta(signExtJSON string) (map[string]string, string, string) {
	ext, err := types.ParseSignExt(signExtJSON)
	if err != nil {
		return nil, "", ""
	}
	return ext, strings.TrimSpace(ext[types.SignExtKeyChainID]), types.SignExtScheme(ext)
}

type pendingHeader struct {
	TxType  int64  `json:"txType"`
	Sid     string `json:"sid"`
	SignExt string `json:"signExt"`
	Coin    struct {
		Symbol string `json:"symbol"`
	} `json:"coin"`
}

func parsePendingHeader(data string) (*pendingHeader, error) {
	var hdr pendingHeader
	if err := json.Unmarshal([]byte(data), &hdr); err != nil {
		return nil, err
	}
	return &hdr, nil
}

func failView(txType int64, msg string) PendingDecodeView {
	return PendingDecodeView{OK: false, Error: msg, TxType: txType, Display: PendingDisplay{}}
}

func marshalView(v PendingDecodeView) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
