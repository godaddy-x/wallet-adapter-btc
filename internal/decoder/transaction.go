package decoder

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter-btc/internal/txbuild"
	"github.com/godaddy-x/wallet-adapter-btc/internal/util"
	"github.com/godaddy-x/wallet-adapter/decoder"
	"github.com/godaddy-x/wallet-adapter/types"
	"github.com/godaddy-x/wallet-adapter/wallet"
	"github.com/shopspring/decimal"
)

// BtcTransactionDecoder implements decoder.TransactionDecoder for native BTC transfers.
type BtcTransactionDecoder struct {
	decoder.TransactionDecoderBase
	Wm *manager.WalletManager
}

// NewTransactionDecoder creates BTC transaction decoder.
func NewTransactionDecoder(wm *manager.WalletManager) *BtcTransactionDecoder {
	return &BtcTransactionDecoder{Wm: wm}
}

// CreateRawTransaction builds unsigned raw transaction with per-input sighash messages.
func (d *BtcTransactionDecoder) CreateRawTransaction(wrapper wallet.WalletDAI, rawTx *types.RawTransaction) error {
	if rawTx.SpeedUp != nil && rawTx.SpeedUp.Active() {
		if err := d.createRBFTransfer(wrapper, rawTx); err != nil {
			return err
		}
		return fillBTCSignExt(&rawTx.SignExt, d.Wm)
	}
	if err := d.createBTCTransfer(wrapper, rawTx); err != nil {
		return err
	}
	return fillBTCSignExt(&rawTx.SignExt, d.Wm)
}

// VerifyRawTransaction merges MPC signatures and validates the signed transaction.
func (d *BtcTransactionDecoder) VerifyRawTransaction(wrapper wallet.WalletDAI, rawTx *types.RawTransaction) error {
	signedTrans, prevOuts, err := d.buildSignedNativeTransaction(rawTx)
	if err != nil {
		return err
	}
	if err := txbuild.VerifySigned(signedTrans, prevOuts); err != nil {
		rawTx.IsCompleted = false
		return types.Errorf(types.ErrVerifyRawTransactionFailed, "transaction verify failed: %v", err)
	}
	rawTx.IsCompleted = true
	rawTx.RawHex = signedTrans
	if txid, err := txIDFromSignedHex(signedTrans); err == nil {
		rawTx.TxID = txid
	}
	return nil
}

// SubmitRawTransaction broadcasts signed transaction.
func (d *BtcTransactionDecoder) SubmitRawTransaction(wrapper wallet.WalletDAI, rawTx *types.RawTransaction) (*types.Transaction, error) {
	if len(rawTx.RawHex) == 0 {
		return nil, fmt.Errorf("transaction hex is empty")
	}
	if !rawTx.IsCompleted {
		return nil, fmt.Errorf("transaction is not completed validation")
	}
	txid, err := txIDFromSignedHex(rawTx.RawHex)
	if err != nil {
		return nil, fmt.Errorf("compute txid: %w", err)
	}
	rawTx.TxID = txid
	nodeTxid, err := d.Wm.SendRawTransaction(rawTx.RawHex)
	if err != nil {
		return nil, fmt.Errorf("broadcast: %w", err)
	}
	if nodeTxid != "" && !strings.EqualFold(strings.TrimSpace(nodeTxid), txid) {
		return nil, fmt.Errorf("broadcast txid mismatch: local=%s node=%s", txid, nodeTxid)
	}
	if nodeTxid != "" {
		txid = strings.TrimSpace(nodeTxid)
	}
	rawTx.TxID = txid
	rawTx.IsSubmit = true
	fromAddrs, fromAmts := splitAddrAmount(rawTx.TxFrom)
	fromAddrs, fromAmts = aggregateAddrAmountLegs(fromAddrs, fromAmts)
	toAddrs, toAmts := splitAddrAmount(rawTx.TxTo)
	decimals := d.Wm.Decimal()
	fees := rawTx.Fees
	return &types.Transaction{
		TxID:       txid,
		AccountID:  rawTx.Account.AccountID,
		Coin:       rawTx.Coin,
		FromAddr:   fromAddrs,
		FromAmt:    fromAmts,
		ToAddr:     toAddrs,
		ToAmt:      toAmts,
		Amount:     rawTx.TxAmount,
		Decimal:    decimals,
		Fees:       fees,
		TxAction:   inferSubmitTxAction(fromAddrs, toAddrs),
		SubmitTime: time.Now().Unix(),
		Status:     types.TxStatusSuccess,
	}, nil
}

// GetRawTransactionFeeRate returns suggested fee rate in BTC/KB.
func (d *BtcTransactionDecoder) GetRawTransactionFeeRate(wrapper wallet.WalletDAI) (feeRate, unit string, err error) {
	rate, err := d.Wm.EstimateFeeRate()
	if err != nil {
		return "", "", err
	}
	return util.Decimal(rate, d.Wm.Decimal()), "K", nil
}

// EstimateRawTransactionFee estimates fee for a transfer request.
func (d *BtcTransactionDecoder) EstimateRawTransactionFee(wrapper wallet.WalletDAI, rawTx *types.RawTransaction) error {
	rate, err := d.Wm.EstimateFeeRate()
	if err != nil {
		return err
	}
	if len(rawTx.FeeRate) > 0 {
		rate, err = util.ParseDecimalAmount(rawTx.FeeRate)
		if err != nil {
			return err
		}
	}
	fee, err := d.Wm.EstimateFee(2, int64(len(rawTx.To)+1), rate)
	if err != nil {
		return err
	}
	rawTx.Fees = util.Decimal(fee, d.Wm.Decimal())
	rawTx.FeeRate = util.Decimal(rate, d.Wm.Decimal())
	return nil
}

// CreateSummaryRawTransactionWithError builds summary sweep transactions.
func (d *BtcTransactionDecoder) CreateSummaryRawTransactionWithError(wrapper wallet.WalletDAI, sumRawTx *types.SummaryRawTransaction) ([]*types.RawTransactionWithError, error) {
	return d.createBTCSummary(wrapper, sumRawTx)
}

func (d *BtcTransactionDecoder) createBTCTransfer(wrapper wallet.WalletDAI, rawTx *types.RawTransaction) error {
	if rawTx.Account == nil {
		return types.Errorf(types.ErrCreateRawTransactionFailed, "account is nil")
	}
	accountID := rawTx.Account.AccountID
	addresses, _, err := wrapper.GetAddressList(wallet.SearchParams{AccountID: accountID, Limit: 2000})
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		return types.Errorf(types.ErrAddressNotFound, "[%s] have not addresses", accountID)
	}
	searchAddrs := make([]string, 0, len(addresses))
	for _, addr := range addresses {
		searchAddrs = append(searchAddrs, addr.Address)
	}
	unspents, err := d.Wm.ListUnspent(0, searchAddrs...)
	if err != nil {
		return err
	}
	unspents = filterExcludedUnspents(unspents, rawTx)
	if len(unspents) == 0 {
		return types.Errorf(types.ErrInsufficientBalanceOfAccount, "[%s] balance is not enough", accountID)
	}
	if len(rawTx.To) == 0 {
		return errors.New("receiver addresses is empty")
	}

	totalSend := decimal.Zero
	destinations := make([]string, 0)
	for addr, amount := range rawTx.To {
		deamount, err := util.ParseDecimalAmount(amount)
		if err != nil {
			return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid amount for %s: %v", addr, err)
		}
		if deamount.IsZero() {
			return types.Errorf(types.ErrCreateRawTransactionFailed, "amount for %s must be positive", addr)
		}
		totalSend = totalSend.Add(deamount)
		destinations = append(destinations, addr)
	}

	feesRate := decimal.Zero
	if len(rawTx.FeeRate) == 0 {
		feesRate, err = d.Wm.EstimateFeeRate()
		if err != nil {
			return err
		}
	} else {
		feesRate, err = util.ParseDecimalAmount(rawTx.FeeRate)
		if err != nil {
			return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid feeRate: %v", err)
		}
	}

	sel, err := selectUTXOsForPayment(
		unspents,
		totalSend,
		feesRate,
		d.Wm.Config.MaxTxInputs,
		len(destinations)+1,
		d.Wm.EstimateFee,
	)
	if err != nil {
		return types.Errorf(types.ErrInsufficientBalanceOfAccount, "%s", err.Error())
	}

	rawTx.FeeRate = util.Decimal(sel.FeeRate, d.Wm.Decimal())
	rawTx.Fees = util.Decimal(sel.Fees, d.Wm.Decimal())

	outputAddrs := make(map[string]decimal.Decimal)
	for to, amount := range rawTx.To {
		decamount, err := util.ParseDecimalAmount(amount)
		if err != nil {
			return types.Errorf(types.ErrCreateRawTransactionFailed, "invalid amount for %s: %v", to, err)
		}
		outputAddrs = appendOutput(outputAddrs, to, decamount)
	}
	if sel.ChangeAmount.GreaterThan(decimal.Zero) {
		outputAddrs = appendOutput(outputAddrs, sel.ChangeAddr, sel.ChangeAmount)
	}
	return d.buildRawTransaction(wrapper, rawTx, sel.UsedUTXO, outputAddrs)
}

func (d *BtcTransactionDecoder) createBTCSummary(wrapper wallet.WalletDAI, sumRawTx *types.SummaryRawTransaction) ([]*types.RawTransactionWithError, error) {
	accountID := sumRawTx.Account.AccountID
	minTransfer, err := util.ParseDecimalAmount(sumRawTx.MinTransfer)
	if err != nil {
		return nil, fmt.Errorf("invalid minTransfer: %w", err)
	}
	limit := sumRawTx.AddressLimit
	if limit <= 0 {
		limit = 2000
	}
	addresses, _, err := wrapper.GetAddressList(wallet.SearchParams{
		AccountID: accountID,
		LastID:    sumRawTx.AddressStartIndex,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("[%s] have not addresses", accountID)
	}

	searchAddrs := make([]string, 0, len(addresses))
	for _, address := range addresses {
		searchAddrs = append(searchAddrs, address.Address)
	}
	balances, err := d.Wm.GetBalanceByAddresses(searchAddrs...)
	if err != nil {
		return nil, err
	}
	sumAddresses := make([]string, 0)
	for _, bal := range balances {
		addrBalance, err := util.ParseDecimalAmount(bal.Balance)
		if err != nil {
			continue
		}
		if addrBalance.GreaterThanOrEqual(minTransfer) {
			sumAddresses = append(sumAddresses, bal.Address)
		}
	}
	if len(sumAddresses) == 0 {
		return nil, nil
	}

	feesRate := decimal.Zero
	if len(sumRawTx.FeeRate) == 0 {
		feesRate, err = d.Wm.EstimateFeeRate()
		if err != nil {
			return nil, err
		}
	} else {
		feesRate, err = util.ParseDecimalAmount(sumRawTx.FeeRate)
		if err != nil {
			return nil, fmt.Errorf("invalid feeRate: %w", err)
		}
	}

	rawTxArray := make([]*types.RawTransactionWithError, 0)
	sumUnspents := make([]*models.Unspent, 0)
	outputAddrs := make(map[string]decimal.Decimal)
	excludeSet := parseSummaryExcludeVinKeys(sumRawTx.ExtParam)

	for i, addr := range sumAddresses {
		unspents, err := d.Wm.ListUnspent(sumRawTx.Confirms, addr)
		if err != nil {
			return nil, err
		}
		unspents = filterExcludedUnspentsSet(unspents, excludeSet)
		unspentLimit := d.Wm.Config.MaxTxInputs - len(sumUnspents)
		if unspentLimit > 0 {
			if len(unspents) > unspentLimit {
				sumUnspents = append(sumUnspents, unspents[:unspentLimit]...)
			} else {
				sumUnspents = append(sumUnspents, unspents...)
			}
		}
		if i == len(sumAddresses)-1 || len(sumUnspents) >= d.Wm.Config.MaxTxInputs {
			fees, _ := d.Wm.EstimateFee(int64(len(sumUnspents)), int64(len(outputAddrs)+1), feesRate)
			totalInputAmount := decimal.Zero
			for _, u := range sumUnspents {
				if !u.Spendable {
					continue
				}
				ua, err := parseUTXOAmount(u)
				if err != nil {
					return nil, err
				}
				totalInputAmount = totalInputAmount.Add(ua)
			}
			sumAmount := totalInputAmount.Sub(fees)
			if sumAmount.GreaterThan(decimal.Zero) {
				outputAddrs = appendOutput(outputAddrs, sumRawTx.SummaryAddress, sumAmount)
				rawTxTo := make(map[string]string)
				for a, m := range outputAddrs {
					rawTxTo[a] = util.Decimal(m, d.Wm.Decimal())
				}
				rawTx := &types.RawTransaction{
					Coin:     sumRawTx.Coin,
					Account:  sumRawTx.Account,
					FeeRate:  util.Decimal(feesRate, d.Wm.Decimal()),
					To:       rawTxTo,
					Fees:     util.Decimal(fees, d.Wm.Decimal()),
					Required: 1,
				}
				if err := d.buildRawTransaction(wrapper, rawTx, sumUnspents, outputAddrs); err != nil {
					return nil, err
				}
				if err := fillBTCSignExt(&rawTx.SignExt, d.Wm); err != nil {
					return nil, err
				}
				rawTxArray = append(rawTxArray, &types.RawTransactionWithError{
					RawTx: rawTx,
				})
			}
			sumUnspents = make([]*models.Unspent, 0)
			outputAddrs = make(map[string]decimal.Decimal)
		}
	}
	return rawTxArray, nil
}

func (d *BtcTransactionDecoder) buildRawTransaction(
	wrapper wallet.WalletDAI,
	rawTx *types.RawTransaction,
	usedUTXO []*models.Unspent,
	to map[string]decimal.Decimal,
) error {
	if len(usedUTXO) == 0 {
		return fmt.Errorf("utxo is empty")
	}
	if len(to) == 0 {
		return fmt.Errorf("receiver addresses is empty")
	}
	accountID := rawTx.Account.AccountID
	accountTotalSent := decimal.Zero
	for addr, amount := range to {
		addresses, _, findErr := wrapper.GetAddressList(wallet.SearchParams{
			AccountID: accountID,
			Address:   addr,
			Limit:     1,
		})
		if findErr != nil || len(addresses) == 0 {
			accountTotalSent = accountTotalSent.Add(amount)
		}
	}
	if len(usedUTXO) > d.Wm.Config.MaxTxInputs {
		return fmt.Errorf("transaction inputs exceed max: %d", d.Wm.Config.MaxTxInputs)
	}

	vins := make([]txbuild.Vin, 0, len(usedUTXO))
	vouts := make([]txbuild.Vout, 0, len(to))
	prevOuts := make([]txbuild.PrevOut, 0, len(usedUTXO))
	txFrom := make([]string, 0, len(usedUTXO))
	txTo := make([]string, 0, len(to))

	for _, utxo := range usedUTXO {
		vins = append(vins, txbuild.Vin{TxID: utxo.TxID, Vout: uint32(utxo.Vout)})
		txAmount := util.StringNumToBigIntWithExp(utxo.Amount, d.Wm.Decimal())
		script, err := decodeScriptHex(utxo.ScriptPubKey)
		if err != nil {
			return fmt.Errorf("utxo script decode: %w", err)
		}
		prevOuts = append(prevOuts, txbuild.PrevOut{
			ScriptPubKey: script,
			Amount:       txAmount.Int64(),
			Address:      utxo.Address,
		})
		txFrom = append(txFrom, fmt.Sprintf("%s:%s", utxo.Address, utxo.Amount))
	}
	for addr, amount := range to {
		txTo = append(txTo, fmt.Sprintf("%s:%s", addr, amount.String()))
		shifted := amount.Shift(d.Wm.Decimal())
		vouts = append(vouts, txbuild.Vout{Address: addr, Amount: shifted.IntPart()})
	}

	emptyTrans, messages, err := txbuild.BuildUnsigned(vins, vouts, prevOuts, d.txReplaceable(rawTx), d.Wm.Config.NetworkName())
	if err != nil {
		return fmt.Errorf("create transaction failed: %w", err)
	}
	storePrevOutsExt(rawTx, vins, prevOuts)
	if rawTx.Signatures == nil {
		rawTx.Signatures = make(map[string][]*types.KeySignature)
	}
	keySigs := make([]*types.KeySignature, 0, len(messages))
	for _, msg := range messages {
		addr, err := wrapper.GetAddress(wallet.SearchParams{Address: msg.Address})
		if err != nil {
			return err
		}
		keySigs = append(keySigs, &types.KeySignature{
			EccType: d.Wm.Config.CurveType,
			Address: addr,
			Message: msg.Hash,
		})
	}
	feesDec, err := util.ParseDecimalAmount(rawTx.Fees)
	if err != nil {
		return fmt.Errorf("invalid fees: %w", err)
	}
	accountTotalSent = accountTotalSent.Add(feesDec)
	accountTotalSent = decimal.Zero.Sub(accountTotalSent)
	rawTx.Signatures[accountID] = keySigs
	rawTx.RawHex = emptyTrans
	rawTx.IsBuilt = true
	rawTx.TxAmount = accountTotalSent.StringFixed(d.Wm.Decimal())
	fromAddrs, fromAmts := splitAddrAmount(txFrom)
	fromAddrs, fromAmts = aggregateAddrAmountLegs(fromAddrs, fromAmts)
	rawTx.TxFrom = joinAddrAmount(fromAddrs, fromAmts)
	rawTx.TxTo = txTo
	return nil
}

func appendOutput(output map[string]decimal.Decimal, address string, amount decimal.Decimal) map[string]decimal.Decimal {
	if cur, ok := output[address]; ok {
		output[address] = cur.Add(amount)
	} else {
		output[address] = amount
	}
	return output
}

func splitAddrAmount(items []string) ([]string, []string) {
	addrs := make([]string, 0, len(items))
	amts := make([]string, 0, len(items))
	for _, item := range items {
		addr, amt, ok := parseAddrAmountLine(item)
		if !ok {
			continue
		}
		addrs = append(addrs, addr)
		amts = append(amts, amt)
	}
	return addrs, amts
}

func parseAddrAmountLine(line string) (addr, amt string, ok bool) {
	line = strings.TrimSpace(line)
	idx := strings.LastIndex(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

// aggregateAddrAmountLegs collapses per-UTXO from legs into one row per wallet address.
func aggregateAddrAmountLegs(addrs, amts []string) ([]string, []string) {
	if len(addrs) == 0 {
		return addrs, amts
	}
	sums := make(map[string]decimal.Decimal, len(addrs))
	order := make([]string, 0, len(addrs))
	for i, addr := range addrs {
		if i >= len(amts) {
			break
		}
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		amount, err := util.ParseDecimalAmount(amts[i])
		if err != nil {
			continue
		}
		if _, seen := sums[addr]; !seen {
			order = append(order, addr)
		}
		sums[addr] = sums[addr].Add(amount)
	}
	outAddrs := make([]string, 0, len(order))
	outAmts := make([]string, 0, len(order))
	for _, addr := range order {
		outAddrs = append(outAddrs, addr)
		outAmts = append(outAmts, sums[addr].String())
	}
	return outAddrs, outAmts
}

func joinAddrAmount(addrs, amts []string) []string {
	lines := make([]string, 0, len(addrs))
	for i, addr := range addrs {
		if i >= len(amts) {
			break
		}
		lines = append(lines, fmt.Sprintf("%s:%s", addr, amts[i]))
	}
	return lines
}

func (d *BtcTransactionDecoder) txReplaceable(rawTx *types.RawTransaction) bool {
	if rawTx != nil && rawTx.SpeedUp != nil && rawTx.SpeedUp.Active() {
		return true
	}
	return d.Wm.Config.EnableRBF
}
