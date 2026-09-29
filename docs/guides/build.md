# 构建

> ngm 不做自研构建引擎。所有构建通过 engine adapter 调用外部工具。

---

## 基本用法

```bash
# 使用默认引擎（ngm.json 中配置）
ngm build

# 指定引擎
ngm build --engine=esbuild
ngm build --engine=deno      # v0.3 的能力；v0.1 没有内置 deno 引擎，现在会失败（见「其它引擎」）

# 指定入口
ngm build src/index.ts --engine=esbuild

# 生产构建
ngm build --production --engine=esbuild
```

---

## 引擎选择

### esbuild（v0.1 唯一适配的引擎）

```json
{
  "engines": {
    "transform": "esbuild",
    "bundle": "esbuild"
  }
}
```

```bash
ngm build --engine=esbuild
```

adapter 调用 `esbuild` CLI。`ngm init` 生成的模板已包含这段配置，因此 `ngm build` 无需参数。

### 其它引擎（v0.1 未适配）

`tsc` / `deno` / `postcss` 的 adapter 排在 v0.2–v0.3。在 v0.1 里**按内置名声明它们不会静默降级**——
你会得到明确的 `exit 5`（引擎不可用）与提示：内置清单里根本没有这些条目。

若你已经装好这些工具，可以**自行声明**一个 subprocess 引擎（协议见 [P4](../modules/p4-ecosystem.md)）——
此时执行的是你给的命令行，与内置清单无关：

```json
{
  "version": 1,
  "engines": [
    {"name": "typescript", "kind": "typeCheck", "adapter": "subprocess",
     "command": "tsc --noEmit", "defaultOptions": {}},
    {"name": "postcss", "kind": "css", "adapter": "subprocess",
     "command": "postcss", "defaultOptions": {}}
  ]
}
```

```bash
ngm typecheck --engine=typescript   # 需要你已声明上面的条目
ngm css src/app.css --engine=postcss --outfile=dist/app.css
```

### esbuild 不做类型检查

这是一个容易踩的坑：`ngm build` 通过（甚至很顺利）**不代表类型是干净的**——
esbuild 只删类型标注。把 esbuild 声明为 `typeCheck` 引擎会被明确拒绝（`exit 5`）而不是"当作通过"：

```bash
$ ngm typecheck --engine=esbuild
EngineNotFound: typeCheck: esbuild: esbuild does not type-check: it only strips type annotations
  hint:  tsc / deno are not adapted in this build; declare a `typeCheck` engine in ngm.engines.json
```

### 先看清 ngm 会怎么调引擎

```bash
$ ngm build --dry-run
would bundle src/index.ts
✓ primary: esbuild (bundle, subprocess)
    status:  available (0.28.2)
    command: esbuild --bundle --format=esm --target=es2020 src/index.ts
    source:  built-in catalog
```

`--dry-run` 只打印、不执行，是排查"参数为什么没生效"最快的入口。

---

## mappings 文件

ngm 生成 `ngm.mappings.json`，供外部构建工具读取：

```json
{
  "version": 1,
  "mappings": [
    {
      "from": "github:my-org/utils",
      "to": "./ngm.vendor/github.com/my-org/utils",
      "main": "./index.js",
      "types": "./index.d.ts"
    }
  ]
}
```

### Vite 集成

```ts
// vite.config.ts
import mappings from './ngm.mappings.json'

export default {
  resolve: {
    alias: mappings.mappings.map(m => ({
      find: m.from,
      replacement: m.to
    }))
  }
}
```

### esbuild 集成

```js
// build.js
import mappings from './ngm.mappings.json'
import { build } from 'esbuild'

build({
  entryPoints: ['src/index.ts'],
  alias: Object.fromEntries(
    mappings.mappings.map(m => [m.from, m.to])
  ),
  outfile: 'dist/index.js',
  bundle: true
})
```

### Deno 集成

```json
// deno.json
{
  "imports": {
    "github:my-org/utils": "./ngm.vendor/github.com/my-org/utils/index.ts"
  }
}
```

### TypeScript 类型解析

TS 语言服务不识别 `github:` 前缀：需要在 `tsconfig.json` 的 `paths` 中映射到 vendor 路径，否则类型检查会报"找不到模块"（`ngm integrations add` 将一并生成，v0.3）。

---

## 不在 ngm 范围内

ngm 不做 HMR / Dev Server / 代码分割优化 / Source Map 完整链路 / 框架插件，全部交给 Vite 等专业工具；完整的能力归属矩阵见[能力矩阵](../internals/capability-matrix.md)。

---

## 诚实说明

1. **ngm build 是薄封装**：本质是 spawn 外部引擎，不比直接调 esbuild 更快
2. **ngm 的构建价值在依赖证明**：确保构建时用的 vendor 代码是可证明的
3. **ngm 不追求构建性能**：Bun/esbuild/Vite 已经是标杆

---

## 相关文档

- [配置详解](./configuration.md)
- [架构：引擎 adapter](../architecture/engine-adapter.md)
- [运行时模型](../architecture/runtime-model.md)
