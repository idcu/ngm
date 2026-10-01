# P6 — 安全模型（非核心模块）

> 权限管理、sandbox、凭证隔离

---

## 状态

**v0.3 已实现。** 权限模型（default-deny，`read:`/`write:`/`net:`/`run:`/`env:`）已落地并**真的施加**；
Deno 沙箱（`ngm verify --sandbox` / `postinstall` / `audit --hook`）已实现。
**凭证面只做隔离，不代管**——ngm 不读取、不缓存 token，认证交给 git 自己的 ssh-agent / credential helper，
这是**有意不做**，不是待办（见[架构：安全模型](../architecture/security-model.md) §token 与凭证管理）。
核心概念唯一维护在 [架构：安全模型](../architecture/security-model.md)；本页只维护模块结构。

---

## 职责

P6 实现 ngm 的权限管理与安全执行环境。

---

## 模块结构

```
internal/security/
├── permissions.go       # 权限模型
├── sandbox.go           # sandbox 模式（Deno）
├── credentials.go       # Git 凭证管理
└── capabilities.go      # capability 映射
```

（完整模块树见 [P0 — ngm core](./p0-core.md)。）

---

## 实现要点

| 能力 | 说明 | 版本 |
|------|------|------|
| 权限模型（default-deny） | `internal/security/permissions.go`；施加点见[安全模型](../architecture/security-model.md) | **v0.3 已实现** |
| `ngm verify --sandbox`（Deno 沙箱） | `internal/security/sandbox.go` + `cmd/ngm/verify_sandbox.go`；决策见 [ADR-012](../adr/adr-012-sandbox.md) | **v0.3 已实现**（依赖 `verify.js`） |
| 凭证隔离（ssh-agent / credential helper） | ngm **不代管**凭证：只做透传、输出脱敏、仓库定位变量剔除与 `env:` 剔除；认证交给 git 自己的配置（见[安全模型](../architecture/security-model.md) §token 与凭证管理） | **v0.1 起，且有意不做"凭证管理"** |

---

## 相关文档

- [架构：安全模型](../architecture/security-model.md)
- [架构：运行时模型](../architecture/runtime-model.md)
- [P3 — 供应链防护](./p3-supplychain.md)
- [ADR-007：Node 还是 Deno？](../adr/adr-007-runtime-node-deno.md)
