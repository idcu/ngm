# P3 — 供应链防护

> verify + audit + OSV.dev + 策略引擎

---

## 职责

P3 是 ngm 的差异化核心：把 refType + commit + archiveDigest + vendor + OSV 收束为同一默认流程。

---

## 模块结构

```
internal/supplychain/
├── policy.go            # 策略引擎
├── osv.go               # OSV.dev 查询
├── minimumage.go        # minimumReleaseAge
├── allowlist.go         # 白名单
├── postinstall.go       # postinstall 策略
└── report.go            # audit 报告

cmd/ngm/
├── verify.go            # ngm verify 入口
└── audit.go             # ngm audit 入口
```

（完整模块树见 [P0 — ngm core](./p0-core.md)；`ngm outdated` 属 P7。）

---

## verify

### 流程

```
读取 ngm.lock
   │
   ▼
对每个依赖：
   ├── 解析当前 refType → 当前 commit
   ├── 对比 lock 中的 commit
   │     ├── 匹配 → ✓
   │     └── 不匹配 → 检查性质
   │           ├── branch 前进 → ⚠ 预期更新
   │           ├── tag 重打 → ✗ 非预期漂移
   │           └── commit 改写 → ✗ 严重
   └── 对比 archiveDigest
         ├── 匹配 → ✓
         └── 不匹配 → ✗ 严重供应链事件
```

### 退出码

| 退出码 | 含义 | CI 行为 |
|--------|------|---------|
| `0` | 全部匹配，或仅有"预期更新"（branch 前进） | 继续（`--strict` 时预期更新升级为 1） |
| `1` | 非预期漂移（tag 重打 / commit 改写） | 阻断（`--allow-drift` 可降级为 0） |
| `2` | digest 重放不匹配 | 必须阻断 |
| `3` | 配置/策略错误 | 阻断 |
| `4` | Git 网络/操作失败（含 `--offline` 资源缺失） | 阻断 |

区分"预期更新"与"非预期漂移"不能只靠文本：`ngm verify --json` 输出 `driftKind`（`expected` / `unexpected` / `critical`）供 CI 判断。

### 实现

```go
func Verify(graph *DependencyGraph) (*VerifyReport, error) {
    report := &VerifyReport{}
    for _, dep := range graph.Nodes {
        lock := dep.LockEntry

        // 重新解析 refType
        current, err := ResolveRef(dep.Name, dep.Ref, dep.RefType)
        if err != nil {
            report.Errors = append(report.Errors, err)
            continue
        }

        // 对比 commit
        if current != lock.Commit {
            drift := classifyDrift(dep)
            report.Drifts = append(report.Drifts, drift)
        }

        // 对比 digest：从 mirror 的同一 commit 重建清单并重算（本地重放，可离线）
        manifest := BuildManifest(dep.Commit) // 见 ADR-008
        digest := computeDigest(manifest)
        if digest != lock.ArchiveDigest {
            report.Critical = append(report.Critical, &DigestMismatch{
                Dep:    dep,
                Locked: lock.ArchiveDigest,
                Actual: digest,
            })
        }
    }
    return report, nil
}
```

### drift 分类

> **分类依据是祖先关系，不是 refType。** 下面这段伪代码曾经按 refType 直接映射，
> 但那样无法处理"branch 被 force push（历史被改写）"——从外部现象看，它和"branch 前进"一模一样。
> 唯一事实源是[可观测性 · 「预期更新」 vs 「非预期漂移」](../architecture/observability.md)，
> 实现见 `internal/verify`（用 `git merge-base --is-ancestor` 判定）：

```
branch，且旧 commit 是新 commit 的祖先   → expected    （branch 前进）
branch，但祖先关系不成立                 → unexpected  （历史被改写）
tag / commit 的 ref 指向了别的 commit    → unexpected  （tag 重打 / ref 被删除或改名）
digest 重放不匹配                       → critical    （不可被降级为 expected）
无法证明祖先关系（对象缺失等）            → unexpected  （保守：不能让 force push 悄悄过关）
```

`DriftExpected` 不计入非零退出码（`--strict` 时除外），通过文本输出与 `--json` 的 `driftKind` 报告，见[信任模型](../architecture/trust-model.md)。

---

## audit

### OSV.dev 查询

```go
func QueryOSV(commit string, ecosystem string) ([]Vulnerability, error) {
    body := map[string]string{
        "commit":    commit,
        "ecosystem": ecosystem,
    }
    resp, err := http.Post(
        "https://api.osv.dev/v1/query",
        "application/json",
        jsonBody(body),
    )
    // 解析 vulns
}
```

### 缓存

OSV 响应缓存到 `~/.ngm/cache/osv/`：

```
~/.ngm/cache/osv/
├── abc123def.json     # commit hash 作为文件名
├── def456abc.json
└── 123456789.json
```

缓存策略：

- 默认缓存 24 小时
- `--no-cache` 强制刷新
- 离线模式只读缓存

### 报告

```go
type AuditReport struct {
    GeneratedAt time.Time
    TotalDeps   int
    Vulnerabilities []Vulnerability
    Summary     struct {
        Critical int
        High     int
        Medium   int
        Low      int
    }
}

func (r *AuditReport) Format() string {
    var sb strings.Builder
    sb.WriteString("ngm audit report\n")
    sb.WriteString(strings.Repeat("=", 40) + "\n\n")
    sb.WriteString(fmt.Sprintf("Dependencies: %d\n", r.TotalDeps))
    sb.WriteString(fmt.Sprintf("Vulnerabilities: %d\n\n", len(r.Vulnerabilities)))

    for _, v := range r.Vulnerabilities {
        sb.WriteString(fmt.Sprintf("✗ %s@%s → %s\n", v.Dep, v.Ref, v.Commit[:7]))
        sb.WriteString(fmt.Sprintf("  %s: %s (%s)\n", v.Severity, v.Summary, v.ID))
        if v.FixedIn != "" {
            sb.WriteString(fmt.Sprintf("  Fixed in: %s\n", v.FixedIn))
        }
        sb.WriteString("\n")
    }
    return sb.String()
}
```

---

## minimumReleaseAge

### 语义

新发布的代码必须"晾晒"一段时间才能进入项目。

### 实现

```go
func CheckMinimumAge(dep *Dependency, policy time.Duration) error {
    // 上游时间：commit object 的 committer date（本地 mirror 读取，离线可用）
    committedAt, err := ReadCommitTime(dep.LockEntry.Commit)
    if err != nil {
        return err
    }

    age := time.Since(committedAt)
    if age < policy {
        return fmt.Errorf(
            "dependency %s committed %v ago, minimum age is %v",
            dep.Name, age, policy,
        )
    }
    return nil
}
```

### 配置

```json
{
  "supplyChain": {
    "minimumReleaseAge": "P3D"
  }
}
```

ISO 8601 duration：`P3D` = 3 天，`PT12H` = 12 小时，`P1W` = 1 周。

---

## 白名单

### allowedGitHosts

```go
func CheckAllowedHost(host string, allowed []string) error {
    for _, h := range allowed {
        if h == host { return nil }
    }
    return fmt.Errorf("host %q not in allowlist: %v", host, allowed)
}
```

### allowlistRepos

```go
func CheckAllowedRepo(repo string, patterns []string) error {
    for _, p := range patterns {
        if matchGlob(repo, p) { return nil }
    }
    return fmt.Errorf("repo %q not in allowlist", repo)
}
```

支持 glob：`github.com/my-org/*`。

---

## postinstall 策略

```go
type PostInstallPolicy string

const (
    PostInstallDeny    PostInstallPolicy = "deny"
    PostInstallPrompt  PostInstallPolicy = "prompt"
    PostInstallAllow   PostInstallPolicy = "allow"
)
```

### 三档的实际行为（v0.3，ADR-009 决策 5）

| 取值 | 行为 |
|------|------|
| `deny`（默认） | 不执行；有钩子时明说它们没有被执行 |
| `prompt` | 不执行——ngm 是**非交互**工具，"询问用户"没有真实形态 |
| `allow` | 在 **Deno 沙箱内**执行 `postinstall.js`；失败即 `exit 2` |

### 为什么入口这么窄

实现里**没有** `exec.Command(scripts.PostInstall)` 这种形态，这是刻意的：

- 只执行 `postinstall.js`——它能在沙箱里被约束；
- `package.json` 的 `scripts.postinstall` **检测到但不执行**：实测派生的 shell
  **不受 Deno 权限约束**（[ADR-012](../adr/adr-012-sandbox.md)），执行它等于把沙箱一次性绕开；
- 沙箱内**无写权限**：vendor 是可证明的，能改写它就会让 digest 失效——
  因此"需要写文件的钩子"（编译原生模块等）**不被支持**；
- 有钩子要跑而缺 Deno → `exit 5`，不降级为非沙箱执行。

权限沿用 [安全模型](../architecture/security-model.md) 的同一套词表：读该依赖自己的子树、
`net:` / `env:` 需显式授权、**从不派生子进程**。

Deno 的 default-deny 哲学：默认不让依赖跑代码。

---

## 成熟度

| 子模块 | 成熟度 | 备注 |
|--------|--------|------|
| verify | done (v0.1) | **核心差异化** |
| audit | done (v0.2) | OSV.dev 集成；按 commit 查询 + 24h 缓存 |
| minimumReleaseAge | done (v0.2) | 时间源为 committer date（见下） |
| 白名单 | done (v0.2) | 对传递依赖生效；解析阶段判定 |
| postinstall 执行入口 | **done (v0.3)** | 沙箱内、仅 `postinstall.js`；npm 风格 shell 钩子检测到不执行（见 ADR-009 决策 5/5a） |
| postinstall 策略 | **done (v0.3)** | `deny`（默认）/ `prompt` 不执行并明说；`allow` 在沙箱内执行 |
| 策略引擎 | done (v0.2) | JSON-first；`allowedGitHosts` / `allowlistRepos` / `minimumReleaseAge` / `osvIgnoreSeverities` / `verifyOnLock` 均已生效 |

> 成熟度口径与唯一事实源[能力矩阵](../internals/capability-matrix.md)一致。

---

## 相关文档

- [P0 — ngm core](./p0-core.md)
- [P1 — 依赖管理](./p1-dependency.md)
- [P2 — 构建引擎](./p2-build.md)
- [架构：信任模型](../architecture/trust-model.md)
- [架构：供应链防护](../architecture/supply-chain.md)
- [架构：安全模型](../architecture/security-model.md)
