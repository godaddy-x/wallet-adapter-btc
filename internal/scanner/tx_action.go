package scanner

import (
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
)

type addrLeg struct {
	sourceKey string
	vins      []*models.Vin
	vouts     []*models.Vout
}

// inferBTCTxAction classifies send/receive/internal for a per-address extract leg.
func inferBTCTxAction(trx *models.Transaction, leg *addrLeg, param types.ScanTargetParam) string {
	if leg == nil {
		return "send"
	}
	hasVin := len(leg.vins) > 0
	hasVout := len(leg.vouts) > 0
	legAddr := legAddress(leg)

	switch {
	case hasVout && !hasVin:
		if vinSameAccount(trx, param, leg.sourceKey, legAddr) {
			return "internal"
		}
		return "receive"
	case hasVin && !hasVout:
		if voutSameAccount(trx, param, leg.sourceKey, legAddr) {
			return "internal"
		}
		return "send"
	case hasVin && hasVout:
		if voutSameAccount(trx, param, leg.sourceKey, legAddr) {
			return "internal"
		}
		return "send"
	default:
		return "send"
	}
}

func legAddress(leg *addrLeg) string {
	if len(leg.vins) > 0 {
		return normalizeScanAddress(leg.vins[0].Addr)
	}
	if len(leg.vouts) > 0 {
		return normalizeScanAddress(leg.vouts[0].Addr)
	}
	return ""
}

func accountSourceKey(param types.ScanTargetParam, addr string) string {
	addr = normalizeScanAddress(addr)
	if addr == "" {
		return ""
	}
	if sk, ok := param.ScanTarget[addr].(string); ok {
		return sk
	}
	return ""
}

func vinSameAccount(trx *models.Transaction, param types.ScanTargetParam, sourceKey, legAddr string) bool {
	if trx == nil || sourceKey == "" {
		return false
	}
	for _, vin := range trx.Vins {
		addr := normalizeScanAddress(vin.Addr)
		if addr == "" || addr == legAddr {
			continue
		}
		if accountSourceKey(param, addr) == sourceKey {
			return true
		}
	}
	return false
}

func voutSameAccount(trx *models.Transaction, param types.ScanTargetParam, sourceKey, legAddr string) bool {
	if trx == nil || sourceKey == "" {
		return false
	}
	for _, vout := range trx.Vouts {
		if vout.Type == "OP_RETURN" {
			continue
		}
		addr := normalizeScanAddress(vout.Addr)
		if addr == "" || addr == legAddr {
			continue
		}
		if accountSourceKey(param, addr) == sourceKey {
			return true
		}
	}
	return false
}
