package decoder

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/godaddy-x/wallet-adapter/wallet"
	"github.com/shopspring/decimal"
)

// BIP125 incremental relay fee assumption: 1 sat/vbyte (bitcoind default incrementalRelayFee).
var satPerVByte = decimal.New(1, -8)

// BTC SpeedUp/Cancel reuse wallet-adapter types.SpeedUp (aligned with ETH API surface):
//   - speedUp.nonce     = origin txid (pending tx to replace via BIP125 RBF)
//   - speedUp.fromAddress = change / cancel payout address (optional)
//   - feeRate / baseFeeRate + feeBumpPercent / feeBumpWei = bumped feerate (BTC/KB; bumpWei treated as BTC/KB increment)

func parseSpeedUpOriginTxID(su *types.SpeedUp) (string, error) {
	if su == nil || strings.TrimSpace(su.Nonce) == "" {
		return "", types.Errorf(types.ErrCreateRawTransactionFailed, "speedUp.nonce (origin txid) is required for BTC RBF")
	}
	return strings.TrimSpace(su.Nonce), nil
}

func applySpeedUpFeeRate(su *types.SpeedUp, rawTx *types.RawTransaction) error {
	if su == nil || !su.Active() {
		return nil
	}

	var rate decimal.Decimal
	var err error
	if fr := strings.TrimSpace(su.FeeRate); fr != "" {
		rate, err = decimal.NewFromString(fr)
		if err != nil {
			return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid speedUp.feeRate: %v", err)
		}
	} else {
		baseStr := strings.TrimSpace(su.BaseFeeRate)
		if baseStr == "" && rawTx != nil {
			baseStr = strings.TrimSpace(rawTx.FeeRate)
		}
		if baseStr == "" {
			return types.Errorf(types.ErrCreateRawTransactionFailed,
				"speedUp requires feeRate or baseFeeRate (with optional feeBumpPercent/feeBumpWei)")
		}
		rate, err = decimal.NewFromString(baseStr)
		if err != nil {
			return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid speedUp base feeRate: %v", err)
		}
		if su.FeeBumpPercent > 0 {
			bump := rate.Mul(decimal.NewFromInt(int64(su.FeeBumpPercent))).Div(decimal.NewFromInt(100))
			rate = rate.Add(bump)
		}
		if su.FeeBumpWei != "" {
			inc, err := decimal.NewFromString(strings.TrimSpace(su.FeeBumpWei))
			if err != nil {
				return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid speedUp.feeBumpWei: %v", err)
			}
			rate = rate.Add(inc)
		}
	}
	if rate.LessThanOrEqual(decimal.Zero) {
		return types.Errorf(types.ErrCreateRawTransactionFailed, "speedUp feeRate must be positive")
	}
	if rawTx != nil {
		rawTx.FeeRate = rate.StringFixed(8)
	}
	return nil
}

func isCancelReplacement(rawTx *types.RawTransaction) (bool, error) {
	if rawTx == nil || len(rawTx.To) == 0 {
		return false, types.Errorf(types.ErrCreateRawTransactionFailed, "to is required for RBF replacement")
	}
	for addr, v := range rawTx.To {
		d, err := decimal.NewFromString(strings.TrimSpace(v))
		if err != nil {
			return false, types.Errorf(types.ErrCreateRawTransactionFailed, "invalid cancel amount for %s: %v", addr, err)
		}
		if !d.Equal(decimal.Zero) {
			return false, nil
		}
	}
	return true, nil
}

func resolveRBFChangeAddress(rawTx *types.RawTransaction, inputs []*models.Unspent) (string, error) {
	if rawTx != nil && rawTx.SpeedUp != nil {
		if pinned := strings.TrimSpace(rawTx.SpeedUp.FromAddress); pinned != "" {
			return pinned, nil
		}
	}
	if len(inputs) == 0 {
		return "", fmt.Errorf("no inputs for RBF change address")
	}
	if inputs[0].Address != "" {
		return inputs[0].Address, nil
	}
	return "", fmt.Errorf("cannot resolve RBF change address")
}

func vinToUnspent(vin *models.Vin) (*models.Unspent, decimal.Decimal, error) {
	if vin == nil || vin.Coinbase != "" {
		return nil, decimal.Zero, fmt.Errorf("coinbase input cannot be replaced")
	}
	if strings.TrimSpace(vin.Value) == "" || strings.TrimSpace(vin.ScriptPubKey) == "" {
		return nil, decimal.Zero, fmt.Errorf("origin input %s:%d missing prevout data", vin.TxID, vin.Vout)
	}
	amt, err := decimal.NewFromString(vin.Value)
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("origin input %s:%d invalid amount: %w", vin.TxID, vin.Vout, err)
	}
	return &models.Unspent{
		TxID:         vin.TxID,
		Vout:         vin.Vout,
		Address:      vin.Addr,
		ScriptPubKey: vin.ScriptPubKey,
		Amount:       vin.Value,
		Spendable:    true,
	}, amt, nil
}

func paidFeeFromTransaction(tx *models.Transaction) decimal.Decimal {
	if tx == nil {
		return decimal.Zero
	}
	inputs := decimal.Zero
	for _, vin := range tx.Vins {
		if v, err := decimal.NewFromString(strings.TrimSpace(vin.Value)); err == nil {
			inputs = inputs.Add(v)
		}
	}
	outputs := decimal.Zero
	for _, vout := range tx.Vouts {
		if v, err := decimal.NewFromString(strings.TrimSpace(vout.Value)); err == nil {
			outputs = outputs.Add(v)
		}
	}
	fee := inputs.Sub(outputs)
	if fee.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	return fee
}

func (d *BtcTransactionDecoder) resolveOriginPaidFee(originTxID string, origin *models.Transaction) decimal.Decimal {
	if d != nil && d.Wm != nil {
		if fee, err := d.Wm.MempoolTransactionFee(originTxID); err == nil && fee.GreaterThan(decimal.Zero) {
			return fee
		}
	}
	return paidFeeFromTransaction(origin)
}

// enforceRBFMinimumFee ensures replacement absolute fee satisfies BIP125 mempool policy:
// newFee >= originFee + incrementalRelayFee(newVsize).
func (d *BtcTransactionDecoder) enforceRBFMinimumFee(origin *models.Transaction, inputs, outputs int64, proposedFee decimal.Decimal) decimal.Decimal {
	originTxID := ""
	if origin != nil {
		originTxID = origin.TxID
	}
	originFee := d.resolveOriginPaidFee(originTxID, origin)
	if originFee.LessThanOrEqual(decimal.Zero) {
		return proposedFee
	}
	newVsize := util.EstimateTxVsize(inputs, outputs)
	incremental := decimal.New(newVsize, 0).Mul(satPerVByte)
	minFee := originFee.Add(incremental)
	if minFee.LessThanOrEqual(originFee) {
		minFee = originFee.Add(satPerVByte)
	}
	if proposedFee.LessThan(minFee) {
		return minFee
	}
	return proposedFee
}

func (d *BtcTransactionDecoder) resolveOriginUnspent(vin *models.Vin) (*models.Unspent, decimal.Decimal, error) {
	if vin == nil {
		return nil, decimal.Zero, fmt.Errorf("vin is nil")
	}
	if vin.Coinbase != "" {
		return nil, decimal.Zero, fmt.Errorf("coinbase input cannot be replaced")
	}
	if strings.TrimSpace(vin.ScriptPubKey) == "" && strings.TrimSpace(vin.Addr) != "" {
		script, err := btcaddr.PayToAddressScript(vin.Addr, d.Wm.Config.NetworkName())
		if err == nil {
			vin.ScriptPubKey = hex.EncodeToString(script)
		}
	}
	if u, amt, err := vinToUnspent(vin); err == nil {
		return u, amt, nil
	}
	out, err := d.Wm.GetTxOut(vin.TxID, vin.Vout)
	if err != nil {
		parent, perr := d.Wm.GetTransaction(vin.TxID)
		if perr != nil || int(vin.Vout) >= len(parent.Vouts) {
			return nil, decimal.Zero, fmt.Errorf("origin input %s:%d unavailable: %w", vin.TxID, vin.Vout, err)
		}
		out = parent.Vouts[vin.Vout]
	}
	amt, err := decimal.NewFromString(out.Value)
	if err != nil {
		return nil, decimal.Zero, fmt.Errorf("origin input %s:%d invalid amount: %w", vin.TxID, vin.Vout, err)
	}
	return &models.Unspent{
		TxID:         vin.TxID,
		Vout:         vin.Vout,
		Address:      out.Addr,
		ScriptPubKey: out.ScriptPubKey,
		Amount:       out.Value,
		Spendable:    true,
	}, amt, nil
}

func (d *BtcTransactionDecoder) collectOriginInputs(origin *models.Transaction, rawTx *types.RawTransaction) ([]*models.Unspent, decimal.Decimal, error) {
	if cached, ok := unspentsFromOriginExt(rawTx); ok {
		if origin == nil || len(origin.Vins) == 0 {
			ok = false
		} else if err := validateOriginPrevOutCache(cached, origin.Vins); err != nil {
			return nil, decimal.Zero, err
		}
		if ok {
			total := decimal.Zero
			for _, u := range cached {
				amt, err := decimal.NewFromString(u.Amount)
				if err != nil {
					return nil, decimal.Zero, err
				}
				total = total.Add(amt)
			}
			return cached, total, nil
		}
	}
	if origin == nil {
		return nil, decimal.Zero, fmt.Errorf("origin transaction is nil")
	}
	total := decimal.Zero
	used := make([]*models.Unspent, 0, len(origin.Vins))
	for _, vin := range origin.Vins {
		u, amt, err := d.resolveOriginUnspent(vin)
		if err != nil {
			return nil, decimal.Zero, err
		}
		total = total.Add(amt)
		used = append(used, u)
	}
	if len(used) == 0 {
		return nil, decimal.Zero, fmt.Errorf("origin transaction has no inputs")
	}
	return used, total, nil
}

// createRBFTransfer rebuilds a BIP125 replacement (SpeedUp or Cancel) using fixed origin inputs.
func (d *BtcTransactionDecoder) createRBFTransfer(wrapper wallet.WalletDAI, rawTx *types.RawTransaction) error {
	if !d.Wm.Config.EnableRBF {
		return types.Errorf(types.ErrCreateRawTransactionFailed, "enableRBF must be true for speedUp/cancel")
	}
	originTxID, err := parseSpeedUpOriginTxID(rawTx.SpeedUp)
	if err != nil {
		return err
	}
	if err := applySpeedUpFeeRate(rawTx.SpeedUp, rawTx); err != nil {
		return err
	}
	feeRate, err := decimal.NewFromString(strings.TrimSpace(rawTx.FeeRate))
	if err != nil {
		return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid feeRate after speedUp: %v", err)
	}

	origin, err := d.Wm.GetTransaction(originTxID)
	if err != nil {
		return types.Errorf(types.ErrCreateRawTransactionFailed, "load origin tx %s: %v", originTxID, err)
	}
	if !models.IsBIP125Replaceable(origin) {
		return types.Errorf(types.ErrCreateRawTransactionFailed, "origin tx is not BIP125 replaceable")
	}
	usedUTXO, inputTotal, err := d.collectOriginInputs(origin, rawTx)
	if err != nil {
		return err
	}

	outputAddrs := make(map[string]decimal.Decimal)
	cancel, err := isCancelReplacement(rawTx)
	if err != nil {
		return err
	}

	if cancel {
		changeAddr, err := resolveRBFChangeAddress(rawTx, usedUTXO)
		if err != nil {
			return types.Errorf(types.ErrCreateRawTransactionFailed, "%v", err)
		}
		fees, err := d.Wm.EstimateFee(int64(len(usedUTXO)), 1, feeRate)
		if err != nil {
			return err
		}
		fees = d.enforceRBFMinimumFee(origin, int64(len(usedUTXO)), 1, fees)
		changeAmount := inputTotal.Sub(fees)
		if changeAmount.LessThanOrEqual(decimal.Zero) {
			return types.Errorf(types.ErrInsufficientBalanceOfAccount, "inputs cannot cover cancel fee")
		}
		outputAddrs[changeAddr] = changeAmount
		rawTx.Fees = util.Decimal(fees, d.Wm.Decimal())
	} else {
		totalSend := decimal.Zero
		for addr, amount := range rawTx.To {
			decamount, err := decimal.NewFromString(strings.TrimSpace(amount))
			if err != nil {
				return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid send amount for %s: %v", addr, err)
			}
			if decamount.LessThanOrEqual(decimal.Zero) {
				return types.Errorf(types.ErrCreateRawTransactionFailed, "send amount must be positive for %s", addr)
			}
			totalSend = totalSend.Add(decamount)
			outputAddrs = appendOutput(outputAddrs, addr, decamount)
		}
		outputCount := int64(len(outputAddrs))
		fees, err := d.Wm.EstimateFee(int64(len(usedUTXO)), outputCount+1, feeRate)
		if err != nil {
			return err
		}
		fees = d.enforceRBFMinimumFee(origin, int64(len(usedUTXO)), outputCount+1, fees)
		changeAmount := inputTotal.Sub(totalSend).Sub(fees)
		if changeAmount.LessThan(decimal.Zero) {
			fees, err = d.Wm.EstimateFee(int64(len(usedUTXO)), outputCount, feeRate)
			if err != nil {
				return err
			}
			fees = d.enforceRBFMinimumFee(origin, int64(len(usedUTXO)), outputCount, fees)
			changeAmount = inputTotal.Sub(totalSend).Sub(fees)
			if changeAmount.LessThan(decimal.Zero) {
				return types.Errorf(types.ErrInsufficientBalanceOfAccount, "origin inputs cannot cover send + bumped fee")
			}
		} else if changeAmount.GreaterThan(decimal.Zero) {
			changeAddr, err := resolveRBFChangeAddress(rawTx, usedUTXO)
			if err != nil {
				return types.Errorf(types.ErrCreateRawTransactionFailed, "%v", err)
			}
			outputAddrs = appendOutput(outputAddrs, changeAddr, changeAmount)
		}
		rawTx.Fees = util.Decimal(fees, d.Wm.Decimal())
	}

	return d.buildRawTransaction(wrapper, rawTx, usedUTXO, outputAddrs)
}
