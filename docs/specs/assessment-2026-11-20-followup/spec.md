# Spec：2026-11-20 全维度评估后的修复批次

依据：`docs/analysis/ngm-full-assessment-2026-11-20.md`（全量发现、证据、缺陷清单与优先级）。
基线：HEAD `5961cbc`，工作树已含本次评估的文档纠错与清理（ADR 与逐版本归档保留）。
本 spec 只定义**下一轮做什么、按什么顺序、怎么验收**；逐项清单见 [tasks.md](./tasks.md)。
禁止强推、改历史、改 git config。

## 0. 结论：本轮按 A→B→C→D 四批推进

1. **A 止血（P0，唯一违反已声明安全承诺的项）**：`ModuleCache.Ensure` 首次 clone 绕过
   `net:` 门禁——`deny net:` 或未放行时仍真实触网。这是整个评估里唯一“文档承诺了、实现没有”的安全边界。
2. **B 防“装得出、验不过”（P1 信任根）**：安装期不校验清单记录（反斜杠/换行路径）、
   `parseListTreeZ` 静默丢畸形条目——两者都会让工具造出一个自己随后拒绝的世界。
3. **C 修对外契约（P1 确定性 / 退出码 / 内存）**：`verify --json` 的 `Details` 未排序、
   `gcExitCode` 漏算 module cache 错误、WASM 输出无界、`--json` 错误信封在早期失败路径为空、
   stale lock 仍产出待签名产物。
4. **D 工程与文档收尾（P2）**：CI job 超时 < `go test` 超时、重复检查、版本号语义
   （内部 `vNNN` 任务号 ≠ 已发布 tag）、`build-release.ps1` 双实现。

**不改退出码 0–6 的含义、不动 `ngm run` 既有语义、不新增 registry 能力、不删 ADR / 逐版本归档。**
发布资产补齐需外部凭据，单列为不占工程排期的人工动作。

## 1. 不变量（任何批次都不得破坏）

- 退出码 0–6 单码语义不变；完整性失败(2) 不被资源缺失(4) 或工具缺失(5) 覆盖（ADR-039）。
- `run:git` / `net:<host>` / `run:<engine>` / `run:script` 五套闸门不合并、不互相放行；
  **任何触网路径**在启动子进程前都必须过 `CheckNetAccess`。
- git 只有 `internal/core/git/spawn.go` 一个子进程构造点；引擎只有 `internal/adapter/subprocess.go`。
- `--json` 时 stdout 恰好一份 JSON；人读与 JSON 同源同码；不接受 `--json` 的命令失败时 stdout 为空（v0.16 规则④）。
- 序列化确定性：排序 + 固定缩进/LF/末尾换行；任何写进报告的切片，顺序必须可复现。
- `internal/core` 不 import 语言/JS 层；所有 `t.Skip` 必须登记进 skip 账本。
- 修复必须带**变异检验**：撤掉修复即红、装上即绿——判据要有牙齿（除非该项本身只改文档）。

## 2. 批次与耦合

| 批次 | 内容 | 前置 | 提交粒度 | 风险 |
|---|---|---|---|---|
| A | A1 net 门禁 + A2 零命中判据 | — | 1 提交 | 低（对齐 Mirror 现成结构） |
| B | B1 安装期 `ValidateRecord` + B2 `parseListTreeZ` fail-closed | — | 每提交 1 项 | 中（需定“不支持文件名”的取舍，见 §3.1） |
| C | C1 Details 排序 · C2 gc 退出码 · C3 信封一致性 · C4 WASM boundbuf · C5 stale lock 门槛 | A/B 合入后（先安全后契约） | 每提交 1 项 | 中（C3 动骨架，需复查归一化类判据；C5 需先定契约） |
| D | D1 CI 超时 · D2 重复检查 · D3 版本号语义 · D4 ps1 双实现 | — | 每提交 1 项 | 低（多为配置/文档） |

## 3. 必须先做的契约决定

**3.1 安装期遇到“合法 Git 路径但 ngm 不支持”（`\` / 换行）怎么办？**
B 批在动代码前先拍板，两个选项：
- (a) **fail-closed**：install 期 exit 3 并点名路径，禁止写入 lock。代价：极端历史仓库可能无法安装。
- (b) **放宽 `ValidateRecord`**：让这些字节进入清单与验证。代价：改变 digest 输入契约，属格式变更，不应轻易做。
默认建议 (a)，与 `ValidateRecord` 现有 fail-closed 精神一致；决定写进 task B1 及相应 ADR 补记。

**3.2 stale lock 的 `attest` / `sbom` 门槛（C5）**
先定契约再改代码（照 F1/ADR-045 的教训）：`--json` 的解码语义是否包含“lock 新鲜度”？
- 建议：产物类（sbom/attest）在 lock 过期时**默认拒绝**（exit 3），提供显式 `--allow-stale` 逃生口；
  `lockStale` 不塞进 statement 字节（那会破坏确定性），而是靠退出码与 stderr 表达。
- 该决定落成 ADR-046 或在 ADR-027 补记后再实现。

**3.3 内部 `vNNN` 任务号 ≠ 已发布 tag（D3）**
`git tag` 只到 `v0.101.0` + `v0.102.0-dev.1/.2`。文档里“v0.120 / v0.135 / v0.136 新增/修”
是内部批次号，不是发布版本。先统一写法（如“本开发线（未打 tag）”或“见 CHANGELOG [Unreleased]”），
再改 `cli.md` / `configuration.md` / `security-model.md`，否则只是把错误复制到新文档。

## 4. 验收总闸（每批提交前都要过）

```bash
go build ./... && go vet ./...
go test -count=1 ./internal/...                    # 单元与核心库
go test -count=1 ./cmd/ngm -run '<本批判据>'        # 定向判据
bash scripts/check-docs-links.sh docs README.md
node scripts/check-discipline.mjs                   # 必须仍是 13/13
```

- 改了源码的每一项：加一条判据 + 变异检验；`t.Skip` 若新增必须同步 skip 账本。
- 改了 JSON 形状的项（C1/C2/C3）：同步 `docs/guides/cli.md` 形状表（`TestV26` 反向守护）。
- 改了文档读数的项：与 `docs/guides/release-pipeline.md` 同源（63/3/38/101 被 `TestV126` 守着）。

## 5. 明确不做（停止清单仍然有效）

- **不新建“契约网”**：roadmap 已记录“最后一个产品缺陷是 v0.55，之后 10 版抓的全是文档与判据自己”。
  本轮判据只针对**已发生的真实缺陷**（A1/C1/C2/C4/C5），不为假想问题加网。
- **不删 ADR（46 个）与逐版本归档（211 个）**：17+ 测试引用它们；与文档范围选择冲突。
- **不动 `materialCache` / `--store` flag 语义 / `--` 分隔符截获**等 P2/P3 项：登记在 tasks 的“候选池”，
  等有真实触发再排期。

## 6. 相关文档

- [全维度代码评估（2026-11-20）](../../analysis/ngm-full-assessment-2026-11-20.md) —— 本轮依据
- [上一批 followup](../assessment-2026-11-18-followup/spec.md) —— 批次组织与验收方式的样板
- [ADR-039](../../adr/adr-039-exit-code-precedence.md) · [ADR-045](../../adr/adr-045-json-output-contract.md) —— 退出码与 JSON 契约
