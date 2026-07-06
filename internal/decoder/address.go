package decoder

import (
	"fmt"

	"github.com/godaddy-x/wallet-adapter-btc/internal/btcaddr"
	"github.com/godaddy-x/wallet-adapter-btc/internal/manager"
	"github.com/godaddy-x/wallet-adapter/decoder"
	"github.com/godaddy-x/wallet-adapter/types"
)

const (
	SignExtSchemeBIP143     = "bip143"
	SignExtEncodingHex      = "hex"
	SignExtHashDoubleSHA256 = "double_sha256"
)

// BtcAddressDecoder implements decoder.AddressDecoder for BTC.
type BtcAddressDecoder struct {
	decoder.AddressDecoderBase
	wm *manager.WalletManager
}

// NewAddressDecoder creates address decoder bound to wallet manager.
func NewAddressDecoder(wm *manager.WalletManager) *BtcAddressDecoder {
	return &BtcAddressDecoder{wm: wm}
}

func (d *BtcAddressDecoder) configuredNetwork() string {
	if d.wm != nil && d.wm.Config != nil {
		if n := d.wm.Config.NetworkName(); n != "" {
			return n
		}
	}
	return "mainnet"
}

// PrivateKeyToWIF converts private key to WIF.
func (d *BtcAddressDecoder) PrivateKeyToWIF(priv []byte, _ bool) (string, error) {
	return "", fmt.Errorf("PrivateKeyToWIF not implemented for MPC flow")
}

// PublicKeyToAddress converts public key to on-chain address.
// Network is taken from wallet config (mainnet / testnet / regtest), not the isTestnet argument.
// Default: Native SegWit P2WPKH (bc1q / tb1q / bcrt1q). Set addressFormat=p2pkh for legacy 1.../m....
func (d *BtcAddressDecoder) PublicKeyToAddress(pub []byte, _ bool) (string, error) {
	network := d.configuredNetwork()
	legacy := d.wm != nil && d.wm.Config != nil && d.wm.Config.UseLegacyP2PKHAddress()
	return btcaddr.PubKeyToAddress(pub, network, legacy)
}

// RedeemScriptToAddress converts redeem script to P2SH address.
func (d *BtcAddressDecoder) RedeemScriptToAddress(pubs [][]byte, required uint64, _ bool) (string, error) {
	return "", fmt.Errorf("RedeemScriptToAddress not implemented")
}

// WIFToPrivateKey converts WIF to private key bytes.
func (d *BtcAddressDecoder) WIFToPrivateKey(wif string, _ bool) ([]byte, error) {
	return nil, fmt.Errorf("WIFToPrivateKey not implemented for MPC flow")
}

// AddressDecode decodes address to hash160 bytes (P2WPKH / P2PKH / P2SH).
func (d *BtcAddressDecoder) AddressDecode(addr string, _ ...interface{}) ([]byte, error) {
	network := d.configuredNetwork()
	return btcaddr.DecodeAddressHash160(addr, network)
}

// AddressEncode encodes hash160 bytes using default address format for the configured network.
func (d *BtcAddressDecoder) AddressEncode(hash []byte, _ ...interface{}) (string, error) {
	network := d.configuredNetwork()
	if d.wm != nil && d.wm.Config != nil && d.wm.Config.UseLegacyP2PKHAddress() {
		return btcaddr.EncodePubKeyHashAddress(hash, network)
	}
	return btcaddr.EncodeWitnessV0Address(hash, network)
}

// AddressVerify checks whether address format is valid for the configured network.
func (d *BtcAddressDecoder) AddressVerify(addr string, _ ...interface{}) bool {
	network := d.configuredNetwork()
	return btcaddr.VerifyAddress(addr, network)
}

// CustomCreateAddress is not supported for MPC flow.
func (d *BtcAddressDecoder) CustomCreateAddress(account *types.AssetsAccount, newIndex uint64) (*types.Address, error) {
	return nil, fmt.Errorf("CustomCreateAddress not implement")
}

// SupportCustomCreateAddressFunction reports false.
func (d *BtcAddressDecoder) SupportCustomCreateAddressFunction() bool {
	return false
}
