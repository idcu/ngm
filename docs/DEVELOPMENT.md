# 开发者入口

> **结论**：这是开发的唯一入口。构建、测试、加命令、加引擎、判据纪律、文档规则都在这里；
> 历史叙事与版本回顾不在本仓库的活文档里。先读本页，再按链接下钻。

---

## 1. 构建与本地校验

环境：Go ≥ 1.25、Git ≥ 2.30；Node.js / Deno 只在需要真实引擎的端到端用例时才需要。
三平台（macOS / Linux / Windows）都要过测试；`-race` 在 Windows 上需要 cgo，本地一般跑不了。

```bash
# 构建
go build ./...

# 测试与静态检查（CI 跑的是同一组命令）
go test ./...
go vet ./...

# 文档检查（链接 / 锚点 / 本地路径告警）
bash scripts/check-docs-links.sh

# 判据纪律自评（十问看板；--strict 让缺口直接失败）
node scripts/check-discipline.mjs --strict
```

CI 命令必须能在本地文档里找到同一句——上面四条就是本地入口。`go test ./...` 命中测试缓存时会"刚改完还是绿的"，
排查时加 `-count=1`。golden 变更用 `go test -update ./cmd/ngm` 重生成，并在 PR 说明差异。

---

## 2. 代码结构

| 位置 | 职责 |
|------|------|
| `cmd/ngm/` | CLI 入口：`root.go` 的 `commands` 表是命令事实源；各命令一个 `runXxx` 实现与一个 `xxxUsage` 常量 |
| `internal/core/` | 语言无关核心：`errs` / `git` / `lock` / `resolve` / `vendor` / `verify` / `digest` / `atomicfile` |
| `internal/adapter/` | 引擎适配器：`catalog.go` 的 `AdapterKind` 与清单解析；`subprocess` 是默认方式 |
| `internal/security/` | 权限判定与施加点；`internal/runid` 做引擎身份判定 |
| `internal/boundbuf/` | 子进程输出的有界捕获 |
| `testdata/` | golden（help / why / tree / lock 向量）；测试 fixture 由 `internal/testutils` 在临时目录现场创建 |

约束：只用 Go 标准库 + `golang.org/x/...` 按需；`wazero` 是当前唯一的直接第三方依赖（wasm 运行时）。
不自研 Git（统一在 `internal/core/git` 的子进程边界）、不自研引擎、不引入 shell 执行引擎命令。

---

## 3. 怎么加一条命令

1. 在 `cmd/ngm/` 写 `runXxx(ctx, args, stdout, stderr) int`，以及 `xxxUsage` 常量
   ——Usage **就是**参数错误时打印的那份文本，不另写帮助文案
2. 在 `root.go` 的 `commands` 表加一项，按命令语义决定是否声明 `JSON: true`（接受 `--json`）
3. 写退出码到 Usage 的 `EXIT CODES` 段——码只能取全局契约 0–6 的子集（见[可观测性](./architecture/observability.md)）
4. 同步三处：`guides/cli.md`、`guides/installation.md` 的 help 片段、`testdata/help.golden`（用 `-update` 重生成）。
   这四处由 `TestHelpGolden` 与 `TestV115` 机械核对，漏一处会红
5. 在[能力矩阵](./internals/capability-matrix.md)的机器可读区加一行 `| command | <name> | done |`
   ——`TestV222CapabilityMatrixMatchesCode` 双向核对矩阵与 `commands` 表
6. 更新[文档索引](./README.md)；用法示例必须能跑通（`TestV15DocExamplesAreRealInvocations` 扫活文档里的 `ngm` 调用）

---

## 4. 怎么加一个引擎

1. 确定能力 kind（`transform` / `bundle` / `typeCheck` / `typeDecl` / `css`）与 adapter 类型。
   新增 adapter kind 要同时改 `internal/adapter/catalog.go` 的 `AdapterKind` 与[能力矩阵](./internals/capability-matrix.md)
   的 `adapter` 行（remote 永久是 `excluded`，不会复活）
2. 引擎清单条目按 `(name, kind)` 唯一；内置条目只收录已适配的，且一律 `optional`——没装不算 issue
3. 走 `Catalog.Probe` 做可用性判定与 `run:<engine>` 闸门；探测与执行必须是同一条路径
4. 每个方法接收 `context.Context`，引擎失败映射成 exit 1；没装 exit 5，未声明 exit 3
5. 子进程输出走 `internal/boundbuf`；产物通道（transform / css / 无输出文件的 build）用大上界且撞界即失败
6. 更新[引擎 adapter](./architecture/engine-adapter.md)与[配置详解](./guides/configuration.md)

---

## 5. 判据纪律

- **规格即测试**：契约写在文档（退出码表、错误码枚举、能力矩阵、ADR 汇总），判据从文档或源码派生，不从记忆抄
- **双向核对**：新增来源要让判据变红；整行删掉也要变红（canonical 集合）
- **判据有牙齿**：负例写在 Go 测试里；行为退化必须能构造出失败用例
- **渲染不引入判定**：同一份结果给退出码、`--json`、外部 schema 与人类文本；渲染层不改结论
- **缺工具 ≠ 已验证**：需要真实外部工具的用例在工具缺失时自动跳过，不伪装成通过
- 新增判据要先指着一个**已发生的产品缺陷**（ADR 汇总表 ADR-049）；停止新建判据，存量作回归档案保留

本地自检见第 1 节。`node scripts/check-discipline.mjs --strict` 检查十问：判据、CI、本地同一条命令、
契约文件、生成物、依赖锁定、文档分层、删除记录、删除记录被守、牙齿。

---

## 6. 文档规则

- 每篇 ≤400 行、中文、先结论后细节；不写历史叙事、性能读数、版本回顾
- 活文档：根 `README.md`、`docs/README.md`、`guides/`、`architecture/`、`internals/`、`adr/README.md`、本页
  决策以 ADR 汇总表为准；旧版本叙事不保留在工作树里（见 `deletions.md`）
- 同一事实只在一处维护：能力矩阵 / 可观测性 / ADR 汇总 / 能力归属各有唯一位置，其余引用
- 链接目标必须是现有文件；锚点只在目标标题真实存在时使用；不指向已删的历史文档
- 决策的当前结论在 [ADR 汇总表](./adr/README.md)；实现与决策冲突时先改决策再改代码
- 结构性删除要留账：[deletions.md](./deletions.md) 每条写全删除/日期/批准/为什么/影响/验证六字段

---

## 7. 完整索引

[文档索引](./README.md) · [架构总览](./architecture/overview.md) · [能力矩阵](./internals/capability-matrix.md) ·
[ADR 汇总](./adr/README.md) · [开发总览](./development/README.md) · [删除记录](./deletions.md)
