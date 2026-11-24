# 架构总览

> **结论**：ngm 把"Git 依赖的可证明性"做成一条默认闭环——声明用 refType、锁定用 commit 与内容 digest、
> 落地用 vendor、复核用 verify。它不是包管理器替代品；差异化在流程闭环与共享状态模型，不在单项性能。

---

## 一句话架构

```
refType 声明 → commit 解析 → 内容 digest 锁定（archive / module）
  → vendor 落地（mirror / module cache / content store / link tree）
  → verify 漂移 + binding + 重放 + 落地四查
  → OSV / 白名单门禁 → 可复现构建
```

**数据流**：`ngm.json`（声明）经解析得到 commit，再从 mirror 的 commit tree 生成规范化清单与 digest，
两者写进 `ngm.lock`；install 按下来的 lock 落地 vendor；verify 重新解析 ref、重放 digest 并校验落地，
把结论交给退出码与报告。

**状态**：真相在 lock（commit + digest + `subPath`），不在 tag/branch，也不在 `resolvedAt`（只用于审计）。
同一仓库的不同子路径是两个独立条目（身份 `name#subPath`）。

**失败语义**：ref 漂移 exit 1、digest 不匹配 exit 2、配置/lock 错误 exit 3、网络失败 exit 4、
引擎不可用 exit 5、内部失败 exit 6；完整契约见[可观测性](./observability.md)。完整性与环境失败严格区分，
CI 才不会把"依赖被篡改"读成"机器缺工具"。

**边界**：只管 Git 来源依赖；registry 包、自研引擎、构建器、Dev Server 都不做（见[能力矩阵](../internals/capability-matrix.md)）。

---

## 三层运行时模型

| 层 | 技术 | 职责 |
|---|---|---|
| **ngm core** | Go 单二进制 + `internal/core`（无 JS 运行时） | 依赖图 / lock / 内容证明 / vendor / module cache / verify / 门禁 |
| **Engine adapters** | `embed` / `subprocess` / `wasm`（`remote` 按决定排除） | 调外部引擎；内置只有 esbuild（bundle/transform）与 `self` stub |
| **Host runtime** | Node.js 或 Deno | 用户代码实际运行的环境 |

core 做判定，adapter 只执行被交给它的那一个调用：引擎是子进程，必须能取消，且失败不静默改结论。
详见[引擎 adapter](./engine-adapter.md)与[安全模型](./security-model.md)。

---

## 子系统

| 子系统 | 数据流/状态 | 失败语义与边界 | 文档 |
|--------|------------|---------------|------|
| 依赖解析 | 4 种 Git URL → canonical；refType → commit；广度优先展开传递依赖 | 上游缺 refType 或传递冲突 → exit 3；不递归 `package.json` | [依赖解析](./dependency-resolution.md) |
| 模块集合 | 身份 `repo + subPath`；模块清单优先、根清单缺失时回退并 warning | 不做共享 lock 的 workspace | [依赖解析 · 传递依赖](./dependency-resolution.md#传递性依赖) |
| 锁定机制 | 根用 `archiveDigest`；模块用 `moduleDigest + subtreeOid`；`resolvedAt` 只审计 | 旧格式 MAJOR 不兼容时明确拒绝、重新 install；frozen 冲突 exit 3 | [锁定机制](./locking.md) |
| vendor / 缓存 | mirror → module cache → content store（blob 池 + 树清单）→ link tree | filter 不支持时明确回退完整 mirror；离线未命中 exit 4 | [依赖解析 · 落地](./dependency-resolution.md#6-vendor-落地) |
| 引擎层 | 统一 interface；选项→argv 映射按能力定义；`primary`/`fallbacks` 顺序尝试 | 未声明 exit 3；声明了但没装 exit 5；引擎自己失败 exit 1 | [引擎 adapter](./engine-adapter.md) |
| 供应链策略 | 策略状态与依赖图共享；锁定 commit 的 committer date 作为时间源 | 超阈值漏洞 exit 1；空 `supplyChain` 段不门禁 | [供应链防护](./supply-chain.md) |
| 可观测性 | 一份判定 → 退出码 / `--json` / 人类文本；渲染不新增判定 | 码 2 压过码 4；"没问到"不冒充漂移 | [可观测性](./observability.md) |

---

## 相关 ADR

决策的当前结论集中在[ADR 汇总表](../adr/README.md)。与本页直接相关的：
ADR-001（用 Go）、ADR-002（Git 直连）、ADR-003（纯 vendor 目录）、ADR-004（refType 声明 / commit 锁定）、
ADR-005（统一引擎接口）、ADR-031（一仓多模块）、ADR-032（模块级内容证明）、ADR-033（语言无关核心）、
ADR-038（不自研 Git）、ADR-042（项目脚本在沙箱外执行）、ADR-044（独立生态）。

## 相关文档

- [依赖解析](./dependency-resolution.md) · [锁定机制](./locking.md) · [引擎 adapter](./engine-adapter.md)
- [安全模型](./security-model.md) · [供应链防护](./supply-chain.md) · [可观测性](./observability.md)
- [能力矩阵](../internals/capability-matrix.md) · [开发者入口](../DEVELOPMENT.md)
