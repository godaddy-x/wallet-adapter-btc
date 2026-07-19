package extparam

import (
	"strings"

	"github.com/godaddy-x/wallet-adapter/types"
)

const (
	KeyBTCPrevOuts        = "btcPrevOuts"
	KeyOriginPrevOuts     = "originBtcPrevOuts"
	KeyExcludeOutpoints   = "btcExcludeOutpoints" // server-side: locked outpoints to skip during listunspent select
	KeyDustDonatedSats    = "dust_donated_sats"   // sats merged from sub-dust change into miner fee
	KeyPayerSendOut       = "payerSendOut"        // JSON map payerAddress -> external sendOut amount
)

// OriginPrevOutsMap copies cached origin prevouts into a new ExtParam map for RBF rebuild.
func OriginPrevOutsMap(origin *types.RawTransaction) map[string]string {
	if origin == nil || origin.ExtParam == nil {
		return nil
	}
	prevOuts := strings.TrimSpace(origin.ExtParam[KeyBTCPrevOuts])
	if prevOuts == "" {
		return nil
	}
	return map[string]string{KeyOriginPrevOuts: prevOuts}
}
