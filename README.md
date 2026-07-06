# wallet-adapter-btc

**Module path**: `github.com/godaddy-x/wallet-adapter-btc`

Bitcoin [wallet-adapter](https://github.com/godaddy-x/wallet-adapter) subclass implementation. Provides UTXO-based transaction building, BIP143/SegWit signing metadata, block scanning, and raw transaction broadcast compatible with external MPC signing.

## Overview

- **Base framework**: [wallet-adapter](https://github.com/godaddy-x/wallet-adapter) `ChainAdapter`
- **Legacy reference**: [bitcoin-adapter](https://github.com/blocktree/bitcoin-adapter) (openwallet v2)
- **Structure reference**: [wallet-adapter-eth](https://github.com/godaddy-x/wallet-adapter-eth)

## Project structure

```
wallet-adapter-btc/
├── go.mod
├── main.go
├── btc/                    # public API
│   ├── adapter.go
│   ├── run.go              # NewAdapter(jsonContent, ...)
│   ├── signverify.go
│   └── doc.go
└── internal/
    ├── config/
    ├── decoder/            # AddressDecoder, TransactionDecoder
    ├── manager/            # RPC, UTXO, fee, broadcast
    ├── models/             # Block, Transaction, Unspent
    ├── rpc/                # Core JSON-RPC + Insight REST
    ├── scanner/            # BlockScanner
    └── util/
```

## Configuration (JSON via LoadAssetsConfig)

| Key | Description |
|-----|-------------|
| `serverAPI` | Bitcoin Core RPC or Insight-API base URL |
| `broadcastAPI` | Optional broadcast URL (defaults to serverAPI) |
| `rpcUser` / `rpcPassword` | Core RPC basic auth |
| `rpcServerType` | `0` = Core RPC, `1` = Insight-API |
| `network` | `mainnet` (default) / `testnet` / `regtest` — also accepts `isTestNet` / `isRegtest` |
| `addressFormat` | `p2wpkh` (default, Native SegWit `bc1q`) / `p2pkh` (legacy `1...`, only when explicitly required) |
| `isTestNet` | Shorthand for `network=testnet` when `network` is unset |
| `isRegtest` | Shorthand for `network=regtest` when `network` is unset |
| `supportSegWit` | Enable SegWit tx build/verify |
| `minFees` | Minimum fee in BTC |
| `maxTxInputs` | Max UTXO inputs per tx (default 18000) |
| `feeTargetBlocks` | `estimatesmartfee` target confirmations (default `2`) |
| `feeEstimateMode` | `economical` / `conservative` (optional, Core RPC) |
| `minFeeRate` | Minimum feerate BTC/KB; regtest fallback when node has no estimate |
| `feeBumpMultiplier` | Default multiplier for RBF/CPFP fee bumps (default `1`) |
| `enableRBF` | BIP125 replaceable transactions (default `true`) |
| `dataDir` | Local data directory |

### Address formats (mainnet vs testnet vs regtest)

Underlying witness program and hash160 are identical; only the **human-readable prefix (HRP / version byte)** differs:

| Environment | Native SegWit (default) | Legacy P2PKH (`addressFormat=p2pkh`) |
|-------------|-------------------------|----------------------------------------|
| Mainnet     | `bc1q...`               | `1...`                                 |
| Testnet     | `tb1q...`               | `m...` / `n...`                        |
| Regtest     | `bcrt1q...`             | `m...` / `n...`                        |

Regtest `bcrt1q` maps to mainnet `bc1q` with the same witness logic — no separate code path. Use `network=regtest` (or `isRegtest=true`) for local bitcoind `-regtest`. Production deployments should keep the default `addressFormat=p2wpkh` unless a project explicitly needs legacy addresses.

Example:

```json
{
  "serverAPI": "http://127.0.0.1:8332",
  "rpcUser": "user",
  "rpcPassword": "password",
  "rpcServerType": "0",
  "isTestNet": "false",
  "supportSegWit": "true",
  "minFees": "0.00001",
  "dataDir": "data"
}
```

## Usage

```go
import (
    "github.com/godaddy-x/wallet-adapter/chain"
    "github.com/godaddy-x/wallet-adapter/flow"
    "github.com/godaddy-x/wallet-adapter-btc/btc"
)

func init() {
    btc.RegisterSignVerify()
}

adapter, err := btc.NewAdapter(jsonContent, "BTC", "Bitcoin", 8)
if err != nil {
    return err
}

decoder, _ := chain.GetTransactionDecoder("BTC")
pending, err := flow.BuildTransaction(decoder, wrapper, rawTx)
tx, err := flow.SendTransaction(decoder, wrapper, pending)
```

## Capabilities

| Capability | Implementation |
|------------|----------------|
| Native BTC transfer | `BtcTransactionDecoder.CreateRawTransaction` |
| UTXO coin selection | smallest-first + fee loop |
| SegWit build/verify | `supportSegWit` config |
| Fee estimation | `estimatesmartfee` via `EstimateFeeRate` / `GetRawTransactionFeeRate` |
| Dynamic fee bump (RBF) | `EstimateFeeRateWithBump` |
| CPFP child feerate | `EstimateChildFeeRate` |
| Summary sweep | `CreateSummaryRawTransactionWithError` |
| Address codec | Native SegWit P2WPKH by default (`bc1q` / `tb1q` / `bcrt1q`); legacy P2PKH via `addressFormat=p2pkh` |
| Block scanning | `BtcBlockScanner` (`ScanBlockWithResult`, `RunScanLoop`, `VerifyTransactionByTxID`) |
| Dual RPC backend | Core JSON-RPC or Insight-API |
| BIP125 RBF | `enableRBF` → replaceable nSequence on built txs |
| Precompute submit txID | `PrecomputeSubmitTxID` — local txid before broadcast (aligned with ETH flow) |

Regtest UTXO test priorities (multi-input, change, RBF, CPFP): [docs/REGTEST_UTXO_TESTING.md](docs/REGTEST_UTXO_TESTING.md).

ETH baseline alignment (native BTC core scope): [docs/CORE_CAPABILITIES.md](docs/CORE_CAPABILITIES.md).

### Block scan security

- **Address/script cross-check**: `ParseVoutFromCore` clears mismatched addresses; extract step re-validates when script is present
- **Positive amounts only**: zero/invalid vout/vin amounts are skipped
- **Per-vout OutputIndex**: one `ExtractDataItem` per matched vin/vout (avoids UniqueHash collision)
- **Failed tx extraction blocks cursor**: `Success=false` when `FailedTxIDs` is non-empty

## Dependencies

- Go 1.26+
- `github.com/godaddy-x/wallet-adapter`
- `github.com/btcsuite/btcd` (address encoding, transaction build/sign/verify)
- `github.com/imroc/req`, `github.com/tidwall/gjson`, `github.com/shopspring/decimal`

Local development:

```go
//replace github.com/godaddy-x/wallet-adapter => ../wallet-adapter
```

## Tests

Unit tests (no node required):

```bash
go test ./btc/... ./internal/...
```

Integration examples in `main_test.go` connect to a local Bitcoin Core RPC (`testConfigJSON` or env `WALLET_ADAPTER_BTC_CONFIG_JSON`). Tests skip automatically when the node is unreachable:

```bash
go test . -run TestStartBlockScanner -v
```
