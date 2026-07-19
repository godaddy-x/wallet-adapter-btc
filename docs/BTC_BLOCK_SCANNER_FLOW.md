# BTC 扫块适配器流程（目标模型）

权威产品与验收：`open_gateway/docs/BTC_SCANNER_FLOW.md`。  
付款方会计： [BTC_BUSINESS_SOURCE_PAYER_ACCOUNTING.md](BTC_BUSINESS_SOURCE_PAYER_ACCOUNTING.md)。

| 组件 | 路径 |
|------|------|
| 适配器扫块 | `wallet-adapter-btc/internal/scanner/` |
| 进程入口 | `open_scanner_btc/main.go` |
| 链上入账 | `open_scanner/scanner/` |

> **2026-07-19：** 产出形状固定为充值 1 条 / 转出 send+fee / 汇总同构。  
> **废弃：** peerOut 挂 To、filterReceive 多层去重作为正确性来源（见网关 `BTC_PEEROUT_RECONCILE_FLOW.md`）。

---

## 1. 数据流

```text
bitcoind
  → RunScanLoop / ScanBlockWithResult
  → extractTransaction（按托管地址 vin/vout）
  → appendBTCTransactionFeeItems
        读 payerSendOut
        change = 自找零（或 0，粉尘已捐 fee）
        fee = (vin − change) − sendOut
  → []ExtractDataItem
  → open_scanner HandleBlock
        newly → promote → balance
  → FinalAudit：DB == 链上 UTXO
```

---

## 2. 目标提取形状（清理后）

| 场景 | Extract 行 |
|------|------------|
| 充值 | 地址 `receive` × 1，`+50` |
| 单地址转出 | 地址 `send` `−50` + `fee` `−0.5` |
| 汇总 | 每付款地址同单地址；目标地址 `receive` × 1 |
| 粉尘 | 无找零 vout 时 fee 含残留；仍两行，无第三行 |

付款方 ToAddr **只含外部收款**（或 internal 展示约定中的自找零展示，**不**含兄弟托管入账）。  
收款方只靠自己的 `receive` 加余额。

---

## 3. 入口函数

| 函数 | 文件 | 说明 |
|------|------|------|
| `RunScanLoop` | `blockscanner.go` | 持续扫块 |
| `ScanBlockWithResult` | `blockscanner.go` | 单块 |
| `extractTransaction` | `blockscanner.go` | 按地址 leg |
| `appendBTCTransactionFeeItems` | `fee_extract.go` | sendOut / fee |
| `inferBTCTxAction` | `tx_action.go` | receive / send / internal |

单块：拉块 → 过滤托管相关 tx → extract → fee 归一化 → 返回 items。

---

## 4. 会计（自找零）

```text
vin_i / change_i（仅回自己）/ sendOut_i / fee_i
fee_i = (vin_i − change_i) − sendOut_i
Σ sendOut = external，Σ fee = chainFee
失败 → business payer accounting failed，块高不推进
```

建单粉尘策略见 `open_gateway/docs/BTC_DUST_CHANGE_TO_FEE.md`。

---

## 5. open_scanner 侧（期望行为）

```text
saveUnConfirmedTx → ow_trade_newly
retryPromoteConfirmableNewly
  send  → −sendOut
  fee   → −fee
  receive → +amount
全部可确认 newly 清空 → 推进 curHeight
```

不依赖：`attachPeerOutTargets` 给兄弟加余额、`shouldSkipCoveredReceive*`、清 outbound 正 delta 作为主路径。

---

## 6. 与旧文档差异

| 旧步骤 | 状态 |
|--------|------|
| `attachPeerOutTargets` 入账 | 废弃 |
| `filterPayerSelfChangeReceive` | 仅丢弃付款方 cancel 自找零 receive |
| scanner ②–⑤ 去重 | 同废；目标形状不产生重复腿 |
| peerOut / chainRemainder 公式 | 见 git；现用自找零公式 |

---

## 7. 回归

```powershell
E:\work\github\wallet-api-go\scripts\run_btc_regression.ps1
```

形状抽查：同 tx 付款地址仅 send+fee；收款仅一条 receive；FinalAudit §3 通过。
