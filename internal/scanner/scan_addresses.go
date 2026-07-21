package scanner

import (
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

func forEachTxCandidateAddress(tx *models.Transaction, txIndex map[string]*models.Transaction, add func(string)) {
	if tx == nil || add == nil {
		return
	}
	for _, out := range tx.Vouts {
		if out == nil || out.Type == "OP_RETURN" {
			continue
		}
		if a := normalizeScanAddress(out.Addr); a != "" {
			add(a)
		}
	}
	for _, in := range tx.Vins {
		if in == nil || len(in.Coinbase) > 0 {
			continue
		}
		if a := vinCandidateAddress(in, txIndex); a != "" {
			add(a)
		}
	}
}

func candidateAddressSet(tx *models.Transaction, txIndex map[string]*models.Transaction) map[string]struct{} {
	set := make(map[string]struct{})
	forEachTxCandidateAddress(tx, txIndex, func(a string) {
		set[a] = struct{}{}
	})
	return set
}

func blockCandidateAddressSet(block *models.Block, txIndex map[string]*models.Transaction) map[string]struct{} {
	set := make(map[string]struct{}, 512)
	add := func(a string) {
		if a != "" {
			set[a] = struct{}{}
		}
	}
	if block != nil {
		for _, tx := range block.TxDetails {
			forEachTxCandidateAddress(tx, txIndex, add)
		}
	}
	if len(set) == 0 && txIndex != nil {
		for _, tx := range txIndex {
			forEachTxCandidateAddress(tx, txIndex, add)
		}
	}
	return set
}

func vinCandidateAddress(input *models.Vin, txIndex map[string]*models.Transaction) string {
	if input == nil {
		return ""
	}
	if a := normalizeScanAddress(input.Addr); a != "" {
		return a
	}
	if txIndex == nil {
		return ""
	}
	preTx := txIndex[input.TxID]
	if preTx == nil || int(input.Vout) >= len(preTx.Vouts) {
		return ""
	}
	return normalizeScanAddress(preTx.Vouts[input.Vout].Addr)
}

func vinNeedsRPCResolution(trx *models.Transaction, txIndex map[string]*models.Transaction) bool {
	if trx == nil {
		return false
	}
	for _, input := range trx.Vins {
		if input == nil || len(input.Coinbase) > 0 {
			continue
		}
		if vinCandidateAddress(input, txIndex) != "" {
			continue
		}
		return true
	}
	return false
}
