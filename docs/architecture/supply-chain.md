# 供应链防护

> **结论**：所有防护共享同一依赖图和策略状态，并且**基于锁定 commit 做审计**，不基于 tag/branch。
> "版本没变、commit 变了"正是投毒的常见形态。空 `supplyChain` 段不门禁——底线是不对没配置的人突然变红。

---

## 数据流与状态

```
lock（commit + digest） + 策略（ngm.json.supplyChain） + 本地 mirror
        │
        ├─ minimumReleaseAge：当前时间 - commit 的 committer date
        ├─ allowlist / allowedGitHosts：对根与传递依赖同样生效
        ├─ OSV：按 commit 查询，结果缓存 24h（--offline 只读缓存）
        ├─ postInstallPolicy：控制依赖脚本是否运行（deny 默认）
        └─ verifyOnLock：install/update 后自动复查（结论按 verify 自己的码返回）
        ▼
    同一份门禁状态 → 允许 / exit 1（超阈值）/ exit 3（策略错）/ exit 4（网络）
```

**边界**：时间源是 commit 的 committer date（从本地 mirror 的 Git 对象读，离线可用），不是 `resolvedAt`；
Git 没有可信时间戳，该机制防的是"快速跟随上游的自动投毒"，不是敌手刻意回填日期。

---

## 机制

| 机制 | 作用 | 失败/边界 |
|------|------|----------|
| `minimumReleaseAge` | 新 commit 晾晒后才进项目；写了 `supplyChain` 段但没写冷却 ⇒ 默认 P1D，`PT0S` 显式关闭、`P0D` 拒绝 | 紧急修复可临时调低或显式放行；见 ADR 汇总表 ADR-050、ADR-053 |
| `allowedGitHosts` / `allowlistRepos` | 限制可接受的 host 与 org/repo（支持 glob），防 typosquatting | 对传递依赖同样生效；维护白名单有成本 |
| `OSV.dev` | 按 lock 中的 commit 查已知漏洞 | 零日不在库中；多数公告按 semver 记录，对 commit 覆盖有限；查不到不报"安全" |
| `archiveDigest` | 规范化清单哈希，本地重放 | 不匹配 exit 2，禁止构建 |
| `postInstallPolicy` | 控制依赖脚本是否运行 | `deny`（默认）/ `prompt`（不执行，非交互语义）/ `allow`（仅 Deno 沙箱内） |
| `verifyOnLock` | 更新锁定时自动复查漂移 | 无默认值，只显式配置才执行；结论按 verify 的码返回，不折成 0 |

### minimumReleaseAge

```
当前时间 - commit committer date >= 阈值
```

```json
{ "supplyChain": { "minimumReleaseAge": "P3D" } }
```

ISO 8601 duration：`P3D` = 3 天，`PT12H` = 12 小时。ngm 不发明概念，只把这套门禁绑到 Git commit
而不是 registry version。安全修复需紧急引入时，临时调低阈值或经显式放行。

### 白名单

```json
{
  "supplyChain": {
    "allowedGitHosts": ["github.com", "gitee.com"],
    "allowlistRepos": ["github.com/my-org/*", "gitee.com/trusted/*"]
  }
}
```

### OSV 与 audit

按 commit 查询 OSV.dev，结果缓存 24 小时，支持离线只读缓存。`supplyChain.osvIgnoreSeverities` 过滤严重级别，
被过滤的严重度仍计入报告的 ignored 条数，不是"忽略"。

- 超阈值漏洞 → exit 1；配置/策略错误 → exit 3；OSV 网络失败且无缓存 → exit 4
- 每条发现项都要有一行"接下来做什么"：有修复版本写 `Fixed in: <版本>`；
  没有就给两条真实的路——读公告，或**明确**把严重度记进 `osvIgnoreSeverities`
- 详见[可观测性 · audit](./observability.md#ngm-audit)

### postInstallPolicy：执行入口很窄

`allow` 也**只执行依赖根目录的 `postinstall.js`**，且只在 Deno 沙箱内、无写权限（vendor 是可证明的，
能改写它的钩子会让 digest 失效）。`package.json` 的 `scripts.postinstall` 检测到但不执行：
实测派生的 shell 不受 Deno 权限约束，执行它等于把沙箱一次性绕开。有钩子要跑而缺 Deno → exit 5，
**不降级**为非沙箱执行；没有钩子要跑时不需要 Deno（见 ADR 汇总表 ADR-009、ADR-012、[安全模型](./security-model.md)）。

### verifyOnLock

`ngm install` / `ngm update` 完成后自动跑一遍复核，但它**不撤回**已写下的内容——只是不允许"写完了却说不出是否一致"。
典型场景：`ngm.lock` 已提交，上游把 tag 移到别的 commit；install 按 lock 装得对，但自己说不出"ref 已不再指向锁定的 commit"。
默认不开启，因为复核每个 ref 需要额外 spawn 与网络，不该替所有用户付这个代价。

---

## 审计产物

`sbom` / `attest` 只读 lock 导出 CycloneDX 1.7 / SPDX 2.3 与 in-toto / SLSA v1 的**未签名**语句，
确定性字节（无时间戳、无绝对路径）；签名交给用户/CI 的标准工具，ngm 不自签（见 ADR 汇总表 ADR-027、ADR-045）。

**过期 lock 默认拒绝导出**：`ngm.json` 与 lock 不一致时 exit 3、不写产物；显式放行才允许，且事实写 stderr
而不塞进产物字节以保确定性。此前只写 warning，导致签名了一份描述过期状态的语句（见 ADR 汇总表 ADR-046）。
构建回执是旁车文件、不进 lock，同输入同字节，不承诺跨工具可复现构建（见 ADR 汇总表 ADR-057）。

---

## 诚实说明

1. ngm 的防护不是独有的：pnpm / Deno / npm 都有类似机制
2. 差异化在流程闭环：refType + commit + digest + vendor + verify + OSV 共享同一状态模型
3. 白名单管理有持续成本；"审计通过"不等于安全
4. 若这些策略只是分别调用外部命令，ngm 就只是配置与编排层

## 相关文档

- [锁定机制](./locking.md) · [可观测性](./observability.md) · [安全模型](./security-model.md)
- [配置详解](../guides/configuration.md) · [ADR 汇总表](../adr/README.md)
