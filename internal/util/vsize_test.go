package util

import "testing"

func TestSegwitTxVsize(t *testing.T) {
	if v := SegwitTxVsize(1, 2); v != 1*68+2*31+11 {
		t.Fatalf("vsize=%d", v)
	}
	if v := EstimateTxVsize(1, 2); v != SegwitTxVsize(1, 2) {
		t.Fatalf("EstimateTxVsize=%d", v)
	}
}
