# BTC Core Capabilities (ETH Baseline Alignment)

Scope: **native BTC only**. Inscriptions / ordinals / ecosystem assets are out of scope until core UTXO flows are stable.

## 唯一设计文档

**产品规则、扫块 extract、粉尘找零、RBF、outputIndex、会计形状** 均在 open_gateway 单文档：

**[BTC_SCANNER_FLOW.md](https://github.com/godaddy-x/open_gateway/blob/main/docs/BTC_SCANNER_FLOW.md)**

| 本文档章节（旧） | 合并后位置 |
|------------------|------------|
| Capability matrix / 出站闭环 | BTC_SCANNER_FLOW §5 |
| SpeedUp / Cancel | BTC_SCANNER_FLOW §7 |
| Block scan extract | BTC_SCANNER_FLOW §8–§9 |
| Dust change → fee | BTC_SCANNER_FLOW §6 |

本文件仅保留 **能力矩阵速查**；实现细节以 §9 与代码为准。

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

## Related docs

- [BTC_SCANNER_FLOW.md](https://github.com/godaddy-x/open_gateway/blob/main/docs/BTC_SCANNER_FLOW.md) — **唯一权威**
- [REGTEST_UTXO_TESTING.md](REGTEST_UTXO_TESTING.md) — regtest test priorities
- [internal/scanner/BTC_SCANNER_EXTRACT_FLOW.md](../internal/scanner/BTC_SCANNER_EXTRACT_FLOW.md) — 代码索引
