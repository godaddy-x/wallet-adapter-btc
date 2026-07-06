package decoder

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
	"github.com/godaddy-x/wallet-adapter-btc/internal/txbuild"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/godaddy-x/wallet-adapter/wallet"
)

func txIDFromSignedHex(signedHex string) (string, error) {
	return txbuild.TxIDFromHex(signedHex)
}

func decodeScriptHex(scriptHex string) ([]byte, error) {
	scriptHex = strings.TrimSpace(scriptHex)
	if scriptHex == "" {
		return nil, errors.New("scriptPubKey is empty")
	}
	return hex.DecodeString(scriptHex)
}

func collectNativeSignatures(network string, rawTx *types.RawTransaction, prevOuts []txbuild.PrevOut) ([]txbuild.SignatureInput, error) {
	if rawTx == nil || len(rawTx.Signatures) == 0 {
		return nil, fmt.Errorf("transaction signature is empty")
	}
	expected, err := txbuild.InputSighashes(rawTx.RawHex, prevOuts)
	if err != nil {
		return nil, err
	}
	byMessage := make(map[string]txbuild.SignatureInput, len(expected))
	for _, keySignatures := range rawTx.Signatures {
		for _, keySignature := range keySignatures {
			signature, err := hex.DecodeString(keySignature.Signature)
			if err != nil {
				return nil, fmt.Errorf("decode signature: %w", err)
			}
			if len(signature) != 64 {
				return nil, fmt.Errorf("signature must be 64 bytes R||S, got %d", len(signature))
			}
			pubkey, err := btcaddr.PubKeyForWitnessProgram(keySignature.Address.Address, keySignature.Address.PublicKey, network)
			if err != nil {
				return nil, err
			}
			byMessage[strings.ToLower(keySignature.Message)] = txbuild.SignatureInput{
				SighashHex: keySignature.Message,
				Signature:  signature,
				PubKey:     pubkey,
			}
		}
	}
	sigs := make([]txbuild.SignatureInput, 0, len(expected))
	for _, exp := range expected {
		sig, ok := byMessage[strings.ToLower(exp.Hash)]
		if !ok {
			return nil, fmt.Errorf("missing signature for input sighash %s", exp.Hash)
		}
		sigs = append(sigs, sig)
	}
	return sigs, nil
}

func (d *BtcTransactionDecoder) buildPrevOuts(vins []txbuild.Vin, rawTx *types.RawTransaction) ([]txbuild.PrevOut, error) {
	if cached, ok := prevOutsFromExt(rawTx, vins); ok {
		return cached, nil
	}
	prevOuts := make([]txbuild.PrevOut, 0, len(vins))
	for _, vin := range vins {
		utxo, err := d.Wm.GetTxOut(vin.TxID, uint64(vin.Vout))
		if err != nil {
			return nil, err
		}
		script, err := decodeScriptHex(utxo.ScriptPubKey)
		if err != nil {
			return nil, err
		}
		txAmount := util.StringNumToBigIntWithExp(utxo.Value, d.Wm.Decimal())
		prevOuts = append(prevOuts, txbuild.PrevOut{
			ScriptPubKey: script,
			Amount:       txAmount.Int64(),
			Address:      utxo.Addr,
		})
	}
	return prevOuts, nil
}

func (d *BtcTransactionDecoder) buildSignedNativeTransaction(rawTx *types.RawTransaction) (string, []txbuild.PrevOut, error) {
	network := d.Wm.Config.NetworkName()
	if _, err := hex.DecodeString(rawTx.RawHex); err != nil {
		return "", nil, errors.New("invalid transaction hex data")
	}
	vins, err := txbuild.OutpointsFromHex(rawTx.RawHex)
	if err != nil {
		return "", nil, fmt.Errorf("decode outpoints: %w", err)
	}
	prevOuts, err := d.buildPrevOuts(vins, rawTx)
	if err != nil {
		return "", nil, err
	}
	sigs, err := collectNativeSignatures(network, rawTx, prevOuts)
	if err != nil {
		return "", nil, err
	}
	signedTrans, err := txbuild.ApplySignatures(rawTx.RawHex, prevOuts, sigs)
	if err != nil {
		return "", nil, fmt.Errorf("transaction compose signatures failed: %w", err)
	}
	return signedTrans, prevOuts, nil
}

// PrecomputeSubmitTxID derives txid from fully signed raw transaction (same bytes as broadcast).
func (d *BtcTransactionDecoder) PrecomputeSubmitTxID(_ wallet.WalletDAI, rawTx *types.RawTransaction) (string, error) {
	if rawTx == nil {
		return "", fmt.Errorf("rawTx is nil")
	}
	signedHex, _, err := d.buildSignedNativeTransaction(rawTx)
	if err != nil {
		return "", err
	}
	return txIDFromSignedHex(signedHex)
}
