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
    },
    {
      "from": "github:my-org/monorepo",
      "path": "packages/core",
      "to": "./ngm.vendor/github.com/my-org/monorepo/packages/core",
      "main": "./src/index.ts"
    }
  ]
}
```

### 先算对"导入标识符"

每种工具的配置都以**导入标识符**为键，而它不等于 `from`：`path` 存在时要拼上去。

```js
// 每个消费方都需要这一行——它是 mappings → 工具配置的桥
const specifier = m => (m.path ? `${m.from}/${m.path}` : m.from)
```

| `path` | 导入标识符 |
|--------|-----------|
| 缺失 | `github:my-org/utils` |
| `packages/core` | `github:my-org/monorepo/packages/core` |

> **为什么键必须是"标识符"而不是 `from`**：同一个仓库的多个子路径**共用同一个 `from`**，
> 而每个条目的 `to` 指向**各自子目录**。因此 `Object.fromEntries(m => [m.from, m.to])`
> 这类写法在 monorepo 上是有损的：它只留下其中一行，而留下的 `to` 是**子目录**——
> 实测（esbuild）会得到
> `Could not resolve "./vendor/.../packages/core/packages/core"`。
> 用标识符当键则在任何情况下都无歧义。

### Vite 集成

```ts
// vite.config.ts
import mappings from './ngm.mappings.json'

const specifier = (m: { from: string; path?: string }) =>
  m.path ? `${m.from}/${m.path}` : m.from

export default {
  resolve: {
    alias: [...mappings.mappings]
      // 长标识符优先：字符串 alias 是**前缀**匹配，短键会吞掉子路径
      .sort((a, b) => specifier(b).length - specifier(a).length)
      .map(m => ({ find: specifier(m), replacement: m.to }))
  }
}
```

### esbuild 集成

```js
// build.js
import mappings from './ngm.mappings.json'
import { build } from 'esbuild'

const specifier = m => (m.path ? `${m.from}/${m.path}` : m.from)

build({
  entryPoints: ['src/index.ts'],
  alias: Object.fromEntries(
    [...mappings.mappings]
      // 同 Vite：esbuild 的 alias 也是**前缀替换**（实测：只给
      // `github:demo/lib` 也能解析 `github:demo/lib/packages/core`），
      // 所以短的键必须排在长的之后，否则子路径会被吞掉。
      .sort((a, b) => specifier(b).length - specifier(a).length)
      .map(m => [specifier(m), m.to])
  ),
  outfile: 'dist/index.js',
  bundle: true
})
```

> **可以更省事的写法**：vendor 布局与仓库布局一致，因此把 `from` 直接指向
> **依赖根**（而不是某个子目录）时，前缀替换会让所有子路径自动落到位
> （实测可行）。但这要求从 `to` 里剥掉 `path` 才能得到"依赖根"——
> 那正是 `path` 想替你省掉的推断。用上面的标识符键不需要任何推断。

### Deno 集成

```json
// deno.json
{
  "imports": {
    "github:my-org/utils": "./ngm.vendor/github.com/my-org/utils/index.ts",
    "github:my-org/monorepo/packages/core": "./ngm.vendor/github.com/my-org/monorepo/packages/core/src/index.ts"
  }
}
```

> Deno 的 import map 支持**精确**键与**前缀**键（尾部带 `/`）。
> 子路径依赖用精确键最稳；若某仓库子路径很多，可用前缀键
> `"github:my-org/monorepo/": "./ngm.vendor/github.com/my-org/monorepo/"` 一并覆盖。

### TypeScript 类型解析

TS 语言服务不识别 `github:` 前缀：需要在 `tsconfig.json` 的 `paths` 中映射到 vendor 路径，
否则类型检查会报"找不到模块"（`ngm integrations add` 将一并生成，v0.3）。

```json
{
  "compilerOptions": {
    "moduleResolution": "bundler",
    "paths": {
      "github:my-org/utils": ["./ngm.vendor/github.com/my-org/utils/index.ts"],
      "github:my-org/monorepo": ["./ngm.vendor/github.com/my-org/monorepo/packages/core/src/index.ts"],
      "github:my-org/monorepo/*": ["./ngm.vendor/github.com/my-org/monorepo/*"]
    }
  }
}
```

> 带 `*` 的那条是**通配**：它使子路径（如 `github:my-org/monorepo/packages/web`）
> 无需逐条声明即可解析——已用真实 tsc 验证（导入根与子路径都不会报"找不到模块"，
> 且故意制造的类型不匹配会指向 vendor 里的真实类型，证明解析确实生效）。
>
> **不要写 `baseUrl`**：TypeScript 7 已移除该选项，写上去会直接报
> `error TS5102: Option 'baseUrl' has been removed`；`paths` 单独就能工作。

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
