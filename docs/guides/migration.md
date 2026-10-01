# 迁移指南

> ngm 只管理 Git 依赖。registry 包继续用 pnpm/npm/yarn。

---

## 从 npm/yarn/pnpm 迁移 Git 依赖

### 场景

你的 `package.json` 里有 Git 依赖：

```json
{
  "dependencies": {
    "express": "^4.21.0",
    "my-utils": "git+https://github.com/my-org/utils.git#v1.2.3"
  }
}
```

迁移后：

```json
// package.json（pnpm 继续管 registry 包）
{
  "dependencies": {
    "express": "^4.21.0"
  }
}
```

```json
// ngm.json（ngm 管 Git 依赖）
{
  "schemaVersion": 1,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "dependencies": [
    {
      "name": "github:my-org/utils",
      "ref": "v1.2.3",
      "refType": "tag"
    }
  ]
}
```

### 迁移步骤

```bash
# 1. 初始化 ngm
ngm init github.com/my-org/my-app --runtime=node

# 2. 逐个迁移 Git 依赖
ngm add github:my-org/utils@v1.2.3 --ref-type tag

# 3. 从 package.json 中移除 Git 依赖
#    保留 registry 依赖不动

# 4. 安装
ngm install

# 5. 生成 mappings
#    ngm.mappings.json 会自动生成

# 6. 配置构建工具读取 mappings（Vite/esbuild/Deno）

# 7. 测试构建
ngm build --engine=esbuild
```

---

## 从 lytd 迁移

### lytd 是什么

lytd 是 TypeScript 工具链（构建+测试+文档+Dev Server+LSP），用 TS 写。

### ngm 与 lytd 的关系

ngm **不替代 lytd 的所有能力**。迁移时分两类：

| lytd 能力 | ngm 的做法 |
|----------|-----------|
| 构建（bundle/transform） | engine adapter → esbuild（**内置**）；deno 已适配但**需自行声明** |
| 类型检查 | engine adapter → tsc / deno（**v0.2 起**） |
| CSS | engine adapter → postcss（**v0.2 起**） |
| 测试 | **交给 Vitest / Deno test** |
| 文档 | **交给 TypeDoc / Deno doc** |
| Dev Server / HMR | **交给 Vite / Deno** |
| LSP | **交给 tsserver / Deno LSP** |

### 迁移步骤

```bash
# 1. 用 ngm 管理 Git 依赖
ngm init github.com/my-org/my-app --runtime=node

# 2. 配置引擎 adapter
#    ngm.json 的 engines 字段指向 esbuild / tsc / postcss

# 3. 替换 lytd 构建命令
#    lytd build → ngm build --engine=esbuild
#    lytd typecheck → ngm typecheck --engine=typescript

# 4. 测试框架换 Vitest / Deno test
# 5. 文档换 TypeDoc / Deno doc
# 6. Dev Server 换 Vite / Deno
```

---

## mappings 协议

ngm 生成 `ngm.mappings.json`，把裸导入映射到 vendor 路径；迁移时把构建工具的 alias 配置改为读取 mappings 即可。

- 接入示例（Vite / esbuild）：[构建指南](./build.md)
- schema：[P4 — 生态与协议](../modules/p4-ecosystem.md)

---

## 诚实说明

1. **迁移有摩擦**：需要同时维护 package.json + ngm.json
2. **ngm 不是 drop-in 替换**：构建/测试/文档/Dev Server 需要换工具
3. **只推荐 Git 依赖多的项目迁移**：纯 registry 依赖的项目没必要用 ngm
4. **mappings 协议是桥接层**：增加了一层间接性

---

## 相关文档

- [依赖管理](./dependency-management.md)
- [构建](./build.md)
- [配置详解](./configuration.md)
- [Node vs Deno](./node-vs-deno.md)
