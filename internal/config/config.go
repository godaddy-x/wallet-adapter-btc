// Package config provides BTC chain configuration loaded via wallet-adapter/config Configer.
package config

import (
	"path/filepath"
	"strings"

	adapterconfig "github.com/godaddy-x/wallet-adapter/config"
	"github.com/godaddy-x/wallet-adapter-btc/internal/chainparams"
	"github.com/shopspring/decimal"
)

const (
	RPCServerCore     = 0
	RPCServerExplorer = 1

	DefaultSymbol    = "BTC"
	DefaultMasterKey = "Bitcoin seed"
	DefaultCurveType = uint32(1) // secp256k1
	DefaultDecimals  = int32(8)

	NetworkMainnet = chainparams.NetworkMainnet
	NetworkTestnet = chainparams.NetworkTestnet
	NetworkRegtest = chainparams.NetworkRegtest

	AddressFormatP2WPKH = "p2wpkh" // Native SegWit bc1q / tb1q / bcrt1q (default for production)
	AddressFormatP2PKH  = "p2pkh"  // Legacy 1... / m... (only when explicitly required)
)

// WalletConfig holds BTC adapter configuration.
type WalletConfig struct {
	Symbol    string
	MasterKey string
	CurveType uint32
	Decimals  int32

	RpcUser      string
	RpcPassword  string
	ServerAPI    string
	BroadcastAPI string
	RpcWallet      string // Bitcoin Core wallet name for listunspent (e.g. btcwatch)
	RpcQueryWallet string // optional wallet for gettransaction fallback (e.g. ops_watch)
	DataDir      string

	Network       string // mainnet | testnet | regtest
	IsTestNet     bool
	IsRegtest     bool
	AddressFormat string // p2wpkh (default) | p2pkh
	SupportSegWit bool
	RPCServerType int
	MaxTxInputs   int
	MinFees       decimal.Decimal

	// Fee policy (native BTC competition model; estimatesmartfee on mainnet/testnet).
	FeeTargetBlocks   int             // default 2 blocks
	FeeEstimateMode   string          // economical | conservative | empty
	MinFeeRate        decimal.Decimal // BTC/KB floor; regtest fallback when node has no fee history
	FeeBumpMultiplier decimal.Decimal // default bump for RBF/CPFP (1.0 = none)
	EnableRBF         bool            // BIP125 nSequence=0xfffffffd on built txs

	// Dust change policy (see docs/BTC_DUST_CHANGE_TO_FEE.md).
	DustLimitSats         int64 // 0 = DefaultDustLimitSats (546); max MaxDustLimitSats
	OmitChangeBelowDust   bool  // merge sub-dust change into miner fee
}

// NewConfig creates default config for the given symbol.
func NewConfig(symbol string) *WalletConfig {
	if symbol == "" {
		symbol = DefaultSymbol
	}
	return &WalletConfig{
		Symbol:            symbol,
		MasterKey:         DefaultMasterKey,
		CurveType:         DefaultCurveType,
		Decimals:          DefaultDecimals,
		Network:           NetworkMainnet,
		IsTestNet:         false,
		IsRegtest:         false,
		AddressFormat:     AddressFormatP2WPKH,
		SupportSegWit:     true,
		RPCServerType:     RPCServerCore,
		MaxTxInputs:       18000,
		MinFees:           decimal.Zero,
		FeeTargetBlocks:   2,
		MinFeeRate:        decimal.RequireFromString("0.00001"),
		FeeBumpMultiplier: decimal.NewFromInt(1),
		EnableRBF:         true,
		OmitChangeBelowDust: true,
	}
}

// MakeDataDir sets data directory path (uses data/<symbol> when DataDir is empty).
func (c *WalletConfig) MakeDataDir() {
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	c.DataDir = filepath.Join(c.DataDir, strings.ToLower(c.Symbol))
}

// NetworkName returns normalized chain network (mainnet / testnet / regtest).
func (c *WalletConfig) NetworkName() string {
	switch strings.ToLower(strings.TrimSpace(c.Network)) {
	case NetworkTestnet, NetworkRegtest:
		return strings.ToLower(strings.TrimSpace(c.Network))
	case NetworkMainnet, "":
		if c.IsRegtest {
			return NetworkRegtest
		}
		if c.IsTestNet {
			return NetworkTestnet
		}
		return NetworkMainnet
	default:
		return NetworkMainnet
	}
}

// Bech32HRP returns BIP173 HRP for the configured network.
func (c *WalletConfig) Bech32HRP() string {
	return chainparams.Bech32HRP(c.NetworkName())
}

// UseLegacyP2PKHAddress reports whether PublicKeyToAddress should emit legacy P2PKH (1...).
func (c *WalletConfig) UseLegacyP2PKHAddress() bool {
	return strings.EqualFold(strings.TrimSpace(c.AddressFormat), AddressFormatP2PKH)
}

// BuildConfigFromConfiger reads config via Configer interface.
func BuildConfigFromConfiger(c adapterconfig.Configer, symbol string) *WalletConfig {
	cfg := NewConfig(symbol)
	cfg.ServerAPI = c.String("serverAPI")
	cfg.BroadcastAPI = c.String("broadcastAPI")
	if cfg.BroadcastAPI == "" {
		cfg.BroadcastAPI = cfg.ServerAPI
	}
	cfg.RpcUser = c.String("rpcUser")
	cfg.RpcPassword = c.String("rpcPassword")
	cfg.RpcWallet = strings.TrimSpace(c.String("rpcWallet"))
	cfg.RpcQueryWallet = strings.TrimSpace(c.String("rpcQueryWallet"))
	cfg.DataDir = c.String("dataDir")

	if n, err := c.Int64("rpcServerType"); err == nil {
		cfg.RPCServerType = int(n)
	}
	cfg.Network = strings.ToLower(strings.TrimSpace(c.String("network")))
	cfg.IsTestNet = parseBool(c.String("isTestNet"), false)
	cfg.IsRegtest = parseBool(c.String("isRegtest"), false)
	cfg.AddressFormat = strings.ToLower(strings.TrimSpace(c.String("addressFormat")))
	if cfg.AddressFormat == "" {
		cfg.AddressFormat = AddressFormatP2WPKH
	}
	if cfg.Network == "" {
		switch {
		case cfg.IsRegtest:
			cfg.Network = NetworkRegtest
		case cfg.IsTestNet:
			cfg.Network = NetworkTestnet
		default:
			cfg.Network = NetworkMainnet
		}
	}
	cfg.SupportSegWit = parseBool(c.String("supportSegWit"), true)

	if n, err := c.Int64("maxTxInputs"); err == nil && n > 0 {
		cfg.MaxTxInputs = int(n)
	}
	if v := c.String("minFees"); v != "" {
		if d, err := decimal.NewFromString(v); err == nil {
			cfg.MinFees = d
		}
	}
	if n, err := c.Int64("feeTargetBlocks"); err == nil && n > 0 {
		cfg.FeeTargetBlocks = int(n)
	}
	cfg.FeeEstimateMode = strings.ToLower(strings.TrimSpace(c.String("feeEstimateMode")))
	if v := c.String("minFeeRate"); v != "" {
		if d, err := decimal.NewFromString(v); err == nil {
			cfg.MinFeeRate = d
		}
	}
	if v := c.String("feeBumpMultiplier"); v != "" {
		if d, err := decimal.NewFromString(v); err == nil && d.GreaterThan(decimal.Zero) {
			cfg.FeeBumpMultiplier = d
		}
	}
	cfg.EnableRBF = parseBool(c.String("enableRBF"), true)
	cfg.OmitChangeBelowDust = parseBool(c.String("omitChangeBelowDust"), true)
	if n, err := c.Int64("dustLimitSats"); err == nil && n > 0 {
		cfg.DustLimitSats = n
	}
	if v := strings.TrimSpace(c.String("dustLimit")); v != "" {
		if d, err := decimal.NewFromString(v); err == nil && d.GreaterThan(decimal.Zero) {
			cfg.DustLimitSats = d.Shift(cfg.Decimals).IntPart()
		}
	}
	cfg.ValidateDustConfig()
	return cfg
}

func parseBool(s string, defaultVal bool) bool {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}
