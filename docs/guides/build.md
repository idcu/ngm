# 构建

ngm 不自研构建引擎：所有构建通过 engine adapter 调用外部工具（esbuild / tsc / postcss / deno）。ngm 负责把 `ngm.mappings.json` 变成引擎能懂的别名，并记录输入、参数与产物摘要。

## 基本用法

```bash
ngm build                                   # 使用 ngm.json 里 engines.bundle 的配置
ngm build --engine=esbuild
ngm build src/index.ts --engine=esbuild     # 指定入口
ngm build --production --engine=esbuild     # 压缩 + NODE_ENV=production
ngm build --outfile=dist/app.js             # 不写 --outfile 时产物走 stdout
ngm build --dry-run                         # 只打印 ngm 会怎么调引擎，不执行
ngm build --provenance                      # 写旁车回执 ngm.build.json
```

`--dry-run` 是排查"参数为什么没生效"最快的入口；它不执行引擎、不探测版本。

## 引擎选择

内置清单自带 `esbuild`（`bundle` + `transform`）、`typescript`（`typeCheck` + `typeDecl`，可选）与 `postcss`（`css`，可选）。`deno` 与 wasm 模块不在内置清单里，需在 `ngm.engines.json` 自行声明（Deno 的 `bundle` 是 ≥ 2.4 的实验特性；wasm 模块是项目本地文件，路径要你自己给）。

```json
{
  "version": 1,
  "engines": [
    { "name": "deno", "kind": "typeCheck", "adapter": "subprocess", "command": "deno check", "defaultOptions": {} },
    { "name": "deno", "kind": "bundle", "adapter": "subprocess", "command": "deno bundle", "defaultOptions": {} }
  ]
}
```

```bash
ngm typecheck --engine=deno
ngm build --engine=deno
```

按内置名声明但能力对不上时会明确失败：`exit 3` 表示"清单里没有这个名字"，`exit 5` 表示"这个名字存在，但干不了这件事"（例如 esbuild 只删类型标注，不做 `typeCheck`）。修法分别是改名字与换引擎。工具缺失一律 `exit 5`，不是"检查通过"。

### esbuild 不做类型检查

`ngm build` 通过**不代表类型干净**：esbuild 只删除类型标注。把 esbuild 当作 `typeCheck` 引擎会被明确拒绝；类型检查请用 `typescript`（`tsc`）或你声明的其它引擎。

```bash
ngm typecheck --engine=typescript
ngm typedecl --outdir=dist/types --engine=typescript
ngm css src/app.css --engine=postcss --outfile=dist/app.css
```

postcss 没有内建压缩：`ngm css --minify --engine=postcss` 是 `exit 3`，提示显式换一个支持压缩的引擎；声明 `"supportsMinify": true` 的条目才接受 `--minify`，绑定字段见[配置详解](./configuration.md#engines)。

## 单文件转换

`ngm transform` 只碰一个文件、**不解析任何导入**；`ngm build` 才打包并解析导入。它面向测试运行器、开发服务器和你自己的构建步骤。

```bash
cat src/util.ts | ngm transform --loader=ts --minify
ngm transform src/util.ts
ngm transform src/util.ts --outfile=dist/util.js
ngm transform src/util.ts --target=es2020 --dry-run
```

规则：

| 规则 | 为什么 |
|------|--------|
| loader 按 `--loader` → 文件扩展名 → `engines.transform.options.loader` 取 | 命令行最明确；扩展名来自输入；清单是团队声明 |
| 扩展名不认识时**不猜** | 猜错的代价是语法错误或静默转换错 |
| "必须有 loader"由引擎判，不是 ngm 的规则 | 自定义引擎可能不需要 loader，ngm 不复刻 esbuild 的限制 |
| 给文件时把推断出的 loader 打印出来 | "我替你选了"与"你选的生效了"是两件事 |
| `--outfile` 由 **ngm** 落盘 | transform 的引擎接口只有 stdout 一条出口 |
| `--dry-run` 缺 loader 时同样报错 | 不能让预演成功、真实调用失败 |
| 不解析导入 | 否则某天它被实现成 bundle，用户会悄悄拿到不同产物 |

`--target` / `--format` / `--minify` / `--sourcemap` 会翻译成引擎参数并生效；`--sourcemap` 走内联，因为输出是 stdout、无法外链。

## mappings 文件

`ngm install` 生成 `ngm.mappings.json`，供外部构建工具读取。先让脚手架生成配置，它不覆盖已有文件（内容不同时 exit 3，由你决定怎么合）：

```bash
ngm integrations add vite   # 或 esbuild / deno / webpack
ngm mappings validate
```

文件形态：

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

关键：**导入标识符 = `m.path ? m.from + "/" + m.path : m.from`**，它不等于 `from`。同一仓库的多个子路径共用 `from`、各有自己的 `to`，用 `from` 当键会丢行。字符串 alias 是前缀匹配，长标识符要排在短标识符之前，否则子路径被吞掉。

```ts
// vite.config.ts
import mappings from './ngm.mappings.json'
const specifier = (m: { from: string; path?: string }) =>
  m.path ? `${m.from}/${m.path}` : m.from
export default {
  resolve: {
    alias: [...mappings.mappings]
      .sort((a, b) => specifier(b).length - specifier(a).length)
      .map(m => ({ find: specifier(m), replacement: m.to }))
  }
}
```

```js
// build.js（esbuild）
import mappings from './ngm.mappings.json'
import { build } from 'esbuild'
const specifier = m => (m.path ? `${m.from}/${m.path}` : m.from)
build({
  entryPoints: ['src/index.ts'],
  alias: Object.fromEntries(
    [...mappings.mappings]
      .sort((a, b) => specifier(b).length - specifier(a).length)
      .map(m => [specifier(m), m.to])
  ),
  outfile: 'dist/index.js',
  bundle: true
})
```

Deno 用 `deno.json` 的 `imports` 做精确键，子路径多时可加尾部带 `/` 的前缀键：

```json
{
  "imports": {
    "github:my-org/utils": "./ngm.vendor/github.com/my-org/utils/index.ts",
    "github:my-org/monorepo/packages/core": "./ngm.vendor/github.com/my-org/monorepo/packages/core/src/index.ts"
  }
}
```

TypeScript 语言服务不识别 `github:` 前缀，需在 `tsconfig.json` 的 `paths` 映射 vendor 路径；用 `*` 做通配可覆盖子路径。**不要写 `baseUrl`**（TypeScript 已移除该选项）。

```json
{
  "compilerOptions": {
    "moduleResolution": "bundler",
    "paths": {
      "github:my-org/utils": ["./ngm.vendor/github.com/my-org/utils/index.ts"],
      "github:my-org/monorepo/*": ["./ngm.vendor/github.com/my-org/monorepo/*"]
    }
  }
}
```

## 构建回执与确定性口径

`ngm build --provenance` 写 `ngm.build.json`：输入摘要、引擎的 name/kind/version/字节摘要、排序后的 argv、mappingsDigest、产物摘要。不进 lock、不写时间戳、不写绝对路径。`ngm verify --receipt` 只读复验这份回执的**回执字节**，不重跑构建；产物不符 exit 2，缺引擎 exit 5，2 压过 5（[ADR-057](../adr/README.md)）。

ngm 对压缩产物**只承诺一件事**：**同输入 / 同参数 / 同引擎** 时，esbuild 的 `--minify` 回执字节是确定的。重复同一条命令、同一份输入、同一个引擎二进制，`ngm.build.json` 记录的产物摘要应当逐字节一致。

**不跨工具承诺**——这是刻意的边界：

- 不承诺 esbuild / lightningcss / cssnano 等**不同压缩工具**之间字节一致；
- 不承诺同一工具的**不同版本**（哪怕只差一个补丁）之间字节一致；
- 不承诺跨操作系统 / 平台字节一致；回执记录的是这次实际使用的工具字节摘要，不是"可复现构建"认证。

边界由 [ADR-013 远程 adapter 的信任边界](../adr/README.md)固定：ngm 不自研引擎、压缩交给外部工具。可复现性探针 [`scripts/probe-reproducibility.sh`](../../scripts/probe-reproducibility.sh) 给出当前工具链下"同输入 / 同参数 / 同引擎 → 同字节"的实测路径；探针过了只说明那一次组合确定，不扩展上面的承诺范围。

## 为什么这样设计 / 边界

- ngm build 是薄封装：本质是 spawn 外部引擎，不比直接调 esbuild 更快；它的价值在于构建时用的 vendor 代码可证明。
- 引擎外置：esbuild / tsc / postcss 不是 ngm 的自研实现，只是清单条目；工具缺失是 `exit 5`，不是"检查通过"（[ADR-025](../adr/README.md)）。
- 不做 HMR / Dev Server / 代码分割优化 / 完整 Source Map 链路 / 框架插件，全部交给 Vite 等专业工具；完整归属见[能力矩阵](../internals/capability-matrix.md)。
- 引擎清单的版本与摘要绑定见[配置详解](./configuration.md#engines)，底层接口见[引擎 adapter](../architecture/engine-adapter.md)。

## 相关文档

- [快速上手](./quickstart.md) · [配置详解](./configuration.md) · [依赖管理](./dependency-management.md)
- [CLI 参考](./cli.md) · [生态定位](./positioning.md) · [能力矩阵](../internals/capability-matrix.md)
