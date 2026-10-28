# ADR-038：不自研 Git，保持子进程 + 单一边界

- **状态**：已定
- **日期**：2026-10-28
- **范围**：Git 对象/协议访问、跨平台、性能、可嵌入性
- **相关**：[ADR-002](./adr-002-git-direct.md)、[ADR-030](./adr-030-mirror-reclaim.md)、[ADR-034](./adr-034-module-cache.md)

> **结论**：ngm / modproof **不自己实现 Git 的对象格式、packfile、传输协议或认证**。继续使用系统 `git` 子进程，并把它收敛在 `internal/core/git` 一个边界内。只有在“没有 git 二进制”或“实测 spawn 成本成为唯一瓶颈”时，才评估 **go-git / gitoxide / libgit2** 这类库作为可替换 backend；仍然不建议从零实现 Git。
>
> **代价**：运行环境必须有 git；spawn 有固定成本；无法在纯 WASM/无进程环境直接运行。这些是明确的边界，不是隐藏缺陷。

---

## 背景

ngm 的差异化在 **模块级可证明、lock、vendor、集合治理**，不在 Git 本身。当前所有 Git 访问都经 `internal/core/git`：

- `ls-remote` / `rev-parse` / `cat-file` / `ls-tree` / `for-each-ref`；
- mirror clone/fetch；
- partial clone 的 `--filter=blob:none` 与 promisor 行为；
- credential/secret/policy 门禁。

“自己实现 Git”听起来能减少依赖，但它会把项目拖进一个完全不同量级的工程。

## 决策

1. **不自研 Git 协议/对象库**：不实现 packfile/delta、protocol v2、smart HTTP/SSH、credential helper、filter、shallow、SHA-1/SHA-256 迁移。
2. **子进程是默认 backend**：`internal/core/git` 是唯一运行时 Git 网关；其他包不得直接 `exec.Command("git", ...)`。
3. **测试可以直连 git**：fixture 构造、`git http-backend` 等测试代码不受此限；判据只约束非测试运行时代码。
4. **未来只换 backend，不重写 Git**：若确需嵌入/无 git 环境，优先评估 go-git / gitoxide / libgit2，通过接口适配；适配器必须通过同一组 conformance fixtures。
5. **重新评估的触发条件**（满足任一才立项）：
   - 目标环境没有可用的 `git`（WASM、移动端、受限容器）；
   - 10k 模块基准中，批处理/缓存后 spawn 仍是主瓶颈；
   - 需要长期运行的服务化对象 API，子进程模型不再合适；
   - 多语言核心需要一个稳定的进程外协议，而不是每个语言绑定 Git 库。

## 为什么不自研

| 维度 | 自研 Git 的代价 |
|------|----------------|
| 正确性 | tree/blob/commit/tag、packfile、delta、refs、replace、alternates、shallow、filter 的组合语义 |
| 安全 | 解析不可信 packfile/对象是攻击面；一个越界读就是供应链工具的事故 |
| 传输 | smart HTTP、SSH、代理、credential helper、redirect、LFS 边界 |
| 兼容 | GitHub/Gitee/GitLab/自建、protocol v0/v1/v2、SHA-256 仓库 |
| 性能 | 自己实现的 pack 索引/增量/缓存通常不如 git；优化需要长期投入 |
| 维护 | 跟随 Git 生态演进，永远追版本 |
| 价值 | 不直接提升“模块证明/lock/集合”的产品价值 |

自研还会制造**第二个 Git 解析器**：一旦它与真实 Git 对同一对象给出不同结论，ngm 的“可证明”就会变成“两套实现谁对”的问题。

## 备选方案对比

| 方案 | 优点 | 缺点 | 结论 |
|------|------|------|------|
| 系统 `git` 子进程 | 完整、成熟、跨平台、认证/代理/filter 都现成 | 需要 git；spawn 成本 | **当前选择** |
| go-git | 纯 Go、易嵌入 | 协议/filter/边缘行为覆盖不如 git；性能与大仓支持需实测 | 未来 backend 候选 |
| gitoxide | Rust 实现、性能好 | FFI/构建/跨平台复杂；API 稳定性 | 未来 backend 候选 |
| libgit2 | 成熟 C 库 | CGO、部署、跨平台构建 | 未来 backend 候选 |
| 自研 | 完全可控 | 成本/风险/维护不可接受 | **不做** |

## 边界与判据

- `internal/core/git` 是唯一运行时 Git 网关；
- 其他非测试 Go 文件不得出现 `exec.Command("git", ...)` / `exec.CommandContext(..., "git", ...)`；
- `ngm doctor` 报告 git 可执行文件与版本；
- 任何未来 backend 必须通过现有 frozen vectors 与 mirror/partial-clone 行为测试。

## 后果

- 运行 ngm 的机器必须有 git；这是产品前提，不是可选项。
- Windows/Linux 的 git 版本差异由 `doctor` 与 CI 覆盖。
- 如果未来需要“无 git 嵌入”，先做 backend 接口与 conformance，不要直接写 Git。
- `modproof` 也遵守同一决定：它只接收 `Repo.Path`，不实现 fetch/对象解析。

## 相关文档

- [Git 后端边界](../architecture/git-backend.md)
- [语言无关核心](../architecture/core-and-adapters.md)
- [ADR-002：Git 直连](./adr-002-git-direct.md)
- [ADR-034：module cache](./adr-034-module-cache.md)
