package util

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/shopspring/decimal"
)

// StringNumToBigIntWithExp converts decimal string amount to satoshi big.Int.
func StringNumToBigIntWithExp(num string, decimals int32) *big.Int {
	num = strings.TrimSpace(num)
	if num == "" {
		return big.NewInt(0)
	}
	d, err := decimal.NewFromString(num)
	if err != nil {
		return big.NewInt(0)
	}
	shifted := d.Shift(decimals)
	return shifted.BigInt()
}

// ParseDecimalAmount parses a non-negative decimal amount string.
func ParseDecimalAmount(num string) (decimal.Decimal, error) {
	num = strings.TrimSpace(num)
	if num == "" {
		return decimal.Zero, fmt.Errorf("amount is empty")
	}
	d, err := decimal.NewFromString(num)
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid decimal %q: %w", num, err)
	}
	if d.IsNegative() {
		return decimal.Zero, fmt.Errorf("amount must be non-negative: %s", num)
	}
	return d, nil
}

// Decimal returns fixed string with chain decimals.
func Decimal(d decimal.Decimal, decimals int32) string {
	return d.StringFixed(decimals)
}
