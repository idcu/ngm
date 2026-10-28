# Git 后端边界

> 本文是 [ADR-038](../adr/adr-038-no-own-git.md) 的活文档：ngm 如何使用 Git，以及为什么不自研 Git。

## 1. 边界

```text
cmd/ngm / internal/*
        │
        ▼
internal/core/git        ← 唯一运行时 Git 网关
        │
        ▼
系统 git 子进程           ← 默认 backend
```

- `internal/core/git` 负责：`ls-remote`、`rev-parse`、`ls-tree`、`cat-file`、`for-each-ref`、mirror clone/fetch、partial clone、签名查询；
- 其他非测试代码不得直接调用 `exec.Command("git", ...)`；
- 测试 fixture 可以直连 git（构造仓库、`git http-backend`）。

## 2. 为什么不自己实现 Git

自研 Git 意味着同时承担：

- packfile / delta / object 解析；
- protocol v0/v1/v2、smart HTTP/SSH、代理、credential helper；
- filter / shallow / alternates / replace；
- SHA-1 → SHA-256 兼容；
- 不可信对象的解析安全；
- 与 GitHub/Gitee/GitLab/自建的差异。

这些都不直接提升 ngm 的差异化（模块级 proof、lock、vendor、集合治理），却会引入一个长期维护的“第二套 Git”。

## 3. 何时重新评估

只有满足任一条件才立项：

1. 目标环境没有 git（WASM、移动端、受限容器）；
2. 10k 模块基准中，批处理/缓存后 spawn 仍是主瓶颈；
3. 需要长期运行的服务化对象 API；
4. 多语言核心需要稳定的进程外协议。

即便如此，优先选择 **go-git / gitoxide / libgit2** 作为 backend，并通过同一组 frozen vectors 与 partial-clone 行为测试；不要从零实现 Git。

## 4. 当前保证

- `ngm doctor` 报告 git 可执行文件与版本；
- `internal/core/git` 是所有运行时 Git 调用的唯一入口；
- 任何 backend 替换必须先通过：
  - archive/module digest frozen vectors；
  - mirror clone/fetch；
  - `--filter=blob:none` / promisor 行为；
  - ref/tag/branch 解析；
  - 权限与 secrets 脱敏。

## 5. 相关

- [ADR-038](../adr/adr-038-no-own-git.md)
- [ADR-002：Git 直连](../adr/adr-002-git-direct.md)
- [module cache](../adr/adr-034-module-cache.md)
- [语言无关核心](./core-and-adapters.md)
