package decoder

import (
	"strconv"

	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter/types"
)

func fillBTCSignExt(rawSignExt *string, wm *manager.WalletManager) error {
	if wm == nil || wm.Config == nil {
		return types.Errorf(types.ErrCreateRawTransactionFailed, "wallet config is nil")
	}
	network := wm.Config.NetworkName()
	segwit := "false"
	if wm.Config.SupportSegWit {
		segwit = "true"
	}
	rbf := "false"
	if wm.Config.EnableRBF {
		rbf = "true"
	}
	signExt, err := types.BuildSignExtJSON(map[string]string{
		types.SignExtKeySignScheme:       SignExtSchemeBIP143,
		types.SignExtKeyUnsignedEncoding: SignExtEncodingHex,
		types.SignExtKeyCurveType:        strconv.FormatUint(uint64(wm.Config.CurveType), 10),
		types.SignExtKeyHashAlgorithm:    SignExtHashDoubleSHA256,
		"network":                        network,
		"segwit":                         segwit,
		"rbf":                            rbf,
	})
	if err != nil {
		return types.ConvertError(err)
	}
	*rawSignExt = signExt
	return nil
}
