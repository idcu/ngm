# ADR-058：引擎绑定字段与 5/2 退出码语义

- **状态**：已定（2026-11-24）
- **范围**：`ngm.engines.json` 条目的 `expectedVersion` / `expectedDigest` / `supportsMinify`；`ngm engines validate` 与 `ngm css --minify` 的退出码
- **依据**：[生态路线 §3.4–3.5](../specs/ecosystem-roadmap-2026-11-24/spec.md) · [ADR-039 退出码优先级](./adr-039-exit-code-precedence.md) · [ADR-057 构建回执](./adr-057-build-receipt.md)
- **判据**：V226（CSS 压缩 fail-closed）、V227（绑定字段 → 5/2/0）

## 结论

1. **绑定是显式承诺，不是默认策略。** 三个字段都留空时完全维持旧行为：`version` 与实装不符仍只是信息（exit 0）；postcss 不带 `--minify` 的编译照旧。只有用户写下字段，ngm 才给出结论。
2. **版本不符 → 5，摘要不符 → 2，摘要压过版本。** `expectedVersion` 不符意味着"声明的这个版本这台机器上不可用"（5）；`expectedDigest` 不符意味着"工具字节与钉住的值不一致"（2，完整性失败）。两者同时发生时按 ADR-039 退 2，且优先级与问题列表顺序无关。
3. **CSS 压缩未声明支持 → 3。** `css --minify` 而当前引擎条目没有 `supportsMinify: true` 时退 3，hint 点名 esbuild / lightningcss 等显式引擎；不再打印一条 note 后仍以 0 结束。postcss 的非 `--minify` 路径与其插件/引擎警告不变。

## 字段形状

写在 `ngm.engines.json` 的 `engines[]` 条目上（schema 版本仍为 1）：

```json
{
  "version": 1,
  "engines": [
    {
      "name": "postcss",
      "kind": "css",
      "adapter": "subprocess",
      "command": "postcss",
      "version": "8.4.0",
      "expectedVersion": "8.4.0",
      "expectedDigest": "sha256:<64 hex>",
      "supportsMinify": true
    }
  ]
}
```

| 字段 | 类型 | 空/默认 | 写入后的判定 |
|------|------|---------|--------------|
| `expectedVersion` | string | 不核对 | 与探测版本不符（探测不到则退到 `version` 声明）→ `IssueVersionBinding` → 5 |
| `expectedDigest` | string | 不核对 | 与工具文件字节 sha256 不符 → `IssueDigestBinding` → 2（允许省略 `sha256:` 前缀） |
| `supportsMinify` | bool | false | `--minify` 未声明支持 → `ConfigInvalid` → 3 |

版本比较沿用清单既有的前缀口径（`0.24` 接受 `0.24.0`），但 `1.0` 不接受 `1.08`；
前导 `v` 忽略。摘要由 Go 标准库读取工具文件字节计算（subprocess 经 LookPath，wasm 用
相对项目根的路径），**不 spawn `sha256sum`** —— Windows 没有该工具，且 spawn 会绕过
`run:` 闸门。embed 引擎没有可绑定的工具字节，给 `expectedDigest` 时读不到文件，不构造
"摘要不符"结论。

## 为什么不复用 `version` 字段

`version` 是清单对实装的**声明**，既有语义是"不一致只报告、不阻断"（旧版本往往照常能跑，
validate 因此不该变成版本管理器）。把它升级成硬失败会破坏所有现有清单。新增
`expectedVersion` 把"我只是记一下装了什么"与"我要求必须是这个版本"分成两件事，
前者不动、后者才进判定链。

## 与回执/W4 的关系

- `expectedDigest` 可以消费 [ADR-057](./adr-057-build-receipt.md) 写下的 `engine.digest`，
  但两者独立：回执记"这次实际用了什么"，绑定声明"允许用什么"。回执缺失不阻止绑定核对。
- 绑定判定发生在 adapter 的清单校验与 CSS 的能力预检里，不依赖 `build --provenance`；
  W4 的 CI receipt mode 只是消费回执，不重复实现 5/2 语义。

## 相关文档

- [配置指南 · 引擎清单条目的绑定字段](../guides/configuration.md#引擎清单条目的绑定字段ngmenginesjson)
- [构建指南 · 压缩的确定性口径](../guides/build.md#压缩的确定性口径)
- [ADR-039：2 压过 4（已确证的篡改优先）](./adr-039-exit-code-precedence.md)
