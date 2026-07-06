package chainparams

import "github.com/btcsuite/btcd/chaincfg"

const (
	NetworkMainnet = "mainnet"
	NetworkTestnet = "testnet"
	NetworkRegtest = "regtest"
)

// Params returns chain parameters for mainnet / testnet / regtest.
func Params(network string) *chaincfg.Params {
	switch network {
	case NetworkRegtest:
		return &chaincfg.RegressionNetParams
	case NetworkTestnet:
		return &chaincfg.TestNet3Params
	default:
		return &chaincfg.MainNetParams
	}
}

// Bech32HRP returns BIP173 human-readable part for the network.
func Bech32HRP(network string) string {
	switch network {
	case NetworkRegtest:
		return "bcrt"
	case NetworkTestnet:
		return "tb"
	default:
		return "bc"
	}
}
