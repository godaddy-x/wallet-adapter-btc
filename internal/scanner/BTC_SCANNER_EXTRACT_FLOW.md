# BTC 扫块 extract 实现索引

> **唯一权威文档**（出站 §5–§7、粉尘 §6、extract **§9**、outputIndex §4）：  
> [open_gateway/docs/BTC_SCANNER_FLOW.md](https://github.com/godaddy-x/open_gateway/blob/main/docs/BTC_SCANNER_FLOW.md)

本文件仅便于在 adapter 仓库内跳转代码，不重复业务规则。

## 入口

| 函数 | 文件 |
|------|------|
| `ScanBlockWithResult` | `blockscanner.go` |
| `ExtractTransactionAndReceiptData` | `blockscanner.go` |
| `VerifyTransactionByTxID` | `blockscanner.go` |
| `extractTransaction` | `blockscanner.go` |
| `verifyBlockPackageIntegrity` | `block_integrity.go` |
| `appendBTCTransactionFeeItems` | `fee_extract.go` |
| `VerifyBeforePromote` | `cross_check.go` |

## 单测

- `blockscanner_test.go` — extract 腿、fee、vin 解析
- `block_integrity_test.go` — Phase A
- `scan_targets_test.go` — touch / 充值无 prevout RPC
- `fee_extract_test.go` — 多 payer fee 形状
