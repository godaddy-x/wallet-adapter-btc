package decoder

import (
	"fmt"
	"sort"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/shopspring/decimal"
)

type feeEstimator func(inputs, outputs int64, feeRate decimal.Decimal) (decimal.Decimal, error)

type utxoSelection struct {
	UsedUTXO     []*models.Unspent
	FeeRate      decimal.Decimal
	Fees         decimal.Decimal
	ChangeAmount decimal.Decimal
	ChangeAddr   string
}

// selectUTXOsForPayment picks inputs covering amount+fee with iterative fee refinement.
// outputSlots includes change output when applicable (typically len(destinations)+1).
func selectUTXOsForPayment(
	unspents []*models.Unspent,
	totalSend decimal.Decimal,
	feeRate decimal.Decimal,
	maxInputs int,
	outputSlots int,
	estimate feeEstimator,
) (*utxoSelection, error) {
	if len(unspents) == 0 {
		return nil, fmt.Errorf("utxo is empty")
	}
	if totalSend.LessThanOrEqual(decimal.Zero) {
		return nil, fmt.Errorf("send amount must be positive")
	}
	if outputSlots <= 0 {
		outputSlots = 1
	}

	sorted := append([]*models.Unspent(nil), unspents...)
	sort.Sort(models.UnspentSort{Values: sorted, Comparator: models.CompareUnspentAmountAsc})

	var usedUTXO []*models.Unspent
	actualFees := decimal.Zero
	computeTotalSend := totalSend
	spendableCount := countSpendableUTXOs(sorted)
	for {
		usedUTXO = make([]*models.Unspent, 0)
		balance := decimal.Zero
		for _, u := range sorted {
			if !u.Spendable {
				continue
			}
			ua, err := parseUTXOAmount(u)
			if err != nil {
				return nil, err
			}
			balance = balance.Add(ua)
			usedUTXO = append(usedUTXO, u)
			if balance.GreaterThanOrEqual(computeTotalSend) {
				break
			}
		}
		if balance.LessThan(computeTotalSend) {
			return nil, fmt.Errorf("balance %s is not enough", balance.String())
		}
		fees, err := estimate(int64(len(usedUTXO)), int64(outputSlots), feeRate)
		if err != nil {
			return nil, err
		}
		computeTotalSend = totalSend.Add(fees)
		if computeTotalSend.GreaterThan(balance) {
			if len(usedUTXO) >= spendableCount {
				return nil, fmt.Errorf("balance %s is not enough for send %s plus fees", balance.String(), totalSend.String())
			}
			continue
		}
		actualFees = fees
		break
	}
	if len(usedUTXO) > maxInputs {
		return nil, fmt.Errorf("transaction inputs exceed max: %d", maxInputs)
	}

	changeAmount := balanceSub(usedUTXO, totalSend, actualFees)
	return &utxoSelection{
		UsedUTXO:     usedUTXO,
		FeeRate:      feeRate,
		Fees:         actualFees,
		ChangeAmount: changeAmount,
		ChangeAddr:   usedUTXO[0].Address,
	}, nil
}

func balanceSub(usedUTXO []*models.Unspent, totalSend, fees decimal.Decimal) decimal.Decimal {
	balance := decimal.Zero
	for _, u := range usedUTXO {
		ua, err := parseUTXOAmount(u)
		if err != nil {
			continue
		}
		balance = balance.Add(ua)
	}
	return balance.Sub(totalSend).Sub(fees)
}

func parseUTXOAmount(u *models.Unspent) (decimal.Decimal, error) {
	if u == nil {
		return decimal.Zero, fmt.Errorf("utxo is nil")
	}
	amt, err := decimal.NewFromString(strings.TrimSpace(u.Amount))
	if err != nil {
		return decimal.Zero, fmt.Errorf("invalid utxo amount txid=%s vout=%d: %w", u.TxID, u.Vout, err)
	}
	if amt.IsNegative() {
		return decimal.Zero, fmt.Errorf("negative utxo amount txid=%s vout=%d", u.TxID, u.Vout)
	}
	return amt, nil
}

func countSpendableUTXOs(unspents []*models.Unspent) int {
	n := 0
	for _, u := range unspents {
		if u != nil && u.Spendable {
			n++
		}
	}
	return n
}
