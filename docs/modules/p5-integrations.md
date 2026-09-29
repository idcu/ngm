# P5 — 外部工具集成

> 构建工具集成脚手架：Vite / esbuild / Deno / Webpack 消费 mappings

---

## 状态

**v0.3 规划。** P5 是 [P4 协议](./p4-ecosystem.md)的消费层：ngm core 只做依赖证明，集成通过 mappings 实现（能力归属见[能力矩阵](../internals/capability-matrix.md)）。

---

## 职责

让主流构建工具能直接消费 `ngm.mappings.json`，并提供集成脚手架命令。

---

## 模块结构

```
internal/integrations/
├── vite.go          # Vite 集成
├── esbuild.go       # esbuild 集成
├── deno.go          # Deno 集成
└── webpack.go       # Webpack 集成（未来）

cmd/ngm/
└── integrations.go  # ngm integrations
```

（完整模块树见 [P0 — ngm core](./p0-core.md)。）

---

## 集成清单

| 工具 | 消费方式 | 版本 |
|------|---------|------|
| Vite | `resolve.alias` 读取 mappings | v0.3 |
| esbuild | `alias` 读取 mappings | v0.3 |
| Deno | 写入 `deno.json` import map | v0.3 |
| Webpack | `resolve.alias` 读取 mappings | v0.3 |

用户侧接入示例见[构建指南](../guides/build.md)；mappings schema 见 [P4](./p4-ecosystem.md)。

---

## 集成命令

```bash
ngm integrations add vite
ngm integrations add esbuild
ngm integrations add deno
```

生成对应的配置片段（不覆盖已有配置，冲突时报错）。

---

## 诚实说明

1. **mappings 是一层间接性**：增加复杂度，但必要
2. **集成是薄层**：ngm 不追求插件生态数量
3. **构建工具的演进风险**：各工具 alias / import map 机制变化时需要跟进

---

## 相关文档

- [P4 — 生态与协议](./p4-ecosystem.md)
- [架构：引擎 adapter](../architecture/engine-adapter.md)
- [构建指南](../guides/build.md)