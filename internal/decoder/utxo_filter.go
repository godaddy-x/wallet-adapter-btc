package decoder

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/godaddy-x/wallet-adapter-btc/internal/extparam"
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
	"github.com/godaddy-x/wallet-adapter/types"
)

func filterExcludedUnspents(unspents []*models.Unspent, rawTx *types.RawTransaction) []*models.Unspent {
	return filterExcludedUnspentsSet(unspents, parseExcludeVinKeys(rawTx))
}

func parseExcludeVinKeys(rawTx *types.RawTransaction) map[string]bool {
	if rawTx == nil || rawTx.ExtParam == nil {
		return nil
	}
	raw := strings.TrimSpace(rawTx.ExtParam[extparam.KeyExcludeOutpoints])
	if raw == "" {
		return nil
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil
	}
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k != "" {
			set[k] = true
		}
	}
	return set
}

func parseSummaryExcludeVinKeys(extParam string) map[string]bool {
	extParam = strings.TrimSpace(extParam)
	if extParam == "" {
		return nil
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(extParam), &payload); err != nil {
		return nil
	}
	raw := strings.TrimSpace(payload[extparam.KeyExcludeOutpoints])
	if raw == "" {
		return nil
	}
	var keys []string
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil
	}
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(strings.ToLower(k))
		if k != "" {
			set[k] = true
		}
	}
	return set
}

func filterExcludedUnspentsSet(unspents []*models.Unspent, exclude map[string]bool) []*models.Unspent {
	if len(exclude) == 0 {
		return unspents
	}
	out := make([]*models.Unspent, 0, len(unspents))
	for _, u := range unspents {
		if u == nil {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(u.TxID)) + ":" + strconv.FormatUint(u.Vout, 10)
		if exclude[key] {
			continue
		}
		out = append(out, u)
	}
	return out
}
