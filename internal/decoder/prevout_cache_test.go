package decoder

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

func TestValidateOriginPrevOutCache(t *testing.T) {
	cached := []*models.Unspent{{TxID: "abc", Vout: 1}}
	vins := []*models.Vin{{TxID: "abc", Vout: 1}}
	if err := validateOriginPrevOutCache(cached, vins); err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	vins[0].TxID = "def"
	if err := validateOriginPrevOutCache(cached, vins); err == nil {
		t.Fatal("expected mismatch error")
	}
}
