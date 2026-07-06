package txbuild

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
)

const sequenceBIP125RBF = wire.MaxTxInSequenceNum - 2

// Vin references a UTXO outpoint.
type Vin struct {
	TxID string
	Vout uint32
}

// Vout is a payment output (amount in satoshis).
type Vout struct {
	Address string
	Amount  int64
}

// PrevOut describes the output being spent (for sighash + verify).
type PrevOut struct {
	ScriptPubKey []byte
	Amount       int64
	Address      string
}

// SighashMessage pairs BIP143 sighash hex with the signing address.
type SighashMessage struct {
	Hash    string
	Address string
}

// SignatureInput carries MPC R||S signature and pubkey for one input.
type SignatureInput struct {
	SighashHex string
	Signature  []byte
	PubKey     []byte
}

// BuildUnsigned constructs unsigned segwit transaction hex and per-input sighashes.
func BuildUnsigned(vins []Vin, vouts []Vout, prevOuts []PrevOut, replaceable bool, network string) (txHex string, messages []SighashMessage, err error) {
	if len(vins) != len(prevOuts) {
		return "", nil, fmt.Errorf("vin/prevOut count mismatch")
	}
	tx := wire.NewMsgTx(wire.TxVersion)

	for _, vin := range vins {
		txHash, err := chainhash.NewHashFromStr(vin.TxID)
		if err != nil {
			return "", nil, fmt.Errorf("invalid txid: %w", err)
		}
		seq := wire.MaxTxInSequenceNum
		if replaceable {
			seq = sequenceBIP125RBF
		}
		tx.AddTxIn(&wire.TxIn{
			PreviousOutPoint: wire.OutPoint{Hash: *txHash, Index: vin.Vout},
			Sequence:         seq,
		})
	}

	for _, vout := range vouts {
		pkScript, err := btcaddr.PayToAddressScript(vout.Address, network)
		if err != nil {
			return "", nil, fmt.Errorf("output script for %q: %w", vout.Address, err)
		}
		tx.AddTxOut(wire.NewTxOut(vout.Amount, pkScript))
	}

	messages, err = inputSighashes(tx, prevOuts)
	if err != nil {
		return "", nil, err
	}

	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(buf.Bytes()), messages, nil
}

// InputSighashes recomputes per-input BIP143/legacy sighashes for an unsigned transaction.
func InputSighashes(unsignedHex string, prevOuts []PrevOut) ([]SighashMessage, error) {
	raw, err := hex.DecodeString(unsignedHex)
	if err != nil {
		return nil, fmt.Errorf("decode unsigned hex: %w", err)
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	if err := tx.Deserialize(bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("decode tx: %w", err)
	}
	if len(tx.TxIn) != len(prevOuts) {
		return nil, fmt.Errorf("input count mismatch")
	}
	return inputSighashes(tx, prevOuts)
}

func inputSighashes(tx *wire.MsgTx, prevOuts []PrevOut) ([]SighashMessage, error) {
	sigHashes := txscript.NewTxSigHashes(tx)
	messages := make([]SighashMessage, 0, len(prevOuts))
	for i, po := range prevOuts {
		var digest []byte
		var err error
		if txscript.IsWitnessProgram(po.ScriptPubKey) {
			digest, err = txscript.CalcWitnessSigHash(po.ScriptPubKey, sigHashes, txscript.SigHashAll, tx, i, po.Amount)
		} else {
			digest, err = txscript.CalcSignatureHash(po.ScriptPubKey, txscript.SigHashAll, tx, i)
		}
		if err != nil {
			return nil, fmt.Errorf("sighash input %d: %w", i, err)
		}
		messages = append(messages, SighashMessage{
			Hash:    hex.EncodeToString(digest),
			Address: po.Address,
		})
	}
	return messages, nil
}

// ApplySignatures merges MPC signatures into unsigned transaction hex.
func ApplySignatures(unsignedHex string, prevOuts []PrevOut, sigs []SignatureInput) (string, error) {
	raw, err := hex.DecodeString(unsignedHex)
	if err != nil {
		return "", fmt.Errorf("decode unsigned hex: %w", err)
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	if err := tx.Deserialize(bytes.NewReader(raw)); err != nil {
		return "", fmt.Errorf("decode tx: %w", err)
	}
	if len(tx.TxIn) != len(sigs) || len(tx.TxIn) != len(prevOuts) {
		return "", fmt.Errorf("input/signature count mismatch")
	}

	for i, sig := range sigs {
		po := prevOuts[i]
		derSig, err := encodeDERSignature(sig.Signature, txscript.SigHashAll)
		if err != nil {
			return "", fmt.Errorf("input %d der encode: %w", i, err)
		}
		if txscript.IsWitnessProgram(po.ScriptPubKey) {
			pubKey, err := btcaddr.CompressPubKey(sig.PubKey)
			if err != nil {
				return "", fmt.Errorf("input %d compress pubkey: %w", i, err)
			}
			tx.TxIn[i].Witness = wire.TxWitness{derSig, pubKey}
			tx.TxIn[i].SignatureScript = nil
		} else {
			pubKey, err := btcaddr.CompressPubKey(sig.PubKey)
			if err != nil {
				return "", fmt.Errorf("input %d compress pubkey: %w", i, err)
			}
			builder := txscript.NewScriptBuilder()
			builder.AddData(derSig).AddData(pubKey)
			script, err := builder.Script()
			if err != nil {
				return "", err
			}
			tx.TxIn[i].SignatureScript = script
		}
	}

	var buf bytes.Buffer
	if err := tx.Serialize(&buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf.Bytes()), nil
}

// VerifySigned checks signatures against prev outputs.
func VerifySigned(signedHex string, prevOuts []PrevOut) error {
	raw, err := hex.DecodeString(signedHex)
	if err != nil {
		return err
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	if err := tx.Deserialize(bytes.NewReader(raw)); err != nil {
		return err
	}
	if len(tx.TxIn) != len(prevOuts) {
		return fmt.Errorf("input count mismatch")
	}

	sigHashes := txscript.NewTxSigHashes(tx)

	for i, po := range prevOuts {
		var digest []byte
		if txscript.IsWitnessProgram(po.ScriptPubKey) {
			digest, err = txscript.CalcWitnessSigHash(po.ScriptPubKey, sigHashes, txscript.SigHashAll, tx, i, po.Amount)
		} else {
			digest, err = txscript.CalcSignatureHash(po.ScriptPubKey, txscript.SigHashAll, tx, i)
		}
		if err != nil {
			return err
		}

		if txscript.IsWitnessProgram(po.ScriptPubKey) {
			if len(tx.TxIn[i].Witness) < 2 {
				return fmt.Errorf("input %d missing witness", i)
			}
			if err := verifySigHash(tx.TxIn[i].Witness[1], digest, tx.TxIn[i].Witness[0]); err != nil {
				return fmt.Errorf("input %d verify: %w", i, err)
			}
			continue
		}
		pushes, err := txscript.PushedData(tx.TxIn[i].SignatureScript)
		if err != nil || len(pushes) < 2 {
			return fmt.Errorf("input %d missing signature/pubkey push", i)
		}
		sigBytes := pushes[0]
		pubKey := pushes[1]
		if err := verifySigHash(pubKey, digest, sigBytes); err != nil {
			return fmt.Errorf("input %d verify: %w", i, err)
		}
	}
	return nil
}

func verifySigHash(pubKey, digest, derSig []byte) error {
	if len(derSig) == 0 {
		return fmt.Errorf("empty signature")
	}
	hashType := derSig[len(derSig)-1]
	sigData := derSig[:len(derSig)-1]
	if hashType != byte(txscript.SigHashAll) {
		return fmt.Errorf("unexpected sighash type %d", hashType)
	}
	sig, err := ecdsa.ParseDERSignature(sigData)
	if err != nil {
		return err
	}
	pk, err := btcec.ParsePubKey(pubKey)
	if err != nil {
		return err
	}
	if !sig.Verify(digest, pk) {
		return fmt.Errorf("signature invalid")
	}
	return nil
}

func encodeDERSignature(sig []byte, hashType txscript.SigHashType) ([]byte, error) {
	if len(sig) != 64 {
		return nil, fmt.Errorf("signature must be 64 bytes R||S, got %d", len(sig))
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])
	s = normalizeSignatureS(s)
	der, err := encodeDERSignatureRS(r, s)
	if err != nil {
		return nil, err
	}
	return append(der, byte(hashType)), nil
}

func normalizeSignatureS(s *big.Int) *big.Int {
	curve := btcec.S256()
	halfN := new(big.Int).Rsh(curve.N, 1)
	if s.Cmp(halfN) > 0 {
		return new(big.Int).Sub(curve.N, s)
	}
	return s
}

func encodeDERSignatureRS(r, s *big.Int) ([]byte, error) {
	rb := trimLeadingZeros(r.Bytes())
	if len(rb) == 0 {
		rb = []byte{0}
	}
	if rb[0]&0x80 != 0 {
		rb = append([]byte{0}, rb...)
	}
	sb := trimLeadingZeros(s.Bytes())
	if len(sb) == 0 {
		sb = []byte{0}
	}
	if sb[0]&0x80 != 0 {
		sb = append([]byte{0}, sb...)
	}
	total := 2 + len(rb) + 2 + len(sb)
	if total > 127 {
		return nil, fmt.Errorf("DER signature too large")
	}
	var buf bytes.Buffer
	buf.WriteByte(0x30)
	buf.WriteByte(byte(total))
	buf.WriteByte(0x02)
	buf.WriteByte(byte(len(rb)))
	buf.Write(rb)
	buf.WriteByte(0x02)
	buf.WriteByte(byte(len(sb)))
	buf.Write(sb)
	return buf.Bytes(), nil
}

func trimLeadingZeros(b []byte) []byte {
	i := 0
	for i < len(b) && b[i] == 0 {
		i++
	}
	return b[i:]
}

// TxIDFromHex returns transaction id from signed raw hex.
func TxIDFromHex(txHex string) (string, error) {
	raw, err := hex.DecodeString(txHex)
	if err != nil {
		return "", err
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	if err := tx.Deserialize(bytes.NewReader(raw)); err != nil {
		return "", err
	}
	return tx.TxHash().String(), nil
}

// OutpointsFromHex extracts outpoints from unsigned/signed tx hex.
func OutpointsFromHex(txHex string) ([]Vin, error) {
	raw, err := hex.DecodeString(txHex)
	if err != nil {
		return nil, err
	}
	tx := wire.NewMsgTx(wire.TxVersion)
	if err := tx.Deserialize(bytes.NewReader(raw)); err != nil {
		return nil, err
	}
	vins := make([]Vin, 0, len(tx.TxIn))
	for _, in := range tx.TxIn {
		vins = append(vins, Vin{
			TxID: in.PreviousOutPoint.Hash.String(),
			Vout: in.PreviousOutPoint.Index,
		})
	}
	return vins, nil
}
