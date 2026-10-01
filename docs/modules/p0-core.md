# P0 — ngm core

> 核心层：CLI、配置加载、依赖图、lock schema、vendor 4 层、verify
> （`audit` 属 P3，已随 v0.2 交付——见[能力矩阵](../internals/capability-matrix.md)）

---

## 职责

ngm core 是"Git-first 依赖证明层"的最小完整实现。它不做构建、不做测试、不做文档、不做 Dev Server。

---

## 模块结构

```
cmd/ngm/                 # CLI：一个命令一个文件；root.go 是命令表与分发
├── main.go              # 入口
├── root.go              # 命令表、参数重排（normalizeArgs）、顶层分发
├── init.go  add.go  install.go  update.go  remove.go  verify.go
├── build.go  typecheck.go（css 同文件）  typedecl.go  engine.go
├── mappings.go  cache.go  config.go  engines.go
├── help.go  env.go      # help 文本、项目环境与路径解析
└── acceptance_m1..m7_test.go 等  # 各阶段的端到端验收

internal/                # 12 个包，按职责划分
├── config/              # ngm.json / ngm.engines.json / ~/.ngm/config.json 的读写与 schema 校验
├── resolve/             # URL 归一化、refType 解析、依赖图、冲突检测、add 语法
├── git/                 # Git 子进程封装：ls-remote / clone / cat-file / tree / ancestry / 凭证
├── digest/              # archiveDigest：canonical manifest（ADR-008）+ LFS 指针检测
├── lock/                # ngm.lock 的 schema、读写与比较
├── vendor/              # 4 层：mirror / content store / link tree / cache + 完整性校验
├── verify/              # ngm verify 引擎：三级检查、漂移分类、退出码契约、JSON 报告
├── mappings/            # ngm.mappings.json 的生成与校验
├── adapter/             # 引擎 adapter：catalog（内置清单）/ runner / subprocess / self
├── testutils/           # 测试用 Git fixture 构造器（annotated tag、force push、monorepo…）
├── errs/                # 错误模型与退出码契约
└── version/             # 版本元信息（由 -ldflags 注入）
```

> 本页是模块结构的**唯一事实源**，但只精确到**包级**——文件级结构请直接读代码（本文不再维护一份会迅速腐坏的文件清单）。
>
> **包的存在状态**：`internal/supplychain/` 已在 v0.2 创建（`policy.go` / `allowlist.go` /
> `minimumage.go` / `osv.go` / `audit.go` / `report.go`；postinstall 的**执行入口**自 v0.3 起在
> `cmd/ngm/postinstall.go`，沙箱内执行）；
> `internal/observability/` 已在 v0.2 D 组创建（`tree.go` / `why.go` / `outdated.go`）；
> `internal/integrations/` 已在 v0.3 B 组创建（工具抽象与冲突策略、四个工具的生成器、
> tsconfig paths 与行级 diff；见 [P5](./p5-integrations.md)）。
>
> 归属约定：`audit` 的实现归 `internal/supplychain/`（P3）；`ngm verify` 的检查与判定在 `internal/verify/`，
> 而"落地树 vs content store 的逐文件比对"在 `internal/vendor/integrity.go`；CLI 入口统一在 `cmd/ngm/`。

---

## 关键设计决策

### 1. CLI 框架

使用 `spf13/cobra` 或标准库 `flag`：

- `cobra`：子命令多，生态成熟
- 标准库 `flag`：零依赖，体积小

**选择**：v0.1 用标准库 `flag`，避免引入依赖。

### 2. 配置三级合并

```
内置（编译进二进制）→ 全局（~/.ngm/config.json）→ 项目（./ngm.json）
```

后者覆盖前者。合并逻辑：

```go
func MergeConfigs(builtin, global, project Config) Config {
    // project 覆盖 global，global 覆盖 builtin
}
```

### 3. 并发模型

- Git fetch：goroutine 池（默认 4 并发）
- archiveDigest 计算：每个依赖一个 goroutine
- lock file 写入：互斥锁保护

### 4. 错误模型

```go
type NgmError struct {
    Code    ErrorCode
    Message string
    Cause   error
    Hint    string  // 给用户的可操作建议
}

type ErrorCode int

// 1-5 与全局退出码约定对齐（见 architecture/observability.md）
const (
    ErrRefDrift ErrorCode = iota + 1 // 1 非预期漂移
    ErrDigestMismatch                // 2 完整性失败（digest 重放不匹配）
    ErrConfigInvalid                 // 3 配置/策略/lock 错误
    ErrGitFetch                      // 4 Git/网络失败
    ErrEngineNotFound                // 5 引擎不可用
)
```

退出码的唯一事实源是[可观测性 · 退出码约定](../architecture/observability.md)（本页上方的代码注释也指向它）。

---

## 成熟度

| 子模块 | 成熟度 | 备注 |
|--------|--------|------|
| CLI | done (v0.1) | 标准库 flag |
| 配置加载 | done (v0.1) | JSON + schema 校验 |
| Git 操作 | done (v0.1) | 4 种协议归一化 |
| 依赖解析 | done (v0.1) | refType + 冲突检测 |
| lock 机制 | done (v0.1) | **最高优先级** |
| vendor 4 层 | done (v0.1) | mirror / content / link tree / cache |
| verify | done (v0.1) | **核心差异化** |
| engine adapter | done (v0.1)；**v0.2 增 typescript / postcss，v0.3 增 wasm** | subprocess 优先；`remote` 按 [ADR-013](../adr/adr-013-remote-adapter.md) 排除 |
| mappings | done (v0.1) | 供外部构建工具读取 |
| audit | done (v0.2) | OSV.dev 集成；实现归 `internal/supplychain/`（P3） |

> 成熟度口径与唯一事实源[能力矩阵](../internals/capability-matrix.md)一致；本表只做索引，不重新定义归属。

---

## v0.1 必须完成的

1. refType 解析（commit/tag/branch）
2. commit 锁定
3. archiveDigest 计算
4. vendor 4 层落地
5. lock file 读写
6. verify（ref 漂移 + digest 检查）
7. esbuild adapter（subprocess）
8. mappings 生成

---

## 相关文档

- [架构总览](../architecture/overview.md)
- [运行时模型](../architecture/runtime-model.md)
- [信任模型](../architecture/trust-model.md)
- [ADR-001：为什么用 Go](../adr/adr-001-go.md)
