package manager

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

// MempoolTransactionFee returns absolute fee (BTC) paid by an unconfirmed mempool tx.
func (wm *WalletManager) MempoolTransactionFee(txid string) (decimal.Decimal, error) {
	if wm == nil || wm.Client == nil {
		return decimal.Zero, fmt.Errorf("rpc client unavailable")
	}
	txid = strings.TrimSpace(txid)
	if txid == "" {
		return decimal.Zero, fmt.Errorf("empty txid")
	}
	result, err := wm.Client.Call("getmempoolentry", []interface{}{txid})
	if err != nil {
		return decimal.Zero, err
	}
	if result == nil || !result.Exists() {
		return decimal.Zero, fmt.Errorf("mempool entry not found")
	}
	feeStr := gjson.Get(result.Raw, "fees.base").String()
	if feeStr == "" {
		feeStr = gjson.Get(result.Raw, "fee").String()
	}
	if feeStr == "" {
		return decimal.Zero, fmt.Errorf("mempool fee unavailable")
	}
	fee, err := decimal.NewFromString(feeStr)
	if err != nil {
		return decimal.Zero, err
	}
	return fee, nil
}
