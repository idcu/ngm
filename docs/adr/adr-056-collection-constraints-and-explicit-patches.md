# ADR-056：集合约束轻量版与显式补丁登记

- **状态**：已定（2026-11-24）
- **范围**：`ngm modules validate --strict`；`ngm.json` 的 `patches` 段；后续 lock 接线
- **依据**：[竞品全维度差距评估 2026-11-24 §6 P1](../analysis/ngm-competitive-gap-full-2026-11-24.md) 的「catalog / constraints 轻量版」与「安全覆盖/补丁机制」两行
- **判据**：V217（集合 ref 一致性）、V218（补丁登记）

## 结论

1. **约束只做一致性，不做版本求解。** 集合内多个模块声明同一 `name` 的依赖时，
   `ref` 必须一致；`ngm modules validate --strict` 不一致即报错并列出模块与各自
   ref。这里不判断哪个 ref "更新"、不尝试共存、不做 semver 求解——它等价于一
   条策略断言，而不是一个解析器。对应 pnpm catalogs / Yarn constraints 的机制，
   不照搬它们的版本语言。
2. **例外必须显式登记且受判据守。** 唯一允许的逃生口是 `ngm.collection.json`
   顶层 `constraints.ignoreSharedRefs`（依赖 name 数组）。名单里的 name 必须至少
   被一个模块声明；没有任何模块声明它时判为**过期豁免**（红）。于是"删掉所有
   声明把例外悄悄留下"这条路径不成立。
3. **补丁只换锚点，不改身份语义。** `ngm.json` 的 `patches` 数组把某依赖从 `from`
   锚点替换到 `to` 锚点。依赖身份仍是 `name`（仓库来源），补丁不创建新身份、
   不接受 semver 范围、不静默改写源文件。`why` 必填且必须写进 lock，让 verify
   读到 lock 时能区分"当前锚点来自补丁"与"原声明漂移"。

## 决策

### 1. 集合约束（`validateSharedDependencies`）

- 按依赖 **name** 汇总（不再带 path 拆分）：同一 name 的不同 ref 一律报到。
- 输出必须点名模块路径与各自 `ref (refType)`，便于从报告直接定位要改哪份
  `ngm.json`。
- 豁免只压制"分歧"这一种错误；过期豁免独立报错，不因名单非空而免责。
- 索引文件来源：显式 `--index=<file>` 优先，否则读项目根 `ngm.collection.json`；
  文件不存在视为无豁免，存在但 JSON 不合法按配置错误（exit 3）处理。
- 默认（非 `--strict`）仍只做发现，不阻止索引生成。

### 2. 补丁登记（`Patch`）

形状：

```json
"patches": [
  {"name":"gitee:org/repo","from":"v1.0.0","to":"v1.0.1","why":"上游 tag 撤回"}
]
```

判据（V218）：

- `why` 必填：没有理由的锚点替换等于隐式覆盖，拒绝。
- `to` 必须是合法锚点：完整 commit / tag / branch / `sha256:<64 hex>`（local-dir
  形态）；`^1.2.3`、`1.x`、`>=1.0.0`、URL、`../` 逃逸等裸范围或路径形态一律拒绝。
- 同一 `name` 只能有一条补丁；git 来源按 `resolve.ParseSlug` 归一化后比较，
  避免 `gitee:org/repo` 与 `gitee.com:org/repo` 绕过唯一性。
- `name` 必须仍解析为仓库来源（或 `local:` 命名空间，且用 `localfs.ValidateSourcePath`
  的规则拒绝 `..` 逃逸）。
- `from` 同样必须是合法锚点；补丁的语义是"从一个锚点换到另一个锚点"。

`Effective.Patches` 与 `Project.Patches` 不共享底层数组，和 SupplyChain 的
深拷贝纪律一致。

### 3. lock 留痕（本 ADR 的接线边界）

本次只落地配置校验层：补丁本身**不**修改任何源文件，也**不**在本次改动里
静默重写 `ngm.json` / `ngm.lock`。要完成"必须在 lock 留痕"，还需在以下位置接线：

- `cmd/ngm/env.go:toDepSpecs`：在转换为 `resolve.DepSpec` 时按 name 查找补丁，
  用 `to` 覆盖 `Ref`（并按 `to` 的形态确定 `RefType`），保留 Name/SubPath/来源字段；
  未登记的补丁不得生效。
- `internal/core/lock/schema.go:Dependency` 及 marshal：新增补丁溯源字段
  （`from` / `why`，例如 `appliedPatch`），字段顺序追加在末尾以免破坏既有字节契约。
- `cmd/ngm/install.go`（约 426、499 行）与 `cmd/ngm/update.go`（约 219、286 行）
  写 lock 条目时带上溯源；`cmd/ngm/verify.go:226` 读 lock 时把带溯源的条目识别为
  补丁生效，而不是"声明与 lock 不一致"。

在这些接线完成前，`patches` 只是**已校验的声明**：写了它不会改变解析结果，
也不会被 verify 当成漂移豁免。

## 后果

- 正面：集合层面的冲突前移到生产者 CI；补丁有可追查的理由，不再靠手改 lock
  或隐式 override。
- 代价：多了一层配置面需要文档与接线对账（V27 的文档反向检查、`wiredFields`
  条目），且 lock  schema 追加字段需要保持顺序稳定。
- 不做：不做多版本共存、不做 semver 覆盖、不做补丁文件内容的哈希验证；这些
  属于供应链与内容层，不在本 ADR 范围内。
