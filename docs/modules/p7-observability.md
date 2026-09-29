# P7 — 可观测性（非核心模块）

> ngm why / tree / outdated

---

## 状态

**v0.2 规划。** 命令语义、输出与退出码唯一维护在 [架构：可观测性](../architecture/observability.md)；本页只维护模块结构与实现要点。

---

## 职责

P7 提供依赖洞察：为什么装了、谁依赖谁、是否过时。

---

## 模块结构

```
internal/observability/
├── why.go         # ngm why
├── tree.go        # ngm tree
└── outdated.go    # ngm outdated
```

（audit 实现归 P3 的 `internal/supplychain`，verify 对比逻辑归 `internal/lock`；完整模块树见 [P0 — ngm core](./p0-core.md)。）

---

## 命令

命令语义与退出码唯一维护在[架构：可观测性](../architecture/observability.md)。

---

## 实现要点

### why

解析依赖图，找到从 root 到目标依赖的所有路径。

### tree

递归打印依赖树，标记漂移（`⚠`）和漏洞（`✗`）。

### outdated

- tag：查询 Git host API 最新 tag
- branch：fetch 最新 commit 对比
- commit：无更新

### audit

见 [P3 — 供应链防护](./p3-supplychain.md)。

### verify

见 [信任模型](../architecture/trust-model.md)；实现归 P0 的 `internal/lock`（对比逻辑）与 P3（CLI 入口）。

---

## 相关文档

- [P3 — 供应链防护](./p3-supplychain.md)
- [架构：可观测性](../architecture/observability.md)
- [架构：信任模型](../architecture/trust-model.md)
