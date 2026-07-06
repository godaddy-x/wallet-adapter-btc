package models

import (
	"encoding/hex"
	"strings"

	"github.com/btcsuite/btcd/txscript"
	"github.com/godaddy-x/wallet-adapter-btc/internal/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

// Unspent UTXO record.
type Unspent struct {
	TxID          string `json:"txid"`
	Vout          uint64 `json:"vout"`
	Address       string `json:"address"`
	AccountID     string `json:"account"`
	ScriptPubKey  string `json:"scriptPubKey"`
	Amount        string `json:"amount"`
	Confirmations uint64 `json:"confirmations"`
	Spendable     bool   `json:"spendable"`
	Solvable      bool   `json:"solvable"`
}

// NewUnspent parses listunspent / explorer utxo item.
func NewUnspent(json *gjson.Result) *Unspent {
	return &Unspent{
		TxID:          gjson.Get(json.Raw, "txid").String(),
		Vout:          gjson.Get(json.Raw, "vout").Uint(),
		Address:       gjson.Get(json.Raw, "address").String(),
		AccountID:     gjson.Get(json.Raw, "account").String(),
		ScriptPubKey:  gjson.Get(json.Raw, "scriptPubKey").String(),
		Amount:        gjson.Get(json.Raw, "amount").String(),
		Confirmations: gjson.Get(json.Raw, "confirmations").Uint(),
		Spendable:     true,
		Solvable:      gjson.Get(json.Raw, "solvable").Bool(),
	}
}

// CompareUnspentAmountAsc sorts smaller UTXO amounts first.
func CompareUnspentAmountAsc(a, b *Unspent) int {
	aAmount, _ := decimal.NewFromString(a.Amount)
	bAmount, _ := decimal.NewFromString(b.Amount)
	if aAmount.GreaterThan(bAmount) {
		return 1
	}
	return -1
}

// UnspentSort sorts UTXOs with custom comparator.
type UnspentSort struct {
	Values     []*Unspent
	Comparator func(a, b *Unspent) int
}

func (s UnspentSort) Len() int           { return len(s.Values) }
func (s UnspentSort) Swap(i, j int)      { s.Values[i], s.Values[j] = s.Values[j], s.Values[i] }
func (s UnspentSort) Less(i, j int) bool { return s.Comparator(s.Values[i], s.Values[j]) < 0 }

// Block chain block.
type Block struct {
	Hash              string
	Confirmations     uint64
	Merkleroot        string
	TxIDs             []string
	Previousblockhash string
	Height            uint64
	Version           uint64
	Time              uint64
	TxDetails         []*Transaction
}

// Transaction parsed transaction.
type Transaction struct {
	TxID          string
	Size          uint64
	Version       uint64
	LockTime      int64
	Hex           string
	BlockHash     string
	BlockHeight   uint64
	Confirmations uint64
	Blocktime     int64
	Fees          string
	Vins          []*Vin
	Vouts         []*Vout
}

// Vin transaction input.
type Vin struct {
	Coinbase     string
	TxID         string
	Vout         uint64
	N            uint64
	Sequence     uint32
	Addr         string
	Value        string
	ScriptPubKey string
}

// Vout transaction output.
type Vout struct {
	N            uint64
	Addr         string
	Value        string
	ScriptPubKey string
	Type         string
}

// BlockParser parses block/tx JSON using network config.
type BlockParser struct {
	Cfg *config.WalletConfig
}

// NewBlockParser creates a parser bound to wallet config.
func NewBlockParser(cfg *config.WalletConfig) *BlockParser {
	return &BlockParser{Cfg: cfg}
}

// NewBlock parses getblock verbose result.
func (p *BlockParser) NewBlock(json *gjson.Result) *Block {
	obj := &Block{
		Height:            gjson.Get(json.Raw, "height").Uint(),
		Hash:              gjson.Get(json.Raw, "hash").String(),
		Confirmations:     gjson.Get(json.Raw, "confirmations").Uint(),
		Merkleroot:        gjson.Get(json.Raw, "merkleroot").String(),
		Previousblockhash: gjson.Get(json.Raw, "previousblockhash").String(),
		Version:           gjson.Get(json.Raw, "version").Uint(),
		Time:              gjson.Get(json.Raw, "time").Uint(),
	}
	txs := make([]string, 0)
	txDetails := make([]*Transaction, 0)
	for _, tx := range gjson.Get(json.Raw, "tx").Array() {
		if tx.IsObject() {
			txObj := p.NewTxByCore(&tx)
			txObj.BlockHeight = obj.Height
			txObj.BlockHash = obj.Hash
			txObj.Blocktime = int64(obj.Time)
			txDetails = append(txDetails, txObj)
		} else {
			txs = append(txs, tx.String())
		}
	}
	obj.TxIDs = txs
	obj.TxDetails = txDetails
	return obj
}

// NewBlockByExplorer parses insight-api block.
func NewBlockByExplorer(json *gjson.Result) *Block {
	obj := &Block{
		Hash:              gjson.Get(json.Raw, "hash").String(),
		Confirmations:     gjson.Get(json.Raw, "confirmations").Uint(),
		Merkleroot:        gjson.Get(json.Raw, "merkleroot").String(),
		Previousblockhash: gjson.Get(json.Raw, "previousblockhash").String(),
		Height:            gjson.Get(json.Raw, "height").Uint(),
		Time:              gjson.Get(json.Raw, "time").Uint(),
	}
	txs := make([]string, 0)
	for _, tx := range gjson.Get(json.Raw, "tx").Array() {
		txs = append(txs, tx.String())
	}
	obj.TxIDs = txs
	return obj
}

// NewTxByCore parses verbose getrawtransaction / getblock tx object.
func (p *BlockParser) NewTxByCore(json *gjson.Result) *Transaction {
	obj := &Transaction{
		TxID:          gjson.Get(json.Raw, "txid").String(),
		Version:       gjson.Get(json.Raw, "version").Uint(),
		LockTime:      gjson.Get(json.Raw, "locktime").Int(),
		BlockHash:     gjson.Get(json.Raw, "blockhash").String(),
		Confirmations: gjson.Get(json.Raw, "confirmations").Uint(),
		Blocktime:     gjson.Get(json.Raw, "blocktime").Int(),
		Size:          gjson.Get(json.Raw, "size").Uint(),
		Hex:           gjson.Get(json.Raw, "hex").String(),
	}
	obj.Vins = make([]*Vin, 0)
	if vins := gjson.Get(json.Raw, "vin"); vins.IsArray() {
		for i, vin := range vins.Array() {
			input := p.newTxVinByCore(&vin)
			input.N = uint64(i)
			obj.Vins = append(obj.Vins, input)
		}
	}
	obj.Vouts = make([]*Vout, 0)
	if vouts := gjson.Get(json.Raw, "vout"); vouts.IsArray() {
		for _, vout := range vouts.Array() {
			obj.Vouts = append(obj.Vouts, p.ParseVoutFromCore(&vout))
		}
	}
	return obj
}

// NewTxByExplorer parses insight-api tx object.
func (p *BlockParser) NewTxByExplorer(json *gjson.Result) *Transaction {
	obj := &Transaction{
		TxID:          gjson.Get(json.Raw, "txid").String(),
		Version:       gjson.Get(json.Raw, "version").Uint(),
		LockTime:      gjson.Get(json.Raw, "locktime").Int(),
		BlockHash:     gjson.Get(json.Raw, "blockhash").String(),
		Confirmations: gjson.Get(json.Raw, "confirmations").Uint(),
		Blocktime:     gjson.Get(json.Raw, "blocktime").Int(),
		Size:          gjson.Get(json.Raw, "size").Uint(),
		Fees:          gjson.Get(json.Raw, "fees").String(),
	}
	blockHeight := gjson.Get(json.Raw, "blockheight").Int()
	if blockHeight > 0 {
		obj.BlockHeight = uint64(blockHeight)
	}
	obj.Vins = make([]*Vin, 0)
	if vins := gjson.Get(json.Raw, "vin"); vins.IsArray() {
		for _, vin := range vins.Array() {
			if input := newTxVinByExplorer(&vin); input != nil {
				obj.Vins = append(obj.Vins, input)
			}
		}
	}
	obj.Vouts = make([]*Vout, 0)
	if vouts := gjson.Get(json.Raw, "vout"); vouts.IsArray() {
		for _, vout := range vouts.Array() {
			if output := p.newTxVoutByExplorer(&vout); output != nil {
				obj.Vouts = append(obj.Vouts, output)
			}
		}
	}
	return obj
}

func (p *BlockParser) newTxVinByCore(json *gjson.Result) *Vin {
	vin := &Vin{
		TxID:     gjson.Get(json.Raw, "txid").String(),
		Vout:     gjson.Get(json.Raw, "vout").Uint(),
		Coinbase: gjson.Get(json.Raw, "coinbase").String(),
	}
	if seq := gjson.Get(json.Raw, "sequence"); seq.Exists() {
		vin.Sequence = uint32(seq.Uint())
	} else {
		vin.Sequence = 0xffffffff
	}
	if prevout := gjson.Get(json.Raw, "prevout"); prevout.Exists() && prevout.IsObject() {
		vin.Value = prevout.Get("value").String()
		vin.Addr = p.resolveVoutAddress(&prevout)
		vin.ScriptPubKey = prevout.Get("scriptPubKey.hex").String()
	}
	return vin
}

func newTxVinByExplorer(json *gjson.Result) *Vin {
	vin := &Vin{
		TxID:     gjson.Get(json.Raw, "txid").String(),
		Vout:     gjson.Get(json.Raw, "vout").Uint(),
		N:        gjson.Get(json.Raw, "n").Uint(),
		Addr:     gjson.Get(json.Raw, "addr").String(),
		Value:    gjson.Get(json.Raw, "value").String(),
		Coinbase: gjson.Get(json.Raw, "coinbase").String(),
	}
	if seq := gjson.Get(json.Raw, "sequence"); seq.Exists() {
		vin.Sequence = uint32(seq.Uint())
	} else {
		vin.Sequence = 0xffffffff
	}
	return vin
}

func (p *BlockParser) ParseVoutFromCore(json *gjson.Result) *Vout {
	obj := &Vout{
		Value:        gjson.Get(json.Raw, "value").String(),
		N:            gjson.Get(json.Raw, "n").Uint(),
		ScriptPubKey: gjson.Get(json.Raw, "scriptPubKey.hex").String(),
		Type:         gjson.Get(json.Raw, "scriptPubKey.type").String(),
	}
	obj.Addr = p.resolveVoutAddress(json)
	if obj.Addr != "" && obj.ScriptPubKey != "" && !util.VoutAddressMatchesScript(obj.Addr, obj.ScriptPubKey, obj.Type, p.network()) {
		obj.Addr = ""
	}
	return obj
}

func (p *BlockParser) network() string {
	if p != nil && p.Cfg != nil {
		return p.Cfg.NetworkName()
	}
	return config.NetworkMainnet
}

// resolveVoutAddress extracts output address from Core/Insight scriptPubKey JSON.
func (p *BlockParser) resolveVoutAddress(json *gjson.Result) string {
	if json == nil {
		return ""
	}
	if addr := gjson.Get(json.Raw, "scriptPubKey.address").String(); addr != "" {
		return addr
	}
	if addresses := gjson.Get(json.Raw, "scriptPubKey.addresses"); addresses.IsArray() {
		if len(addresses.Array()) == 1 {
			return addresses.Array()[0].String()
		}
	}
	scriptHex := gjson.Get(json.Raw, "scriptPubKey.hex").String()
	if scriptHex == "" {
		return ""
	}
	scriptBytes, err := hex.DecodeString(scriptHex)
	if err != nil {
		return ""
	}
	return p.resolveVoutAddressFromScript(scriptBytes, gjson.Get(json.Raw, "scriptPubKey.type").String())
}

func (p *BlockParser) resolveVoutAddressFromScript(scriptBytes []byte, scriptType string) string {
	if scriptType != "witness_v0_keyhash" && scriptType != "witness_v0_scripthash" {
		if len(scriptBytes) < 2 || scriptBytes[0] != txscript.OP_0 {
			return ""
		}
	}
	network := config.NetworkMainnet
	if p != nil && p.Cfg != nil {
		network = p.Cfg.NetworkName()
	}
	addr, err := util.ScriptPubKeyToBech32Address(scriptBytes, network)
	if err != nil {
		return ""
	}
	return addr
}

func (p *BlockParser) newTxVoutByExplorer(json *gjson.Result) *Vout {
	asm := gjson.Get(json.Raw, "scriptPubKey.asm").String()
	if strings.HasPrefix(asm, "OP_RETURN") {
		return &Vout{
			Value: gjson.Get(json.Raw, "value").String(),
			N:     gjson.Get(json.Raw, "n").Uint(),
			Type:  "OP_RETURN",
		}
	}
	obj := p.ParseVoutFromCore(json)
	if obj.ScriptPubKey == "" && asm != "" {
		if scriptBytes, err := DecodeScript(asm); err == nil {
			obj.ScriptPubKey = hex.EncodeToString(scriptBytes)
			if obj.Addr == "" {
				obj.Addr = p.resolveVoutAddressFromScript(scriptBytes, obj.Type)
			}
		}
	}
	return obj
}

// DecodeScript converts asm script to bytes.
func DecodeScript(script string) ([]byte, error) {
	opcodes := strings.Split(script, " ")
	builder := txscript.NewScriptBuilder()
	for _, codeName := range opcodes {
		code, ok := txscript.OpcodeByName[codeName]
		if ok {
			builder.AddOp(code)
		} else {
			if len(codeName)%2 != 0 {
				codeName = "0" + codeName
			}
			data, err := hex.DecodeString(codeName)
			if err != nil {
				return nil, err
			}
			builder.AddData(data)
		}
	}
	return builder.Script()
}
