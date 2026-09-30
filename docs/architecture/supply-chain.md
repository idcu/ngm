# 供应链防护

## 核心原则

**供应链防护基于锁定 commit 做审计，不基于 tag/branch。**

所有防护机制共享同一依赖图和策略状态。

---

## 防护机制

| 机制 | 作用 | 数据来源 |
|------|------|---------|
| `minimumReleaseAge` | 推迟新 commit 进入项目 | commit 的 committer date |
| 白名单 | 只允许白名单内的源/命名空间 | allowedGitHosts / allowlistRepos |
| OSV.dev | 已知漏洞扫描 | lock 中的 commit |
| `verify` | ref 漂移检测 | refType → commit 对比 |
| `archiveDigest` | 本地内容一致性（清单重放） | 规范化内容清单哈希（见 [ADR-008](../adr/adr-008-archive-digest.md)） |
| `postInstallPolicy` | 控制 build script 是否运行 | 策略配置 |
| `verifyOnLock` | 更新锁定时自动检查漂移 | 策略配置 |

---

## minimumReleaseAge

### 语义

新发布的代码必须"晾晒"一段时间才能进入项目，降低"上游刚发布即被自动引入"的快速投毒风险。

### 实现

```
当前时间 - commit 的上游时间 >= minimumReleaseAge
```

上游时间取 **commit object 的 committer date**（从本地 mirror 的 Git 对象读取，离线可用）。

### 配置

```json
{
  "supplyChain": {
    "minimumReleaseAge": "P3D"
  }
}
```

ISO 8601 duration 格式：`P3D` = 3 天，`PT12H` = 12 小时，`P1W` = 1 周。

### 时间源与局限（诚实说明）

- Git 没有可信时间戳：committer date 由仓库作者控制，可以被回填；该机制防的是"快速跟随上游的自动投毒"，不是敌手刻意伪造日期
- **不使用 resolvedAt（本地解析时间）**：否则新依赖首次安装必然失败，且防不住投毒窗口
- 安全修复需要紧急引入时：临时调低阈值，或经 allowlist 显式放行；`minimumReleaseAgeExclude` 列入后续规划
- 与 `resolvedAt` 的关系：resolvedAt 只用于审计"何时引入"，不参与门禁计算

### 同类方案参考

- pnpm：`minimumReleaseAge`（基于 registry 发布时间）
- Deno：`minimumDependencyAge`
- npm：`min-release-age`（npm 11.10+）

ngm 不发明概念，只把这套门禁**绑定到 Git commit 的 committer date，而不是 registry version**。

---

## 白名单

### allowedGitHosts

```json
{
  "supplyChain": {
    "allowedGitHosts": ["github.com", "gitee.com"]
  }
}
```

限制可接受的 Git 托管来源，防止 typosquatting 或恶意 host。

### allowlistRepos

```json
{
  "supplyChain": {
    "allowlistRepos": ["github.com/my-org/*", "gitee.com/trusted/*"]
  }
}
```

精确到 org/repo 的白名单。支持 glob 模式。

---

## OSV.dev 集成

### 查询方式

```
GET https://api.osv.dev/v1/query
{
  "commit": "abc123def4567890abcdef1234567890abcdef12",
  "ecosystem": "Go"  // 或 "npm" / "Maven" 等
}
```

### 结果处理

```json
{
  "vulns": [
    {
      "id": "GHSA-xxxx-xxxx-xxxx",
      "summary": "Prototype pollution in library X",
      "severity": "HIGH",
      "affected": [
        {
          "commit": ["abc123..."],
          "versions": ["< 1.2.3"]
        }
      ]
    }
  ]
}
```

### 告警策略

```json
{
  "supplyChain": {
    "osvIgnoreSeverities": ["LOW"]
  }
}
```

`ngm audit` 输出：

```
✗ github:org/utils@v1.2.3 (tag) → abc123def
  HIGH: Prototype pollution (GHSA-xxxx)
  修复建议：升级到 v1.2.4
```

---

## postInstallPolicy

```json
{
  "supplyChain": {
    "postInstallPolicy": "deny"
  }
}
```

| 值 | 行为 |
|----|------|
| `deny`（默认） | 不执行。有依赖声明钩子时**明说**它们没有被执行 |
| `prompt` | 不执行。ngm 是**非交互**工具（CI 里没有 TTY），"询问用户"没有真实形态——它的语义是"别自动执行，把决定权留在配置里显式表达" |
| `allow` | 在 **Deno 沙箱内**执行依赖的 `postinstall.js`；失败即 `exit 2` |

**执行入口很窄，这是刻意的**（[ADR-009](../adr/adr-009-supply-chain-policy.md) 决策 5 / 5a）：

- 只执行 `postinstall.js`（依赖根目录）——它能在沙箱里被约束；
- `package.json` 的 `scripts.postinstall` **检测到但不执行**：实测派生的 shell 不受 Deno 权限约束
  （[ADR-012](../adr/adr-012-sandbox.md)），执行它等于把沙箱一次性绕开；
- 沙箱内**没有写权限**：vendor 是可证明的，任何能改写它的钩子都会让 digest 失效。
  因此"需要写文件的钩子"（编译原生模块等）**不被支持**；
- 有钩子要跑而缺 Deno → `exit 5`，**不降级**为非沙箱执行；没有钩子要跑时不需要 Deno。

Deno 的 default-deny 哲学：默认不让依赖跑代码——**要跑也只在沙箱里跑**。

---

## verifyOnLock

```json
{
  "supplyChain": {
    "verifyOnLock": true
  }
}
```

**done (v0.2)**：`ngm install` / `ngm update` 完成后自动跑一遍 `ngm verify`。

两处刻意的设计（失败语义定义在 [ADR-009 决策 6](../adr/adr-009-supply-chain-policy.md)）：

- **结论按 `verify` 自己的退出码返回**（`1` 漂移 / `2` 完整性 / `4` 网络），**不折成 0**。
  "装好了，但锁定的 ref 已经指向别处"如果被报成成功，这条策略就没有存在的意义。
  自动复查**不撤回** install/update 已写下的内容——它只是不允许"写完了却说不出是否一致"。
- **无默认值，只有显式配置才执行**。verify 要重新解析每个 ref（100 依赖约 3 秒且需网络），
  默认替所有用户付这个代价不是好交易。

典型场景（也是它唯一无法被 install 自身覆盖的场景）：`ngm.lock` 已提交，上游把 tag 挪到了
另一个 commit。install 会正确地按 lock 安装，但它自己说不出"ref 已经不再指向你锁定的那个
commit"——自动复查补上这句。

---

## audit 命令

> **done (v0.2)**。`ngm audit` 已实现：按 lock 中的 **commit** 查询 OSV.dev（不是按版本——
> "版本没变、commit 变了"正是投毒的常见形态），结果缓存 24 小时，
> 支持 `--offline`（只读缓存）、`--no-cache`、`--json`。本节的报告格式与退出码即当前行为。
>
> 两点仍需说明：`supplyChain` 里只有 `osvIgnoreSeverities` 参与 audit（过滤严重级别），
> 其余字段的生效情况见[配置详解 · 成熟度](../guides/configuration.md)；而"审计通过"表示
> **库里没有关于这个 commit 的记录**，不等于安全——见下方覆盖局限。

```bash
ngm audit
```

输出：

```
ngm audit report (2026-09-29T10:00:00Z)
==========================================

Dependencies: 12
Vulnerabilities: 2

✗ github:org/utils@v1.2.3 (tag) → abc123def
  HIGH: Prototype pollution (GHSA-xxxx-xxxx)
  Fixed in: v1.2.4

⚠ github:org/logger@main (branch) → def456abc
  MEDIUM: ReDoS in parser (GHSA-yyyy-yyyy-yyyy)
  Fixed in: v2.0.1

✓ github:org/legacy@v0.9.0 (tag) → 123abc456
  No known vulnerabilities

---

Summary:
  2 vulnerabilities (1 HIGH, 1 MEDIUM)
  1 dependency with ref drift
```

### 退出码（规划）

`0` 无超阈值漏洞 / `1` 存在超阈值漏洞（经 `osvIgnoreSeverities` 过滤）/ `3` 策略或配置错误 /
`4` OSV 网络失败且无可用缓存。定义与全局约定唯一维护在[可观测性 · 退出码](./observability.md)。

---

## 诚实说明

1. **ngm 的防护不是独有的**：pnpm/Deno/npm 都有类似机制
2. **ngm 的差异化在流程闭环**：refType + commit + archiveDigest + vendor + verify + OSV 共享同一状态模型
3. **OSV.dev 不是万能的**：零日漏洞不在数据库中；且多数漏洞按 semver 版本记录，对 Git commit 的匹配覆盖率有限，未覆盖的 commit 无法给出结论
4. **白名单管理有成本**：需要持续维护 allowlistRepos

---

## 相关文档

- [信任模型](./trust-model.md)
- [锁定机制](./locking.md)
- [配置详解](../guides/configuration.md)
- [可观测性](./observability.md)
