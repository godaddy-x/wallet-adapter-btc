package models

const maxTxInSequenceNum = uint32(0xffffffff)

// BIP125ReplaceableSequenceMax is the exclusive upper bound for opt-in signaling.
const BIP125ReplaceableSequenceMax = maxTxInSequenceNum - 1

// IsBIP125Replaceable reports whether any non-coinbase input signals BIP125 replaceability.
func IsBIP125Replaceable(tx *Transaction) bool {
	if tx == nil {
		return false
	}
	for _, vin := range tx.Vins {
		if vin == nil || vin.Coinbase != "" {
			continue
		}
		if vin.Sequence < BIP125ReplaceableSequenceMax {
			return true
		}
	}
	return false
}
