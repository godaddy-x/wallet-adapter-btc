package scanner

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
)
func TestPeerOutFeeAccountingTable(t *testing.T) {
	const dec = int32(8)

	type wantLeg struct {
		sendOut int64
		fee     int64
		netOut  int64
	}

	tests := []struct {
		name   string
		trx    *models.Transaction
		legs   []types.TradeOrderPayerLeg
		sendOut map[string]string
		want   map[string]wantLeg // nil → illegal peerIn, accounting must fail
	}{
		{
			name: "summary_two_payer_peerOut_zero",
			trx: &models.Transaction{
				TxID: "summary-ab",
				Vins: []*models.Vin{
					{Addr: "bcrt1qa", Value: "0.6"},
					{Addr: "bcrt1qb", Value: "0.4"},
				},
				Vouts: []*models.Vout{
					{Addr: "bcrt1qext", Value: "0.9"},
					{Addr: "bcrt1qa", Value: "0.09989"},
				},
			},
			legs: []types.TradeOrderPayerLeg{
				{PayerAddress: "bcrt1qa", SendOut: "0.50004889"},
				{PayerAddress: "bcrt1qb", SendOut: "0.39995111"},
			},
			sendOut: map[string]string{
				"bcrt1qa": "0.50004889",
				"bcrt1qb": "0.39995111",
			},
			want: map[string]wantLeg{
				"bcrt1qa": {sendOut: 50_004_889, fee: 6111, netOut: 50_011_000},
				"bcrt1qb": {sendOut: 39_995_111, fee: 4889, netOut: 40_000_000},
			},
		},
		{
			name: "dust_main_peerIn_to_sibling_rejected",
			trx: &models.Transaction{
				TxID: "0985c7161bace08767a00fc8791ddca7ee618196e01a994416c2874573e346c8",
				Vins: []*models.Vin{
					{Addr: "bcrt1qdust", Value: "0.003"},
					{Addr: "bcrt1qmain", Value: "49.96694326"},
				},
				Vouts: []*models.Vout{
					{Addr: "bcrt1qext", Value: "0.003"},
					{Addr: "bcrt1qdust", Value: "49.96693326"},
				},
			},
			legs: []types.TradeOrderPayerLeg{
				{PayerAddress: "bcrt1qdust", SendOut: "0.0000369", TxFromAmount: "0.0000369"},
				{PayerAddress: "bcrt1qmain", SendOut: "0.0029631", TxFromAmount: "49.96694326"},
			},
			sendOut: map[string]string{
				"bcrt1qdust": "0.0000369",
				"bcrt1qmain": "0.0029631",
			},
			want: nil,
		},
		{
			name: "speedup_fund_peerIn_rejected",
			trx: &models.Transaction{
				TxID: "19ed07e8e5eea55f44e0b762228089909b3af4847ec48576f1524a58e463ba88",
				Vins: []*models.Vin{
					{Addr: "bcrt1qxhmnzg24z83gw3eq6u3ddtll86alw43f0h2egx", Value: "0.0000369"},
					{Addr: "bcrt1q7hfsp0ja6lwmz85djtgh03w7nch2d4nujp4juq", Value: "24.96694326"},
				},
				Vouts: []*models.Vout{
					{Addr: "bcrt1qwr2p87r8edjydqcmnncaulaetgg65y0hm3rxq0", Value: "0.003"},
					{Addr: "bcrt1qxhmnzg24z83gw3eq6u3ddtll86alw43f0h2egx", Value: "24.96397016"},
				},
			},
			legs: []types.TradeOrderPayerLeg{
				{PayerAddress: "bcrt1qxhmnzg24z83gw3eq6u3ddtll86alw43f0h2egx", SendOut: "0.0000369", TxFromAmount: "0.0000369"},
				{PayerAddress: "bcrt1q7hfsp0ja6lwmz85djtgh03w7nch2d4nujp4juq", SendOut: "0.0029631", TxFromAmount: "24.96694326"},
			},
			sendOut: map[string]string{
				"bcrt1qxhmnzg24z83gw3eq6u3ddtll86alw43f0h2egx": "0.0000369",
				"bcrt1q7hfsp0ja6lwmz85djtgh03w7nch2d4nujp4juq": "0.0029631",
			},
			want: nil,
		},
		{
			name: "fee_only_with_sibling_peerIn_rejected",
			trx: &models.Transaction{
				Vins: []*models.Vin{
					{Addr: "bcrt1qdust", Value: "0.003"},
					{Addr: "bcrt1qmain", Value: "49.96694326"},
				},
				Vouts: []*models.Vout{
					{Addr: "bcrt1qext", Value: "0.003"},
					{Addr: "bcrt1qdust", Value: "49.96693326"},
				},
			},
			legs: []types.TradeOrderPayerLeg{
				{PayerAddress: "bcrt1qdust", SendOut: "0.003", TxFromAmount: "0.003"},
				{PayerAddress: "bcrt1qmain", SendOut: "0", TxFromAmount: "49.96694326"},
			},
			sendOut: map[string]string{
				"bcrt1qdust": "0.003",
				"bcrt1qmain": "0",
			},
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := minimalExtractItems(tc.trx, tc.sendOut)
			acctCtx := &btcFeeAccountingContext{
				lookup: &stubTradeOrderLookup{snap: &types.TradeOrderOutboundSnapshot{
					Found: true,
					Legs:  tc.legs,
				}},
				symbol:    "BTC",
				accountID: "account-1",
			}
			got := computePayerLegAccounting(tc.trx, items, dec, acctCtx)
			if tc.want == nil {
				if len(got) != 0 {
					t.Fatalf("expected illegal peerIn fail-stop, got %d legs", len(got))
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("legs=%d want %d", len(got), len(tc.want))
			}
			byPayer := map[string]btcPayerLeg{}
			for _, leg := range got {
				byPayer[leg.payerAddr] = leg
			}
			var feeSum, sendSum int64
			for payer, w := range tc.want {
				leg, ok := byPayer[normalizeScanAddress(payer)]
				if !ok {
					t.Fatalf("missing leg %s", payer)
				}
				if leg.sendOutSats != w.sendOut {
					t.Fatalf("%s sendOut=%d want %d", payer, leg.sendOutSats, w.sendOut)
				}
				if leg.feeSats != w.fee {
					t.Fatalf("%s fee=%d want %d", payer, leg.feeSats, w.fee)
				}
				if leg.netOutSats != w.netOut {
					t.Fatalf("%s netOut=%d want %d", payer, leg.netOutSats, w.netOut)
				}
				if leg.sendOutSats+leg.feeSats != leg.netOutSats {
					t.Fatalf("%s sendOut+fee != netOut", payer)
				}
				feeSum += leg.feeSats
				sendSum += leg.sendOutSats
			}
			chainFee, ok := computeBTCTransactionFee(tc.trx)
			if !ok {
				t.Fatal("chain fee")
			}
			chainFeeSats, ok := decimalToSats(chainFee, dec)
			if !ok {
				t.Fatal("chain fee sats")
			}
			if feeSum != chainFeeSats {
				t.Fatalf("fee sum=%d want chain %d", feeSum, chainFeeSats)
			}
			external := chainExternalOutSatsExcludingPayers(tc.trx, map[string]int64{
				normalizeScanAddress("bcrt1qa"): 0,
				normalizeScanAddress("bcrt1qb"): 0,
				normalizeScanAddress("bcrt1qdust"): 0,
				normalizeScanAddress("bcrt1qmain"): 0,
			}, dec)
			// rebuild payer set from want keys
			payerSet := make(map[string]int64, len(tc.want))
			for payer := range tc.want {
				payerSet[normalizeScanAddress(payer)] = 0
			}
			external = chainExternalOutSatsExcludingPayers(tc.trx, payerSet, dec)
			if sendSum != external {
				t.Fatalf("send sum=%d want external %d", sendSum, external)
			}
		})
	}
}

// TestAppendBTCTransactionFeeItemsRejectsIllegalPeerIn fails when a managed payer receives
// more than self-change on the same tx (change must stay on the payer; sibling transfer is a separate tx).
func TestAppendBTCTransactionFeeItemsRejectsIllegalPeerIn(t *testing.T) {
	dust := "bcrt1qg5xunuluw78tv3mh6vvhmpcrpe2gx3ygawzpcu"
	main := "bcrt1q3lvzr9xgzytgsxudm455pav3jju0mmth5lnxxm"
	peer := "bcrt1qja3mzjkarkvw0z3xaa7wwk8k2z84amx3fzcl67"
	trx := &models.Transaction{
		TxID:          "b7d9dd65246d04af232fbe786bcfefe04f9dac02a4237ca294e0677b5928edb4",
		BlockHash:     "hash",
		BlockHeight:   246,
		Confirmations: 1,
		Vins: []*models.Vin{
			{N: 0, Addr: dust, Value: "0.0000369"},
			{N: 1, Addr: main, Value: "49.96694326"},
		},
		Vouts: []*models.Vout{
			{N: 0, Addr: peer, Value: "0.003"},
			{N: 1, Addr: dust, Value: "49.96397016"},
		},
	}
	items := []*types.ExtractDataItem{
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{dust}, FromAmt: []string{"0.0000369"},
			ToAddr: []string{dust}, ToAmt: []string{"49.96397016"},
			TxAction: "internal", OutputIndex: btcAddressNetOutputIndex, Decimal: 8, Fees: btcNonFeeLegFees,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, FromAddr: []string{main}, FromAmt: []string{"49.96694326"},
			TxAction: "internal", OutputIndex: 1, Decimal: 8, Fees: btcNonFeeLegFees,
		}}}},
		{SourceKey: "account-1", Data: []*types.TxExtractData{{Transaction: &types.Transaction{
			TxID: trx.TxID, ToAddr: []string{peer}, ToAmt: []string{"0.003"}, Amount: "0.003",
			TxAction: "internal", OutputIndex: 0, Decimal: 8, Fees: btcNonFeeLegFees,
		}}}},
	}
	acctCtx := &btcFeeAccountingContext{
		lookup: &stubTradeOrderLookup{snap: &types.TradeOrderOutboundSnapshot{
			Found: true,
			Legs: []types.TradeOrderPayerLeg{
				{PayerAddress: dust, SendOut: "0.0000369", TxFromAmount: "0.0000369"},
				{PayerAddress: main, SendOut: "0.0029631", TxFromAmount: "49.96694326"},
			},
		}},
		symbol:    "BTC",
		accountID: "account-1",
	}
	err := appendBTCTransactionFeeItems(trx, &items, types.Coin{Symbol: "BTC"}, 0, 8, acctCtx)
	if err == nil {
		t.Fatal("expected business payer accounting failed for illegal peerIn vout to sibling")
	}
}

func minimalExtractItems(trx *models.Transaction, sendOut map[string]string) []*types.ExtractDataItem {
	items := make([]*types.ExtractDataItem, 0, len(sendOut))
	for payer, amt := range sendOut {
		payer = normalizeScanAddress(payer)
		var vinAmt string
		for _, in := range trx.Vins {
			if normalizeScanAddress(in.Addr) == payer {
				vinAmt = in.Value
				break
			}
		}
		tx := &types.Transaction{
			TxID:     trx.TxID,
			TxAction: "send",
			FromAddr: []string{payer},
			FromAmt:  []string{vinAmt},
			ToAddr:   []string{"bcrt1qext"},
			ToAmt:    []string{amt},
		}
		items = append(items, &types.ExtractDataItem{
			SourceKey: "account-1",
			Data:      []*types.TxExtractData{{Transaction: tx}},
		})
	}
	return items
}
