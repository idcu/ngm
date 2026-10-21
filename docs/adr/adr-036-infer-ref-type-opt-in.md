# ADR-036：`--infer-ref-type` 是显式 opt-in 的远端证据推断

- **状态**：已定
- **日期**：2026-10-20
- **范围**：`ngm add`
- **上游**：[ADR-004](./adr-004-reftype.md)、[ADR-035](./adr-035-refType-narrow-default.md)

> **结论**：`ngm add` 在未给 `--ref-type`、且 `ref` 不是完整 40 位 commit 时，仍然报错；用户可显式传 `--infer-ref-type`，让 ngm 做一次 `ls-remote`，用远端证据二选一。ref 同时是 tag 和 branch、或都不存在时，必须失败，不允许回退到“看起来像”。
>
> **代价**：`add` 从“默认不触网”变成“有一个显式触网开关”；该开关必须走全局权限门禁，并在输出里说明推断结果。

---

## 背景

ADR-035 只放行了“完整 40 位 hash”的窄默认，因为那种情况语法上无歧义。tag/branch 仍然必填，但实际使用中，用户经常只知道 ref 名字，不知道它是 tag 还是 branch；手动查一次远端再回来补 flag 很啰嗦。

## 决策

1. `--infer-ref-type` 是布尔开关，默认关闭；
2. 只在 `--ref-type` 缺失且 `ref` 非完整 commit 时生效；
3. 推断只信远端精确 ref：
   - 只有 `refs/tags/<ref>` → `tag`；
   - 只有 `refs/heads/<ref>` → `branch`；
   - 两者都有 → 报歧义；
   - 都没有 → 报不存在；
4. 推断结果显式写进 `ngm.json`，并打印一行说明；
5. 推断走 `resolve.InferRefTypeFromRemote`，与所有 git 子进程同一条 `run:git` / `net` 门禁路径；
6. 不做隐式推断，不做“先试 tag 再试 branch”的回退。

## 后果

- 少打 flag，但不牺牲声明自描述：写进 `ngm.json` 的仍是显式 `refType`；
- 网络访问只在用户显式要求时发生；
- tag/branch 同名时不会静默选错；
- 修复了 `git.LSRemote` 的参数顺序问题：`<repository>` 必须在 `<patterns>` 之前，否则带 pattern 的 ls-remote 会把 pattern 当成仓库名。

## 相关文档

- [ADR-004：refType 声明](./adr-004-reftype.md)
- [ADR-035：refType 窄默认](./adr-035-refType-narrow-default.md)
- [依赖管理](../guides/dependency-management.md)
