# BTC 付款方会计（业务源 sendOut）

与网关权威文档一致：`open_gateway/docs/BTC_SCANNER_FLOW.md`。

| 关联 | 路径 |
|------|------|
| 目标数据流 | `open_gateway/docs/BTC_SCANNER_FLOW.md` |
| 粉尘→矿工费 | `open_gateway/docs/BTC_DUST_CHANGE_TO_FEE.md` |
| 扫块适配器 | [BTC_BLOCK_SCANNER_FLOW.md](BTC_BLOCK_SCANNER_FLOW.md) |

> **2026-07-19：** 自找零模型；**废弃 peerOut 入账**。手续费由恒等式闭合，**禁止对 chainFee 做比例/边际分摊**。

---

## 1. 核心闭环（只认应付）

```text
建单（tx 已选好 UTXO / 输出后）
  已有：vin_i、change_i、external（业务 To，不含自找零）
  写入：ExtParam.payerSendOut[i] = sendOut_i   // 应付
       ↓
广播 → ow_trade（reqType=2，payload 含 payerSendOut）
       ↓
扫块
  链上算：vin_i、change_i
  查单读：sendOut_i（只读，不猜）
  算出：  fee_i = (vin_i − change_i) − sendOut_i
```

扫块**不**分配手续费；只消费建单已存的应付。

---

## 2. 交易单产出

| 场景 | 每地址产出 |
|------|------------|
| 充值 | receive × 1，`+amount` |
| 单地址转出 | send + fee，`−sendOut`、`−fee` |
| 多地址汇总 | **每个付款地址** 同单地址：send + fee；收款方 receive × 1 |
| 粉尘残留 | 建单不生成找零 → 残留进 `fee`；仍是两条，无第三行 |
| 取消 / fee-only | `sendOut="0"` → 通常仅 fee 行 |

---

## 3. 规范摘要

| # | 规则 |
|---|------|
| R1 | 建单必写 `ExtParam.payerSendOut`（`"0"` = fee-only） |
| R2 | 扫块只读；禁止从链上 To / 比例推断 sendOut |
| R3 | `fee_i = (vin_i − change_i) − sendOut_i`；禁止分摊 chainFee |
| R4 | 缺数据 / `Σ sendOut ≠ external` / `Σ fee ≠ chainFee`（±1 sat）→ fail-stop |
| R5 | 找零只回本地址；禁止打到兄弟托管地址 |
| R6 | 低于粉尘阈值 → 无 change，残留计入 fee（建单侧） |

---

## 4. 每地址恒等式

```text
vin_i      = 该地址 input 之和
change_i   = 打回自己的找零（无找零 / 已捐矿工则为 0）
grossOut_i = vin_i − change_i
sendOut_i  = payerSendOut[i]          // 应付（建单写入）
fee_i      = grossOut_i − sendOut_i   // ≥ 0，否则 fail-stop

记账行：
  send  Amount = sendOut_i
  fee   Fees   = fee_i（fee_i>0 才落库）

Δbalance_i = −(sendOut_i + fee_i) = −grossOut_i
```

整笔：

```text
Σ sendOut_i = external
Σ fee_i     = chainFee
```

**已删除：** `peerOut_i`、`chainRemainder`、挂 ToAddr 给兄弟入账。

---

## 5. 建单：如何写应付（`writePayerSendOutExtParam`）

文件：`internal/decoder/payer_send_out.go`  
调用点：`buildRawTransaction` 末尾（UTXO / 输出已定）。

### 5.1 输入（建单时已固定）

| 输入 | 来源 |
|------|------|
| `vin_i` | `usedUTXO` 按地址汇总 |
| `change_i` | `allOutputs` 中打回付款方自己的金额 |
| `external` | `rawTx.To` 合计（业务外部，**不含**自找零） |

### 5.2 写入规则

| 条件 | `payerSendOut` |
|------|----------------|
| 仅 1 个付款地址 | `sendOut = external`；若 `external=0` 则 `"0"` |
| 多付款地址且 `external=0` | 每个地址 `"0"`（取消 / 纯 fee） |
| 多付款 + dust+main（UTXO 顺序覆盖后**仅 1 个**正应付） | 出资方 = 外部金额，其余 `"0"`（fee-only） |
| 多付款且多人共同覆盖外部（汇总） | 按下式写各地址应付（见 §5.3） |

### 5.3 多出资方：按各地址 grossOut 写应付

建单**不算、不分摊**手续费。只把已固定的外部合计拆成各地址应付：

```text
grossOut_i = vin_i − change_i
sendOut_i  = external × grossOut_i / Σ grossOut   // sat 整数；最后一腿吃余数
```

之后扫块：

```text
fee_i = grossOut_i − sendOut_i
```

因此：vin（净支出）大的地址应付大，手续费也自然大——这是恒等式结果，不是对 `chainFee` 做费率/边际分摊。

dust+main **不走**本分支：UTXO 顺序一人盖住外部即可，其余应付为 `"0"`。

---

## 6. 扫块：只读应付算 fee（`fee_extract.go`）

```text
GetTradeOrderOutbound(txID)
  → legs[].SendOut = payerSendOut[i]

链上：
  vin_i、change_i（自找零；打到兄弟托管 → illegal peerIn → fail-stop）
  grossOut_i = vin_i − change_i
  fee_i      = grossOut_i − sendOut_i

校验（±1 sat）：
  Σ sendOut ≈ 链上 external（排除付款方自找零）
  Σ fee     ≈ chainFee = Σvin − Σvout
```

产出：每付款地址 `send` +（fee>0 时）`fee`；收款方 `receive`。

查单：`open_scanner/scanner/trade_order_outbound.go`。

---

## 7. 多 payer 汇总形状

```text
每个 payer i：send_i + fee_i
收款方：receive
Σ sendOut = 汇总外部目标金额
Σ fee     = chainFee
```

与单地址同一公式，只是付款地址个数 > 1。

---

## 8. 实现索引

| 组件 | 路径 |
|------|------|
| 建单写应付 | `internal/decoder/payer_send_out.go` |
| 扫块会计 | `internal/scanner/fee_extract.go` |
| 粉尘策略 | `internal/decoder`（`applyChangePolicy` 等） |
| scanner 查单 | `open_scanner/scanner/trade_order_outbound.go` |

---

## 9. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-07-19 | 明确：建单只写应付；扫块恒等式算 fee；去掉 chainFee 边际/比例分摊 |
| 2026-07-19 | 去掉 peerOut 会计；与网关三场景 / 粉尘捐 fee 对齐 |
| 更早 | peerOut / chainRemainder 等见 git |
