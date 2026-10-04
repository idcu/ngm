# 构建

> ngm 不做自研构建引擎。所有构建通过 engine adapter 调用外部工具。

---

## 基本用法

```bash
# 使用默认引擎（ngm.json 中配置）
ngm build

# 指定引擎
ngm build --engine=esbuild
ngm build --engine=deno      # 已适配，但需在 ngm.engines.json 里自行声明（见「需要自行声明的引擎」）

# 指定入口
ngm build src/index.ts --engine=esbuild

# 生产构建
ngm build --production --engine=esbuild
```

---

## 引擎选择

内置清单**自带** `esbuild`（`bundle` + `transform`）、`typescript`（`typeCheck` + `typeDecl`）
与 `postcss`（`css`）；后两者是 `optional`，**没装不算清单问题**。
`deno` 与 wasm 模块**不进内置清单**，需自行声明（理由见下）。

### esbuild（内置的 bundle / transform 引擎）

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

### typescript（内置的 typeCheck / typeDecl 引擎，v0.2 起）

```bash
ngm typecheck --engine=typescript                    # 或在 ngm.json 里写 "typeCheck": "typescript"
ngm typedecl --outdir=dist/types --engine=typescript # .d.ts 声明（v0.4 起有命令入口）
```

`tsc` 未安装时是 `exit 5`（引擎不可用），**不是**"检查通过"。

### postcss（内置的 css 引擎，v0.2 起）

```bash
ngm css src/app.css --engine=postcss --outfile=dist/app.css
```

postcss **没有内建压缩**：`--minify` 会被明确告知忽略，而不是假装压缩过。

### 需要自行声明的引擎：deno / wasm

`deno` 不进内置清单有两个理由：它的 `bundle` 是自身 ≥ 2.4 的实验特性；且内置会抢掉
typeCheck 的默认顺序。wasm 模块则是**项目本地文件**，路径得由你给。两者都按
[P4 协议](../modules/p4-ecosystem.md)自行声明（示例见
[engine-adapter](../architecture/engine-adapter.md)）：

```json
{
  "version": 1,
  "engines": [
    {"name": "deno", "kind": "typeCheck", "adapter": "subprocess",
     "command": "deno check", "defaultOptions": {}},
    {"name": "deno", "kind": "bundle", "adapter": "subprocess",
     "command": "deno bundle", "defaultOptions": {}}
  ]
}
```

```bash
ngm typecheck --engine=deno   # 需要你已声明上面的条目
ngm build --engine=deno
```

**按内置名声明不会静默降级**：清单里的名字对不上本 build 能做的事时，你会得到明确的
`exit 5`（可用性）或 `exit 3`（清单结构），而不是一个"看起来通过"的结果。

### esbuild 不做类型检查

这是一个容易踩的坑：`ngm build` 通过（甚至很顺利）**不代表类型是干净的**——
esbuild 只删类型标注。把 esbuild 当作 `typeCheck` 引擎会被明确拒绝（`exit 5`）而不是"当作通过"。

**但要注意这一步的前提**（v0.14 实测补上）：`--engine=<name>` 的名字是**按 kind 查**的，
而内置清单里 `esbuild` 只覆盖 `bundle` / `transform`——所以**没声明**时它连能力检查都走不到，
直接是配置错误（`exit 3`）：

```bash
$ ngm typecheck --engine=esbuild          # 未声明 esbuild 为 typeCheck 引擎
ConfigInvalid: no `typeCheck` engine named "esbuild" in the catalog
  hint:  pick one of the known engines, or declare yours in ngm.engines.json
         (available: self, typescript)
```

**声明之后**才走到能力检查（`exit 5`，正是这一节要说明的那个拒绝）：

```bash
# 先在 ngm.engines.json 里加一条 {"name":"esbuild","kind":"typeCheck",...}
$ ngm typecheck --engine=esbuild
EngineNotFound: esbuild does not type-check: it only strips type annotations
  hint:  use the built-in `typescript` engine instead: pass --engine=typescript,
         or set "typeCheck": "typescript" in ngm.json (see modules/p4-ecosystem.md)
```

**两个退出码的差别不是措辞问题**：`3` 说的是"你的清单里没有这个名字"，
`5` 说的是"这个名字存在，但它干不了这件事"——修法不同（改名字 vs 换个引擎）。

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

## 单文件转换（`ngm transform`，v0.5）

`ngm build` 打包一个项目并解析导入；`ngm transform` 只碰**一个文件**，**不解析任何导入**。
它面向的是"想要一个引擎、不想要一个打包器"的管道——测试运行器、开发服务器、自己的构建步骤。

```bash
# 从 stdin 读（管道形态），产物走 stdout
cat src/util.ts | ngm transform --loader=ts --minify

# 从文件读：loader 由扩展名推断，并且**会把推断说出来**
ngm transform src/util.ts
# note: loader "ts" inferred from .ts (pass --loader to override)

# 产物落盘（由 ngm 写，不是引擎答应的）
ngm transform src/util.ts --outfile=dist/util.js

# 先看清它会怎么调引擎
ngm transform src/util.ts --target=es2020 --dry-run
```

规则（每一条都有验收测试钉着）：

| 规则 | 为什么 |
|------|--------|
| loader 按 **`--loader` → 文件扩展名 → `engines.transform.options.loader`** 的顺序取 | 三处来源各自的含义不同：命令行说得最明确、扩展名来自用户的输入、清单是团队声明 |
| 扩展名不认识时**不猜**（不凭空造一个 `--loader=`） | 猜错的代价是语法错误或更糟的静默转换 |
| "必须有 loader" **不是 ngm 的规则，是 esbuild 的**，由 adapter 判 | 自定义引擎可能压根不需要 loader。ngm 一度在 CLI 里复刻了这条规则，结果是**清单里声明好的 loader 被挡在门外**——与"声明了、没生效"同型 |
| 给文件时 loader 由扩展名推断，**并打印出来** | "我替你选了一个"和"你选的那个生效了"是两件事（本项目在"相对路径按了 CWD"上栽过两次，都是同一类） |
| `--outfile` 由 **ngm** 落盘 | transform 的引擎接口只有 stdout 一条出口（`TransformResult` 没有 outfile），所以这不是"引擎答应写的" |
| `--dry-run` 在缺 loader 时**同样报错** | 让 dry-run 成功而真实调用失败，恰好把"先看清它会怎么调"这件事变成骗人的 |
| **不解析导入** | 断言写着"import 仍在、被导入模块的代码不在产物里"；否则哪天它被实现成 bundle，用户会不知不觉拿到不一样的东西 |

`--target` / `--format` / `--minify` / `--sourcemap` 会翻译成引擎参数并真的生效
（`--sourcemap` 走内联：输出是 stdout，无法外链）。

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

### 先试脚手架

```bash
ngm integrations add vite      # 或 esbuild / deno / webpack
```

它按下面的规则生成配置，并且**不会覆盖**你已有的配置文件：内容不同时报出差异并以 exit 3 结束，
由你决定怎么合。下面的章节是它生成的内容与逐条理由，供已有配置的项目手工合并时对照。

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
否则类型检查会报"找不到模块"。

`ngm integrations add <tool>` 会把这些一次性生成好（见下），下面是它生成的内容——
如果你已经有自己的配置文件，照着手工合并即可。

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
