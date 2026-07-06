package decoder

import (
	"testing"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
)

func TestFilterExcludedUnspents(t *testing.T) {
	unspents := []*models.Unspent{
		{TxID: "aaa", Vout: 0, Amount: "1"},
		{TxID: "bbb", Vout: 1, Amount: "2"},
	}
	rawTx := &types.RawTransaction{
		ExtParam: map[string]string{
			extparam.KeyExcludeOutpoints: `["aaa:0"]`,
		},
	}
	out := filterExcludedUnspents(unspents, rawTx)
	if len(out) != 1 || out[0].TxID != "bbb" {
		t.Fatalf("unexpected filter result: %+v", out)
	}
}
