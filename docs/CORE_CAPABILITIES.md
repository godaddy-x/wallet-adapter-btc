# BTC Core Capabilities (ETH Baseline Alignment)

Scope: **native BTC only**. Inscriptions / ordinals / ecosystem assets are out of scope until core UTXO flows are stable.

## Capability matrix

| ETH baseline | BTC implementation | Status |
|--------------|-------------------|--------|
| Address encode/decode | Native SegWit P2WPKH default (`bc1q`/`tb1q`/`bcrt1q`); legacy P2PKH optional | ✅ |
| Block scan → extract by address | `extractTransaction` + script cross-check + positive amounts | ✅ |
| Create raw tx | `CreateRawTransaction` (UTXO select + change) | ✅ |
| MPC sign + verify | BIP143 SegWit sighash in `VerifyRawTransaction` | ✅ |
| Precompute txID before broadcast | `PrecomputeSubmitTxID` | ✅ |
| Submit / broadcast | `SubmitRawTransaction` + local txid verify | ✅ |
| Fee estimate | `estimatesmartfee` + `minFeeRate` floor + bump APIs | ✅ |
| SpeedUp (replace pending) | BIP125 RBF via `rawTx.SpeedUp` (same inputs, higher fee) | ✅ adapter |
| Cancel (drop pending intent) | RBF self-pay: `To[from]=0` + `SpeedUp` | ✅ adapter |
| Summary sweep | `CreateSummaryRawTransactionWithError` | ✅ |
| Smart contract / batch / tokens | N/A for BTC core | ⏸ deferred |

## End-to-end closed loop (production path)

```text
PublicKeyToAddress → CreateRawTransaction → MPC sign → VerifyRawTransaction
  → PrecomputeSubmitTxID → SubmitRawTransaction → block scan extract → confirm credit
```

## SpeedUp / Cancel (aligned with ETH `types.SpeedUp`)

ETH uses **same nonce + higher gas**. BTC uses **same inputs + BIP125 RBF + higher feerate (BTC/KB)**.

| Field | ETH | BTC |
|-------|-----|-----|
| `speedUp.nonce` | on-chain nonce | **origin txid** (tx to replace) |
| `speedUp.fromAddress` | payer address | change / cancel refund address |
| `speedUp.feeRate` | gas price | feerate BTC/KB |
| `speedUp.feeBumpPercent` | +N% gas | +N% feerate |
| `speedUp.feeBumpWei` | +wei | +BTC/KB increment |

**SpeedUp:** scanner restores original `to` from DB snapshot; adapter rebuilds with origin inputs, same outputs, reduced change / higher fee.

**Cancel:** scanner sets `to[from]=0`; adapter builds single output returning `inputs − fee` to `fromAddress` (ETH 0-value self-send equivalent).

**Prerequisites (BTC-specific):**

| Rule | ETH | BTC |
|------|-----|-----|
| Must `SubmitTrade` before SpeedUp/Cancel | optional (same nonce offline) | **required** — origin tx must be in mempool |
| Chained SpeedUp (same `originSid`) | same nonce anchor | supported — scanner walks replacement chain (origin + speed-up/cancel children) and picks the **newest unconfirmed** submit txid |
| `enableRBF` on scanner node | N/A | **required** (default `true`) — origin tx must be BIP125 replaceable |

Requires `enableRBF=true` (default). Replace target must be **unconfirmed** (`confirmations=0`).

## Scanner / stdrpc integration

| Layer | Status |
|-------|--------|
| `wallet-adapter-btc` SpeedUp/Cancel build | ✅ |
| `open_scanner_btc` `SpeedUpFromOriginTrade` / `CancelFromOriginTrade` | ✅ (DB snapshot → `createRawTransaction(..., speedUp)`, replaceable txid from submit chain) |
| `open_gateway` SpeedUp/Cancel APIs | ✅ (EVM today; calls scanner RPC) |

## Explicitly deferred

- Inscriptions / ordinals / BRC-20
- Omni / token layers
- Batch contract (dataType=4) — BTC uses native multi-output or summary sweep instead

## Related docs

- [REGTEST_UTXO_TESTING.md](REGTEST_UTXO_TESTING.md) — regtest test priorities
- [../open_gateway/docs/TRANSFER_SPEEDUP_FLOW.md](../../../work/coding/open_gateway/docs/TRANSFER_SPEEDUP_FLOW.md) — platform SpeedUp/Cancel API (EVM-oriented; BTC maps to RBF)
