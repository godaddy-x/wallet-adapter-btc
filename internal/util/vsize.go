package util

// SegwitTxVsize estimates vsize for native P2WPKH (bech32) transactions built by this adapter.
// Reference: ~68 vB/input, ~31 vB/output, ~11 vB overhead (1-in-2-out ≈ 141 vB).
func SegwitTxVsize(inputs, outputs int64) int64 {
	return inputs*68 + outputs*31 + 11
}

// EstimateTxVsize returns the vsize model used for fee estimation (segwit/bech32 wallet).
func EstimateTxVsize(inputs, outputs int64) int64 {
	return SegwitTxVsize(inputs, outputs)
}
