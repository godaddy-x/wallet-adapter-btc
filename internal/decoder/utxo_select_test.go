package decoder

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/shopspring/decimal"
)

func testFeeEstimator(_ int64, _ int64, rate decimal.Decimal) (decimal.Decimal, error) {
	// Fixed 1000 vbyte tx at given rate (BTC/KB).
	return rate, nil
}

func utxo(addr, amount string) *models.Unspent {
	return &models.Unspent{Address: addr, Amount: amount, Spendable: true}
}

func TestSelectUTXOsMultiInputAggregation(t *testing.T) {
	unspents := []*models.Unspent{
		utxo("addr1", "0.3"),
		utxo("addr2", "0.4"),
		utxo("addr3", "0.5"),
	}
	send := decimal.RequireFromString("0.65")
	rate := decimal.RequireFromString("0.00001")

	sel, err := selectUTXOsForPayment(unspents, send, rate, 10, 2, testFeeEstimator)
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.UsedUTXO) < 2 {
		t.Fatalf("expected multiple inputs, got %d", len(sel.UsedUTXO))
	}
	inputSum := decimal.Zero
	for _, u := range sel.UsedUTXO {
		ua, _ := decimal.NewFromString(u.Amount)
		inputSum = inputSum.Add(ua)
	}
	if inputSum.LessThan(send.Add(sel.Fees)) {
		t.Fatalf("inputs %s < send+fee %s", inputSum, send.Add(sel.Fees))
	}
}

func TestSelectUTXOsChangeOutput(t *testing.T) {
	unspents := []*models.Unspent{utxo("change-addr", "1.0")}
	send := decimal.RequireFromString("0.4")
	rate := decimal.RequireFromString("0.00001")

	sel, err := selectUTXOsForPayment(unspents, send, rate, 10, 2, testFeeEstimator)
	if err != nil {
		t.Fatal(err)
	}
	if !sel.ChangeAmount.GreaterThan(decimal.Zero) {
		t.Fatalf("expected change, got %s", sel.ChangeAmount)
	}
	if sel.ChangeAddr != "change-addr" {
		t.Fatalf("change addr = %q", sel.ChangeAddr)
	}
	wantChange := decimal.RequireFromString("1.0").Sub(send).Sub(rate)
	if !sel.ChangeAmount.Equal(wantChange) {
		t.Fatalf("change = %s want %s", sel.ChangeAmount, wantChange)
	}
}

func TestSelectUTXOsInsufficientBalance(t *testing.T) {
	unspents := []*models.Unspent{utxo("a", "0.01")}
	send := decimal.RequireFromString("1.0")
	rate := decimal.RequireFromString("0.00001")

	if _, err := selectUTXOsForPayment(unspents, send, rate, 10, 2, testFeeEstimator); err == nil {
		t.Fatal("expected insufficient balance error")
	}
}

func TestSelectUTXOsFeeIterationDoesNotLoopForever(t *testing.T) {
	// All spendable UTXOs selected but send+fee still exceeds balance.
	unspents := []*models.Unspent{utxo("a", "0.5")}
	send := decimal.RequireFromString("0.49")
	rate := decimal.RequireFromString("0.2")

	_, err := selectUTXOsForPayment(unspents, send, rate, 10, 2, testFeeEstimator)
	if err == nil {
		t.Fatal("expected insufficient balance for send+fees error")
	}
}
