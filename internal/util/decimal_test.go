package util

import "testing"

func TestParseDecimalAmount(t *testing.T) {
	d, err := ParseDecimalAmount("0.0001")
	if err != nil || d.IsZero() {
		t.Fatalf("valid amount: d=%s err=%v", d, err)
	}
	if _, err := ParseDecimalAmount(""); err == nil {
		t.Fatal("empty amount should fail")
	}
	if _, err := ParseDecimalAmount("not-a-number"); err == nil {
		t.Fatal("invalid decimal should fail")
	}
	if _, err := ParseDecimalAmount("-1"); err == nil {
		t.Fatal("negative amount should fail")
	}
}
