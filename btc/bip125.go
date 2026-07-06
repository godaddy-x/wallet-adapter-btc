package btc

import (
	"github.com/godaddy-x/wallet-adapter-btc/internal/models"
)

// IsBIP125Replaceable reports whether tx signals BIP125 replaceability via input nSequence.
func IsBIP125Replaceable(tx *models.Transaction) bool {
	return models.IsBIP125Replaceable(tx)
}
