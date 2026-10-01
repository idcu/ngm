# P8 — 模块与里程碑映射

> 路线图的唯一事实源是 [internals/roadmap.md](../internals/roadmap.md)；本页只维护"模块 ↔ 版本"映射，不重复路线图内容。

---

## 模块 ↔ 版本

| 模块 | v0.1 | v0.2 | v0.3 | v0.4 |
|------|------|------|------|------|
| P0 ngm core（CLI / 配置 / lock / vendor / verify） | ✓ | | | 配置字段接线断言（`field_wiring_test.go`）/ `types` 接线、`vendor.commit` 与死类型 `EngineRef` 删除 |
| P1 依赖管理（解析 / 依赖图 / 冲突检测 / mappings） | ✓ | | | |
| P2 构建引擎（adapter） | esbuild | tsc / deno / postcss | ✓ wasm；remote **已排除**（ADR-013） | ✓ `ngm typedecl` 给 `typeDecl` 一个入口（真 tsc 验收） |
| P3 供应链防护 | verify | OSV / 策略 / audit | | ✓ `verify --signatures` / `--require-signed`（ADR-014） |
| P4 协议（adapter / mappings） | adapter 协议 + mappings v1 | | mappings 子路径扩展（可选 `path` 字段，版本号不变） | |
| P5 外部集成 | | | ✓ Vite / esbuild / Deno / Webpack 脚手架 + tsconfig paths | |
| P6 安全模型 | | | sandbox / 权限；凭证只隔离不代管 | |
| P7 可观测性 | | why / tree / outdated | | |

---

## 退出标准与范围收缩

见 [internals/roadmap.md](../internals/roadmap.md) 的 v0.1 退出标准、里程碑依赖关系与"后续探索"。

---

## 相关文档

- [路线图（internals）](../internals/roadmap.md)
- [能力矩阵（internals）](../internals/capability-matrix.md)
- [P0 — ngm core](./p0-core.md)