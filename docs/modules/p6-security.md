# P6 — 安全模型（非核心模块）

> 权限管理、sandbox、凭证隔离

---

## 状态

**v0.3 部分实现。** 权限模型（default-deny）已落地并施加；sandbox 与凭证管理仍待实现。
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
| 权限模型（default-deny） | `internal/security/permissions.go`；施加点见[安全模型](../architecture/security-model.md) | **v0.3 已实现**（`run:<引擎>` 待施加） |
| `ngm verify --sandbox`（Deno 沙箱） | core Go 决策，Deno 执行 | planned v0.3（需 ADR-012） |
| 凭证管理（ssh-agent / credential helper） | 见[安全模型](../architecture/security-model.md)；v0.1 起已落地的保证见该页 | v0.1 部分 / v0.3 续 |

---

## 相关文档

- [架构：安全模型](../architecture/security-model.md)
- [架构：运行时模型](../architecture/runtime-model.md)
- [P3 — 供应链防护](./p3-supplychain.md)
- [ADR-007：Node 还是 Deno？](../adr/adr-007-runtime-node-deno.md)
