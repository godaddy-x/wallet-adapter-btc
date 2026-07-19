# BTC 付款方会计（业务源 sendOut）

与网关权威文档一致：`open_gateway/docs/BTC_SCANNER_FLOW.md`。

| 关联 | 路径 |
|------|------|
| 目标数据流 | `open_gateway/docs/BTC_SCANNER_FLOW.md` |
| 粉尘→矿工费 | `open_gateway/docs/BTC_DUST_CHANGE_TO_FEE.md` |
| 扫块适配器 | [BTC_BLOCK_SCANNER_FLOW.md](BTC_BLOCK_SCANNER_FLOW.md) |

> **2026-07-19：** 自找零模型；**废弃 peerOut 入账**。代码中 `attachPeerOutTargets` / peerOut 分支须收敛到本文，不得再作为正确路径。

---

## 1. 交易单产出（与网关三场景一致）

| 场景 | 每地址产出 |
|------|------------|
| 充值 | receive × 1，`+amount` |
| 单地址转出 | send + fee，`−sendOut`、`−fee` |
| 多地址汇总 | **每个付款地址** 同单地址：send + fee；收款方 receive × 1 |
| 粉尘残留 | 建单不生成找零 → 残留进 `fee`；仍是两条，无第三行 |

---

## 2. 规范摘要

| # | 规则 |
|---|------|
| R1 | 建单必写 `ExtParam.payerSendOut`（`"0"` = fee-only） |
| R2 | 扫块只读；禁止从 To / 比例推断 sendOut |
| R3 | `fee_i = (vin_i − change_i) − sendOut_i`；禁止比例分摊 chainFee |
| R4 | 缺数据 / `Σ sendOut ≠ external` / `Σ fee ≠ chainFee` → fail-stop |
| R5 | 找零只回本地址；禁止打到兄弟托管地址 |
| R6 | 低于粉尘阈值 → 无 change，残留计入 fee（建单侧） |

---

## 3. 每地址恒等式（自找零）

```text
vin_i      = 该地址 input 之和
change_i   = 打回自己的找零（无找零则为 0）
grossOut_i = vin_i − change_i
sendOut_i  = payerSendOut[i]
fee_i      = grossOut_i − sendOut_i    // ≥ 0

记账行：
  send  Amount = sendOut_i
  fee   Fees   = fee_i（fee_i>0 才落库）

Δbalance = −(sendOut_i + fee_i)
```

整笔：

```text
Σ sendOut_i = external
Σ fee_i     = chainFee
```

**已删除：** `peerOut_i`、`chainRemainder` 与「挂 ToAddr 给兄弟入账」——挪币必须另建转出单。

---

## 4. 多 payer 汇总

与单地址同一公式，按地址并列：

```text
每个 payer i：send_i + fee_i
收款方：receive
Σ sendOut = 汇总外部目标金额
Σ fee     = chainFee
```

业务在建单时写好各 `sendOut_i`；扫块不猜份额。

---

## 5. 实现索引

| 组件 | 路径 |
|------|------|
| 建单写入 | `internal/decoder/payer_send_out.go` |
| 扫块会计 | `internal/scanner/fee_extract.go` |
| 粉尘策略 | `internal/decoder`（`applyChangePolicy` 等，见网关 dust 文档） |
| scanner 查单 | `open_scanner/scanner/trade_order_outbound.go` |

---

## 6. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-07-19 | 去掉 peerOut 会计；与网关三场景 / 粉尘捐 fee 对齐 |
| 更早 | peerOut / chainRemainder 等见 git |
