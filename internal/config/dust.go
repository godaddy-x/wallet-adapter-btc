package config

import (
	"fmt"

	"github.com/shopspring/decimal"
)

const (
	// DefaultDustLimitSats is the mainnet P2WPKH dust threshold (546 sats).
	DefaultDustLimitSats = 546
	// MaxDustLimitSats caps configurable dust (2× default); exceeding panics at load.
	MaxDustLimitSats = DefaultDustLimitSats * 2
)

// ValidateDustConfig panics when DustLimitSats exceeds MaxDustLimitSats.
func (c *WalletConfig) ValidateDustConfig() {
	if c == nil {
		return
	}
	sats := c.EffectiveDustLimitSats()
	if sats > MaxDustLimitSats {
		panic(fmt.Sprintf("dustLimitSats exceeds max allowed: got %d, max %d", sats, MaxDustLimitSats))
	}
}

// EffectiveDustLimitSats returns configured dust threshold in sats (default 546).
func (c *WalletConfig) EffectiveDustLimitSats() int64 {
	if c == nil || c.DustLimitSats <= 0 {
		return DefaultDustLimitSats
	}
	return c.DustLimitSats
}

// DustLimitBTC returns the effective dust threshold as BTC (8 decimals).
func (c *WalletConfig) DustLimitBTC() decimal.Decimal {
	return decimal.New(c.EffectiveDustLimitSats(), -c.decimalsOrDefault())
}

func (c *WalletConfig) decimalsOrDefault() int32 {
	if c == nil || c.Decimals <= 0 {
		return DefaultDecimals
	}
	return c.Decimals
}
