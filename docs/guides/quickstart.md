# 快速上手

> 本页的 5 分钟流程（init → add → install → verify → build）可在真实项目上**逐字执行**，
> 由 CI 的两条验收守护：`TestM7Quickstart`（用受控的**替身引擎**，保证流程本身不被外部工具影响）
> 与 `TestM7Quickstart_RealToolchain`（用**真实 esbuild**，机器上没装 esbuild 时自动跳过）。
> 未实现的能力都显式标注了版本号。

---

## 5 分钟上手

### 1. 初始化项目

```bash
# Node.js 项目
ngm init github.com/my-org/my-app --runtime=node

# Deno 项目
ngm init github.com/my-org/my-app --runtime=deno
```

生成：

```
my-app/
├── ngm.json
├── src/
│   └── index.ts
└── ngm.vendor/   （install 后生成）
```

### 2. 添加依赖

```bash
# tag 依赖
ngm add github:my-org/utils@v1.2.3 --ref-type tag

# branch 依赖
ngm add github:my-org/logger@main --ref-type branch

# commit 依赖
ngm add github:my-org/legacy@abc123def --ref-type commit
```

`--ref-type` 必填，因为 `@main` 无法区分是 tag 还是 branch。

### 3. 安装

```bash
ngm install
```

流程：

```
解析 refType → commit → archiveDigest → vendor 4 层落地 → 生成 lock
```

> 这几个词（`refType` / `archiveDigest` / `vendor 4 层` / `driftKind`）都有精确定义，
> 不熟就先翻[术语表](./glossary.md)——每个词都标了出处，不用猜。

### 4. 验证

```bash
ngm verify
```

检查 ref 是否仍指向锁定 commit。

### 5. 构建

> **前置条件**：`ngm build` 调用的是**外部**引擎，需要先装 esbuild（`npm i -g esbuild`，或装在项目里）。
> 没装时 ngm 返回 `exit 5`（引擎不可用）并给出提示，不会静默产出一个空文件。

```bash
# 通过 adapter 调 esbuild（v0.1 唯一适配的 bundle 引擎）
ngm build --engine=esbuild

# 指定产物；不写 --outfile 时产物走 stdout（与直接跑 esbuild 一致）
ngm build --outfile=dist/app.js

# 先看看 ngm 究竟会怎么调引擎（只打印，不执行）
ngm build --dry-run
```

`ngm build` 会把 `ngm.mappings.json` 的每条映射变成引擎的模块别名，因此
`import utils from "github:my-org/utils"` 能解析到 `ngm.vendor/` 里那份**可证明**的代码。

```bash
# 或调 deno —— v0.3 适配；v0.1 可用 ngm.engines.json 自行声明 subprocess 引擎
# ngm build --engine=deno
```

---

## 完整工作流

```bash
# 初始化
ngm init github.com/my-org/my-app --runtime=node

# 添加依赖
ngm add github:vuejs/core@v3.5.0 --ref-type tag
ngm add github:my-org/utils@main --ref-type branch

# 安装
ngm install

# 提交 lock file
git add ngm.json ngm.lock
git commit -m "chore: add dependencies"

# CI 安装（不修改 lock）—— `--frozen-lockfile` 属 v0.2，见路线图
# ngm install --frozen-lockfile
ngm install

# 检查漂移
ngm verify

# 审计已知漏洞（OSV.dev；结果缓存 24 小时，--offline 时只读缓存）
ngm audit

# 构建
ngm build --engine=esbuild
```

---

## ngm.json 最小结构

`ngm init` 生成的就是下面这份（逐字取自实际输出）：

```json
{
  "schemaVersion": 1,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "./src/index.ts",
  "dependencies": [],
  "engines": {
    "transform": "esbuild",
    "bundle": "esbuild"
  },
  "vendor": {
    "mode": "local",
    "linkMode": "auto"
  }
}
```

- `main` 与 `src/index.ts` 一并生成，使 `ngm build` 无需参数即可工作（`ngm init` **不会**覆盖已存在的入口文件）
- 依赖由 `ngm add` 追加；手写时字段为 `name` / `ref` / `refType`（`refType` **必填**），可选 `path`（monorepo 子目录）

手写一份带依赖的示例：

```json
{
  "schemaVersion": 1,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "./src/index.ts",
  "dependencies": [
    {
      "name": "github:my-org/utils",
      "ref": "v1.2.3",
      "refType": "tag"
    },
    {
      "name": "github:my-org/logger",
      "ref": "main",
      "refType": "branch"
    }
  ],
  "engines": {
    "transform": "esbuild",
    "bundle": "esbuild"
  }
}
```

> `typeCheck: "typescript"` 在 v0.1 尚不可用：ngm 只适配了 esbuild，声明未适配的引擎会得到
> 明确的 `exit 5`（而不是静默跳过）。自行声明 subprocess 引擎的方法见[配置详解](./configuration.md)。

---

## ngm.lock 示例

```json
{
  "version": 1,
  "lockfileVersion": "1.0.0",
  "dependencies": [
    {
      "name": "github:my-org/utils",
      "ref": "v1.2.3",
      "refType": "tag",
      "commit": "abc123def4567890abcdef1234567890abcdef12",
      "archiveDigest": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "resolvedAt": "2026-09-29T10:00:00Z",
      "vendorPath": "github.com/my-org/utils"
    }
  ]
}
```

---

## 下一步

- [配置详解](./configuration.md)
- [依赖管理](./dependency-management.md)
- [构建](./build.md)
- [Node vs Deno](./node-vs-deno.md)
