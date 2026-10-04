package manager

import (
	"strings"
	"testing"
)

func TestValidateRequestedTxIDOK(t *testing.T) {
	if err := validateRequestedTxID("abc", "ABC"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRequestedTxIDMismatch(t *testing.T) {
	err := validateRequestedTxID("aaa", "bbb")
	if err == nil || !strings.Contains(err.Error(), "txid mismatch") {
		t.Fatalf("want mismatch, got %v", err)
	}
}

func TestValidateRequestedTxIDMissingResponse(t *testing.T) {
	err := validateRequestedTxID("aaa", "")
	if err == nil || !strings.Contains(err.Error(), "missing txid") {
		t.Fatalf("want missing txid, got %v", err)
	}
}
