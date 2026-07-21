# BTC 扫块适配器流程（目标模型）

权威产品与验收：`open_gateway/docs/BTC_SCANNER_FLOW.md`。  
**当前技术实现（扫块链路、RPC、Mongo、性能）**：`open_gateway/docs/BTC_BLOCK_SCAN_TECH_FLOW.md`。  
付款方会计（建单写应付 + 扫块恒等式）：[BTC_BUSINESS_SOURCE_PAYER_ACCOUNTING.md](BTC_BUSINESS_SOURCE_PAYER_ACCOUNTING.md)。

| 组件 | 路径 |
|------|------|
| 适配器扫块 | `wallet-adapter-btc/internal/scanner/` |
| 建单写应付 | `wallet-adapter-btc/internal/decoder/payer_send_out.go` |
| 进程入口 | `open_scanner_btc/main.go` |
| 链上入账 | `open_scanner/scanner/` |

> **2026-07-19：** 产出形状固定为充值 1 条 / 转出 send+fee / 汇总同构。  
> **废弃：** peerOut 挂 To、对 chainFee 比例/边际分摊、filterReceive 多层去重作为正确性来源。

---

## 1. 数据流

```text
建单 buildRawTransaction
  → writePayerSendOutExtParam（写入应付 payerSendOut）
  → 广播 ow_trade

bitcoind 出块
  → RunScanLoop / ScanBlockWithResult
  → extractTransaction（按托管地址 vin/vout）
  → appendBTCTransactionFeeItems
        GetTradeOrderOutbound → 读 sendOut_i（只读）
        链上：vin_i、change_i（自找零）
        fee_i = (vin_i − change_i) − sendOut_i
        校验 Σ sendOut / Σ fee
  → []ExtractDataItem
  → open_scanner HandleBlock
        newly → promote → balance
  → FinalAudit：DB == 链上 UTXO
```

---

## 2. 目标提取形状

| 场景 | Extract 行 |
|------|------------|
| 充值 | 地址 `receive` × 1 |
| 单地址转出 | 地址 `send` + `fee` |
| 汇总 | 每付款地址 `send` + `fee`；目标地址 `receive` × 1 |
| 粉尘 | 无找零 vout 时 fee 含残留；仍两行 |
| fee-only | `sendOut="0"` → 通常仅 `fee` |

付款方 ToAddr **只含外部收款**。收款方只靠自己的 `receive` 加余额。

---

## 3. 入口函数

| 函数 | 文件 | 说明 |
|------|------|------|
| `writePayerSendOutExtParam` | `decoder/payer_send_out.go` | 建单写应付 |
| `RunScanLoop` | `blockscanner.go` | 持续扫块 |
| `ScanBlockWithResult` | `blockscanner.go` | 单块 |
| `extractTransaction` | `blockscanner.go` | 按地址 leg |
| `appendBTCTransactionFeeItems` | `fee_extract.go` | 读应付 / 算 fee |
| `inferBTCTxAction` | `tx_action.go` | receive / send / internal |

---

## 4. 会计（自找零）

```text
扫块侧（禁止猜 sendOut、禁止分摊 chainFee）：
  sendOut_i ← ow_trade ExtParam.payerSendOut
  vin_i / change_i ← 链
  fee_i = (vin_i − change_i) − sendOut_i
  Σ sendOut = external，Σ fee = chainFee（±1 sat）
  失败 → business payer accounting failed，块高不推进
```

建单如何写应付、dust+main / 多出资方规则：见 [BTC_BUSINESS_SOURCE_PAYER_ACCOUNTING.md](BTC_BUSINESS_SOURCE_PAYER_ACCOUNTING.md) §5。  
粉尘策略：`open_gateway/docs/BTC_DUST_CHANGE_TO_FEE.md`。

---

## 5. open_scanner 侧

```text
saveUnConfirmedTx → ow_trade_newly
retryPromoteConfirmableNewly
  send  → −sendOut
  fee   → −fee
  receive → +amount
全部可确认 newly 清空 → 推进 curHeight
```

查单：`trade_order_outbound.go` → `GetTradeOrderOutbound`。  
不依赖：`attachPeerOutTargets`、多层 covered-receive skip 作为主路径。

---

## 6. 与旧文档差异

| 旧步骤 | 状态 |
|--------|------|
| `attachPeerOutTargets` 入账 | 废弃 |
| chainFee 比例 / 边际 vsize 分摊 | 废弃；fee 仅恒等式 |
| `filterPayerSelfChangeReceive` | 仅丢弃付款方 cancel 自找零 receive |
| peerOut / chainRemainder 公式 | 见 git；现用自找零 + 应付快照 |

---

## 7. 回归

```powershell
E:\work\github\wallet-api-go\scripts\run_btc_regression.ps1
```

形状抽查：同 tx 付款地址仅 send+fee；收款仅一条 receive；FinalAudit §3 通过。
