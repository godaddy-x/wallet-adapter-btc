# Regtest UTXO Testing Guide

Native BTC UTXO management is the foundation. On regtest, validate **main-coin transaction core capabilities** before any ecosystem assets (inscriptions, tokens, etc.).

## Priority test matrix

| Priority | Scenario | What to verify |
|----------|----------|----------------|
| P0 | Multi-input aggregation | Several small UTXOs combine to cover send + fee |
| P0 | Change output | Single large input → payment + change back to same wallet |
| P0 | Fee loop convergence | Input set recalculated when fee increases total spend |
| P1 | RBF (BIP125) | `enableRBF=true` → nSequence `0xfffffffd`; higher-fee replacement confirms |
| P1 | CPFP acceleration | Child spends unconfirmed change with elevated feerate |
| P2 | Summary sweep | `CreateSummaryRawTransactionWithError` batches inputs up to `maxTxInputs` |

Unit tests (no node): `internal/decoder/utxo_select_test.go`, `internal/manager/fee_test.go`.

Integration (regtest bitcoind): use `network=regtest` and fund addresses via `generatetoaddress`.

## Regtest setup

```bash
bitcoind -regtest -fallbackfee=0.0002
```

Mongo / adapter JSON:

```json
{
  "serverAPI": "http://127.0.0.1:18443",
  "rpcUser": "user",
  "rpcPassword": "password",
  "network": "regtest",
  "supportSegWit": "true",
  "enableRBF": "true",
  "minFeeRate": "0.00001",
  "feeTargetBlocks": "2"
}
```

Regtest has **no real fee market**. `estimatesmartfee` often returns errors until fee history exists; adapter falls back to `minFeeRate`.

## Fee policy (mainnet competition model)

All feerates are **BTC/KB** (same unit as Bitcoin Core `estimatesmartfee`).

| Config key | Default | Role |
|------------|---------|------|
| `feeTargetBlocks` | `2` | `estimatesmartfee` confirmation target |
| `feeEstimateMode` | empty | `economical` / `conservative` (Core v0.17+) |
| `minFeeRate` | `0.00001` | Floor; regtest fallback when node has no estimate |
| `feeBumpMultiplier` | `1` | Default multiplier for RBF resubmit / CPFP base bump |
| `enableRBF` | `true` | Set BIP125 replaceable sequence on built transactions |

### Dynamic adjustment APIs (`WalletManager`)

```go
// Current smart fee for configured target.
rate, err := wm.EstimateFeeRate()

// RBF: bump before rebuild (e.g. multiplier 1.25).
bumped, err := wm.EstimateFeeRateWithBump(decimal.RequireFromString("1.25"))

// CPFP: child feerate to accelerate underpaid parent in mempool.
childRate, err := wm.EstimateChildFeeRate(parentPaidFee, parentVsize, childVsize, targetBlocks)
```

Mainnet note: ecosystem-asset traffic still competes in the **same block space** as native BTC. Fee estimation must always go through the native competition model above, not asset-specific shortcuts.

## Suggested regtest flows

### 1. Multi-input + change

1. Fund three addresses with `0.3 / 0.4 / 0.5` BTC.
2. Send `0.65` BTC to external address.
3. Assert: ≥2 inputs selected, change output returned, `Fees` > 0.

### 2. RBF replacement

1. Build tx with `enableRBF=true`, broadcast with low `feeRate`.
2. Call `EstimateFeeRateWithBump(1.5)`, rebuild with same inputs (different nonce via new change or extra fee output).
3. Broadcast replacement; original tx evicted from mempool.

### 3. CPFP

1. Parent tx stuck with low fee; note its paid fee and vsize.
2. Build child spending unconfirmed change output.
3. Set child `feeRate` from `EstimateChildFeeRate(...)`; broadcast child.

## Out of scope (for now)

- Inscription / ordinals minting paths
- Asset-specific fee shortcuts bypassing `estimatesmartfee`

Keep regtest focus on UTXO + fee mechanics that mirror mainnet block-space competition.
