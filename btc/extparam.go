package btc

import (
	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter/types"
)

const (
	ExtKeyBTCPrevOuts    = extparam.KeyBTCPrevOuts
	ExtKeyOriginPrevOuts = extparam.KeyOriginPrevOuts
)

// OriginPrevOutsExt copies origin pending prevout cache for RBF rebuild.
func OriginPrevOutsExt(origin *types.RawTransaction) map[string]string {
	return extparam.OriginPrevOutsMap(origin)
}
