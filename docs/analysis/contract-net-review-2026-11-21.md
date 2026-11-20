# 契约网存量复核（2026-11-21）

> **状态：只读复核，未删任何网**。依据 [ADR-049](../adr/adr-049-contract-net-freeze.md) 的两条
> 复核门槛。按 spec §3.3，本轮只产出"候选与数字"，删除需另过门槛（连续 3 版因自身红且不影响不变量）。

## 0. 结论

**未发现满足删除门槛的网**。存量契约网按 ADR-049 是"历史缺陷的回归档案"，
本轮复核后**保留全部**。复核的价值是把"网很多"的印象变成可核对的数字。

## 1. 实测读数

| 类别 | 数量 | 采集命令 |
|---|---|---|
| `cmd/ngm` 测试文件总数 | **185** | `ls cmd/ngm/*_test.go \| wc -l` |
| AST 类契约网（import `go/ast`/`go/parser` 或 AST 遍历） | **8** | `grep -rlE '"go/ast"\|"go/parser"\|ast\.Inspect\|parser\.ParseFile' cmd/ngm/*_test.go \| wc -l` |
| 源码文件读取类判据（`os.ReadFile(filepath.Join(root,...))`） | **6** | `grep -rlE 'os\.ReadFile\(filepath\.Join\(root' cmd/ngm/*_test.go \| wc -l` |
| ADR-049 记录的"约 39 个契约网" | 口径不同 | 旧数按更宽的"源码扫描"口径；本轮用严格的 AST/文件读取口径，得 8+6=14，不冲突 |

AST 类 8 个文件：
`atomic_write_guard_test.go`、`core_boundary_test.go`、`doc_prose_fields_test.go`、
`error_hint_runtime_test.go`、`exit_codes_sources_test.go`、`flag_hint_guard_test.go`、
`git_backend_boundary_test.go`、`hint_contract_test.go`。

## 2. 门槛核对（ADR-049 两条）

| 门槛 | 核对结果 |
|---|---|
| 某网连续 **3 个版本**因自身（文档措辞、判据实现）而红、却从未抓到产品缺陷 | **无候选**：最近 3 版（v0.100–v0.102 开发线）的判据红都指向真实缺陷（net 门禁、退出码、覆盖率算法等），没有"纯粹自己红"的网 |
| 删除它不影响任何产品不变量，且能明确减少维护成本（给数字） | **无候选**：8 个 AST 网各自守不同不变量（core 边界、git 单构造点、flag 名、hint 契约、JSON 形状、原子写），无不变量重叠 |

## 3. 建议

保留全部存量网。**复核节奏**（沿用 ADR-049）：仅当出现上面两条之一时才重新评估，
不做"定期清理"——那会周期性地重写 golden 并制造新的漂移面。

## 4. 相关文档

- [ADR-049 契约网冻结](../adr/adr-049-contract-net-freeze.md)
- [roadmap 停止清单](../internals/roadmap.md)
- [competitive-followup spec §3.3](../specs/assessment-2026-11-21-competitive-followup/spec.md)
