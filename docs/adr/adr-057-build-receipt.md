# ADR-057：构建回执是旁车证据，不是可复现构建认证

- **状态**：已定（2026-11-24）
- **范围**：`ngm build --provenance` 写出的 `ngm.build.json`；`ngm verify --receipt` 的只读复验；`ngm build --cache` 的本地缓存解释
- **依据**：[生态路线 2026-11-24 §3.1–3.3](../specs/ecosystem-roadmap-2026-11-24/spec.md) · [ADR-044 独立生态边界](./adr-044-independent-ecosystem.md) · [ADR-039 退出码优先级](./adr-039-exit-code-precedence.md)
- **判据**：V223（回执确定性/旁车/引擎摘要/缺工具 hint）、V224（产物不符与缺工具分离）、V225（缓存键与 source）

## 结论

1. **回执不进 lock。** `ngm.build.json` 是项目根的旁车文件：lock 证明"依赖是什么"，回执证明"用哪个引擎、按什么参数、把哪些字节变成了产物"。产物每次构建都可能变，混进 lock 会破坏 lock 的字节确定性与既有判据；因此 `build --provenance` 前后 `ngm.lock` 必须逐字节不变。
2. **回执不写时间。** 顶层只有 `receiptVersion` / `inputs` / `engine` / `argv` / `mappingsDigest` / `output` / `source`，没有时间戳、没有绝对路径。`argv` 按键排序，输入按 path 排序；同输入两次构建产出同一份字节。`engine.digest` 是**工具文件字节**的 sha256（Go 标准库读取，不 spawn `sha256sum`），不是路径——路径换台机器就变了。需要时间由 CI 或签名层提供。
3. **回执不承诺可复现构建认证。** 它只回答"当前产物是否仍是回执记录的那串字节"：只读重算输出摘要、检查引擎工具是否还在且字节未变，**不重跑构建**。跨工具、跨版本、跨平台是否字节一致不在承诺内；回执不上传、不签名，签名仍走用户侧标准工具（[signing.md](../guides/signing.md)）。`--receipt` 不启动引擎，缺引擎 exit 5、产物不符 exit 2，两者同时按 ADR-039 由 2 压过 5。

## 决策细节

### 1. 字段来源

- `inputs`：入口文件的相对路径 + 字节摘要；摘要错了就是构建输入变了。
- `engine`：`name` / `kind` / `version` / `digest`。`version` 是清单声明或探测读数，仅作人读说明；真正参与复验的是 `digest`。
- `argv`：subprocess adapter 已按 map 键排序输出，回执沿用该顺序，不重新推导语义。
- `mappingsDigest`：mappings 文件的字节摘要；没有文件时用空串的固定摘要，让"有/无 mappings"也是一个明确取值。
- `output`：产物相对路径或 `<stdout>` + 字节摘要。
- `source`：`built` 或 `cache`，只在启用 `--cache` 时有意义；它不是证据真伪的判断，只是"这次有没有省掉引擎调用"的解释。

### 2. 缓存的边界

键 = 输入 digest 列表 + engine digest + argv + 平台三元组（GOOS/GOARCH/compiler）+ mappingsDigest。
只做本地 `~/.ngm/build-cache`（NGM_HOME 可覆盖），不上传、不共享、不远端命中。
读出的字节摘要与写入时记录的不符即判污染：失效并重建一次；重建后仍不符则 exit 2，绝不静默把错误产物交给调用方。

### 3. 与 W2/W4 的接口

- E8 的 `expectedDigest` 可以消费本回执写下的 `engine.digest` 字段，但绑定语义（版本不符 5 / 摘要不符 2）另立 ADR-058，不在本文决定。
- E14 的 CI `receipt` mode 只做"构建 → 按回执复验"两步；它不替用户签名，也不把回执当 SLSA 认证。
