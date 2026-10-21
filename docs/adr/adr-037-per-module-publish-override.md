# ADR-037：逐模块 publish 开关完全覆盖全局 --releases

- **状态**：已定
- **日期**：2026-10-21
- **范围**：`ngm modules index`、模块清单 `publish`
- **上游**：[ADR-031](./adr-031-module-collections.md)

> **结论**：模块清单的 `publish` 是**逐对象开关**。显式 `true`/`false` 完全优先，不再与全局 `--releases` 做 AND/OR；只有 `publish` 缺省（nil）时才跟随全局默认。全局开关因此是**默认值**，不是可以短路逐对象选择的“总闸”。
>
> **代价**：`--releases=false` 不能强制关掉一个显式 `publish:true` 的模块；要关掉它，必须改该模块的 `publish`。这是有意的：逐对象声明必须可被信任，否则“模块自己说发布”会被一个全局参数悄悄推翻。

---

## 背景

集合索引需要同时支持两种用法：

- 大多数模块跟随集合默认（发/不发 release）；
- 少数模块需要显式例外（例如实验模块不发布，或默认不发但某个模块要单独发布）。

如果把全局开关做成“总闸”，逐模块的 `publish:true` 会被 `--releases=false` 短路；用户无法用一个全局参数表达“默认关闭，但这几个例外打开”。

## 决策

1. 模块清单新增可选 `publish` boolean；
2. 解析优先级：
   - `publish` 存在 → 直接使用它的值；
   - `publish` 缺省 → 使用 `ngm modules index --releases` 的全局默认；
3. **不做 AND/OR 合并**：不存在“全局关了就一定关”的短路；
4. `publish` 只影响 release 索引生成，不影响安装、验证、vendor；
5. `--shards` 与 `publish` 组合沿用同一规则；
6. 没有逐对象开关时，行为与旧版完全一致（全局默认）。

## 后果

- `--releases=false` + 某模块 `publish:true` → 该模块仍生成 release；
- `--releases=true` + 某模块 `publish:false` → 该模块不生成 release；
- 要“全局强关”，必须把所有模块的 `publish` 设为 false（或删除 release tag/索引），不能靠全局参数；
- 文档必须明确这一点，避免用户以为 `--releases=false` 是 kill switch；
- `publish` 进入 `ProjectFile`，需要配置字段文档与反向判据。

## 相关文档

- [ADR-031：模块集合](./adr-031-module-collections.md)
- [配置详解](../guides/configuration.md)
- [模块集合架构](../architecture/module-collections.md)
