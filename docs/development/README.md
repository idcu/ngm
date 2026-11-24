# 开发总览

> **结论**：本页只保留开发工作的方式与验收口径。逐版计划、复盘、评估与读数已随历史文档清理移出工作树，
> 历史用 `git log --oneline -- docs/` 查。开发者唯一入口是[DEVELOPMENT.md](../DEVELOPMENT.md)。

---

## 本地怎么跑

```bash
# 构建与全量校验
go build ./...
go test ./...
go vet ./...

# 文档链接检查（只认现有文件，锚点由 Go 判据单独查）
bash scripts/check-docs-links.sh

# 判据纪律自评（十问；--strict 让缺口直接失败）
node scripts/check-discipline.mjs --strict
```

CI 跑的是同一组命令；本地与 CI 必须是同一条，这条由 `check-discipline` 的第 3 问核对。
`go test ./...` 可能命中缓存，排查"改完还是绿的"时加 `-count=1`。需要真实引擎的用例缺工具时自动跳过，
不报成通过。golden 用 `go test -update ./cmd/ngm` 重生成并在 PR 说明。

---

## 工作流

1. **分支与提交**：`main` 保护 + feature 分支；提交信息用 Conventional Commits（feat / fix / test / docs / chore）
2. **每个 PR 的完成定义**：
   - `go build ./...`、`go test ./...`、`go vet ./...` 全绿
   - `gofmt -l ./cmd ./internal` 无输出（CRLF 会让 gofmt 判为未格式化，行尾纪律见 `.gitattributes`）
   - 涉及输出格式（lock / digest / mappings / golden）的变更必须说明差异
   - 行为偏离文档时先提文档/ADR 变更，再提代码
3. **CI**：三平台矩阵跑 build / vet / test，`go test -race`（ubuntu）、gofmt 与行尾检查、
   里程碑验收、lock 字节可复现、真实引擎集成、文档链接检查
4. **判据纪律**：新增判据必须指向已发生的产品缺陷；存量判据作回归档案保留，见
   [DEVELOPMENT.md · 判据纪律](../DEVELOPMENT.md#5-判据纪律) 与 [ADR 汇总表](../adr/README.md)

---

## 测试策略

| 策略 | 说明 |
|------|------|
| 现场 fixture | `internal/testutils` 用临时目录创建本地 Git 仓库（tag / annotated tag / branch / force push / monorepo 子路径），不依赖公网、不残留状态 |
| golden | lock、digest 向量、help、why/tree 输出全部快照化，支持 `-update` 重生成 |
| 表驱动 | URL 归一化、退出码、schema 校验、漂移分类全部表驱动 |
| 规格即测试 | 退出码表、错误码枚举、能力矩阵、ADR 结论逐条对应测试用例 |
| 确定性优先 | 禁止 map 迭代参与序列化；时间字段排除在可复现断言外；三平台同一字节 |

单包、单验收的运行方式：

```bash
go test -count=1 ./internal/vendor
go test -count=1 -run 'TestM1Acceptance' -v ./cmd/ngm
go test -count=1 -run 'TestM[1-7]Acceptance' -v ./cmd/ngm
```

---

## 开发环境

| 工具 | 要求 | 用途 |
|------|------|------|
| Go | ≥ 1.22 | 编译 ngm（运行时不依赖 Go） |
| Git | ≥ 2.30 | 依赖拉取、测试 fixture |
| Node.js / Deno | 可选 | 需要真实引擎的端到端用例 |

代码与文档同仓：`docs/` 就是文档目录，这样 ADR 变更与实现改动能在同一个 PR 里评审。
依赖最小化：只用标准库 + `golang.org/x/...` 按需，`wazero` 是当前唯一的直接第三方依赖。

---

## 文档分工

| 内容 | 位置 |
|------|------|
| 开发者唯一入口（构建 / 加命令 / 加引擎 / 纪律 / 文档规则） | [DEVELOPMENT.md](../DEVELOPMENT.md) |
| 文档索引 | [docs/README.md](../README.md) |
| 架构：数据流 / 状态 / 失败语义 / 边界 | [architecture/](../architecture/) |
| 决策当前结论 | [ADR 汇总表](../adr/README.md) |
| 能力归属与成熟度 | [能力矩阵](../internals/capability-matrix.md) |
| 历史文档删除台账 | [deletions.md](../deletions.md) |

> 原来的逐版计划 / 复盘 /  ledger / stages / acceptance 等文件已不属于工作树；需要时从 git 历史读取，
> 不在活文档里复制它们的读数。
