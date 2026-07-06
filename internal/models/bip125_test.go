package models

import "testing"

func TestIsBIP125Replaceable(t *testing.T) {
	replaceable := &Transaction{
		Vins: []*Vin{{Sequence: 0xfffffffd}},
	}
	if !IsBIP125Replaceable(replaceable) {
		t.Fatal("expected replaceable for nSequence=0xfffffffd")
	}
	final := &Transaction{
		Vins: []*Vin{{Sequence: 0xffffffff}},
	}
	if IsBIP125Replaceable(final) {
		t.Fatal("expected non-replaceable for nSequence=0xffffffff")
	}
}
