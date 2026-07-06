package extparam

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter/types"
)

func TestOriginPrevOutsMap(t *testing.T) {
	if OriginPrevOutsMap(nil) != nil {
		t.Fatal("nil origin should return nil map")
	}
	m := OriginPrevOutsMap(&types.RawTransaction{
		ExtParam: map[string]string{KeyBTCPrevOuts: `[{"txid":"abc","vout":0}]`},
	})
	if m == nil || m[KeyOriginPrevOuts] == "" {
		t.Fatalf("map=%v", m)
	}
}
