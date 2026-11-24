# 开发可被 ngm 引用的包

> **结论**：一个"ngm 包"不是 registry 上的发布物，而是**一个能 clone 的 Git 仓库（或仓库子目录）+ 一个可解析的 ref + 一个能推断出的入口**。
> 单包最少三件事：`ngm.json`、`package.json`、真实存在的入口文件；打一个不再移动的 tag，别人就能 `ngm add` 它。
> monorepo 把每个包做成独立模块，tag 用 `<path>/v<版本>`。本文是**包作者唯一入口**；消费方流程见[依赖管理](./dependency-management.md)。

## 1. 先选形态

| 形态 | 你维护什么 | 消费方声明 | lock 里的证明 |
|---|---|---|---|
| 单包 Git 仓库 | 根目录 `ngm.json` + `package.json` + 入口 + tag `v1.2.3` | `ngm add github:org/repo@v1.2.3 --ref-type=tag` | `archiveDigest` |
| monorepo 子路径 | 每个 `packages/<name>/` 自带清单与入口，tag `packages/<name>/v1.2.3` | `ngm add github:org/repo@v1.2.3 --ref-type=tag --path=packages/core` | `moduleDigest` + `subtreeOid` |
| 本地目录 | 目录内有入口与清单；消费方用 `local-dir` / `local-git` 指向它 | `ngm add local:kernel --ref-type=local-dir --source-path=./packages/kernel` | `dirDigest` / commit |

三点前提，无论哪种形态都成立：

- **ref 必须可解析且稳定**：tag 一旦发出不要移动。tag 移动会让消费方的 `ngm verify` 报漂移，branch 只适合开发期。
- **仓库必须可访问**：公开仓库直接 clone；私有仓库需要消费方自己配 Git 凭证，ngm 不代管密钥。
- **入口必须能被推断**：三层推断规则见第 3 节；三层都落空时映射仍生成，但没有 `main` / `types`，消费方构建会找不到入口。

本地目录的最小可跑形态见 [examples/local-dir-offline](../../examples/local-dir-offline/)：`vendor-src/fixture-pkg/` 只有 `package.json` 与 `index.js`，靠第 3 层约定提供入口。

## 2. 最小单包：从零到可被引用

目录骨架（名字换成你自己的）：

```
my-utils/
├── ngm.json
├── package.json
├── LICENSE
├── README.md
└── src/
    ├── index.js
    └── index.d.ts
```

`package.json` —— 给 Node 工具链与库标准自检用：

```json
{
  "name": "@my-org/utils",
  "version": "1.2.3",
  "license": "MIT",
  "type": "module",
  "main": "./src/index.js",
  "types": "./src/index.d.ts",
  "exports": {
    ".": {
      "types": "./src/index.d.ts",
      "import": "./src/index.js"
    }
  }
}
```

`ngm.json` —— ngm 读的第一层入口，也是传递依赖与内容证明的载体：

```json
{
  "schemaVersion": 3,
  "name": "github.com/my-org/utils",
  "version": "1.2.3",
  "runtime": "node",
  "main": "./src/index.js",
  "types": "./src/index.d.ts",
  "dependencies": []
}
```

字段要点（完整 schema 见[配置详解](./configuration.md)）：

- `schemaVersion` 必须是 `3`；写未知字段会被拒绝，不会静默忽略。
- `exports` **只属于 `package.json`**。它不是 `ngm.json` 字段，写进 `ngm.json` 会 exit 3。
- `permissions` 也**不属于项目 `ngm.json`**：那是消费方 `~/.ngm/config.json` 的门禁（见[安全模型](../architecture/security-model.md)）。包作者不能替消费方放行网络或引擎。
- 你的 Git 依赖要声明在 `dependencies[]` 里，写成 `{ "name": "github:org/lib", "ref": "v1.0.0", "refType": "tag" }`。`package.json` 里的 Git 依赖不会被 ngm 解析，只会触发"请迁到 ngm.json"的警告。
- `src/index.js` 与 `src/index.d.ts` 必须真实存在：入口声明指向不存在的文件时，自检会失败。

## 3. 入口推断：消费方拿到什么

`ngm install` 从**同一棵内容树**落地 vendor 并生成 `ngm.mappings.json`，入口按下面的顺序取，先命中者胜出：

| 层 | 来源 | 取什么 |
|---:|---|---|
| 1 | 包自己的 `ngm.json` | `main` / `types`（两者都为空才落下一层） |
| 2 | 包自己的 `package.json` | `exports["."]`，其次 `main` / `types`。`exports` 支持字符串、`{".": "..."}` 与条件对象；类型取 `types`，运行时按 `import` → `module` → `default` → `require` 取第一个非空值 |
| 3 | 约定文件名 | main：`./index.ts`、`./index.js`、`./index.mjs`、`./index.cjs`；types：`./index.d.ts`、`./index.d.mts`、`./index.d.cts` |

消费方代码里的导入标识符不是 `package.json` 的 `name`，而是 mappings 的 specifier：没有子路径时就是依赖名，有子路径时是 `依赖名/子路径`。例如：

```ts
import { hello } from "github.com/my-org/utils";
import { core } from "github.com/my-org/collection/packages/core";
```

构建工具如何读这些映射（Vite / esbuild / tsconfig paths / Deno import map）见[构建指南](./build.md)。

## 4. 发布与自检

在包仓库里按顺序跑：

```bash
ngm config validate --dir=.
git tag -a v1.2.3 -m "release 1.2.3"
git push --tags
```

再用 ngm 仓库里的库标准脚本检查你的目录（Windows 用 `scripts/check-library-standard.ps1 -Dir <dir> -Strict`）：

```bash
bash scripts/check-library-standard.sh ../my-utils
bash scripts/check-library-standard.sh ../my-utils --strict
bash scripts/check-library-standard.sh ../my-utils --json
```

脚本的唯一实现是 [check-library-standard.sh](../../scripts/check-library-standard.sh)，需要本机有 `node`。检查口径：

- **必做项**（不满足 exit 1）：`package.json` 合法且有 `name` / semver `version`；入口可解析且文件存在；入口指向 `dist/` `lib/` 等目录时产物未被 `.gitignore` 排除；无 `preinstall` / `install` / `postinstall` / `prepare` 钩子、无 `.npmrc`；无 Git submodule（gitlink）、LFS 指针与逃逸包目录的 symlink；文本文件 LF；如带 `ngm.json` 须通过 schemaVersion 3 / name / version / runtime 校验；有 `license` 字段或 `LICENSE` 文件。
- **推荐项**（只有 `--strict` 才计入退出码）：ESM-first、`exports` 条件、提供 types、发布范围、`README.md`、`engines`、仓库根 `.gitattributes` 声明 LF、semver tag、Git 依赖同时写进 `ngm.json`。
- **无法检查**（exit 2）：目录不存在、没有 `node`、`package.json` 读不出来。不要把 exit 2 当成通过。

`postinstall.js` 即使存在，消费方默认也是 `deny`（只有显式允许才会在沙箱里执行）；不要把它当成发布流程的一环。

## 5. monorepo：多个模块与集合索引

### 目录与 tag

```
collection/
├── ngm.json                  # 仓库根清单：项目本身，不是模块
├── ngm.collection.json       # 由 modules index 生成，可手工补约束
└── packages/
    ├── core/
    │   ├── ngm.json          # 模块清单：schemaVersion 3 + name/version/runtime
    │   ├── package.json
    │   └── src/index.ts
    └── ui/
        ├── ngm.json
        └── package.json
```

规则（来自动作实现，不是约定俗成）：

- `ngm modules` 只把**嵌套的 `ngm.json`** 当模块；仓库根 `ngm.json` 是项目清单，不算模块。
- 模块 `version` 写纯 `1.0.0`（`validate --strict` 也接受前导 `v`，但 tag 生成固定拼成 `<path>/v<版本>`，写 `v1.0.0` 会得到 `vv1.0.0`）；版本需形如 semver，可有 `-rc1` / `+build` 后缀。
- tag 用 `packages/core/v1.0.0`。`ngm modules index` 从本地 tag 收集版本 → commit 映射；tag 不在本地就收集不到。
- 单个模块可在清单里写 `publish: false` 隐藏它；显式 `true` / `false` 完全覆盖索引命令的 `--releases` 开关，留空才跟随。

### 集合门禁

```bash
ngm modules validate --dir=. --strict
ngm modules index --dir=. --dry-run --json
ngm modules index --dir=.
ngm modules validate --dir=. --strict --index=ngm.collection.json
```

- `validate` 的退出码只有 0 / 3：0 表示零错误，3 表示清单非法、重名、版本不合格、索引不一致之一。**不要拿 `modules list` 当门禁**：它即使发现模块解析错误也返回 0。
- `--strict` 额外要求每个模块有 `name` / semver-like `version`，并开启共享依赖一致性检查：同一个依赖 `name` 在多个模块里必须钉到同一个 `ref (refType)`，否则 exit 3。
- 确实允许某个依赖分歧时，在生成的 `ngm.collection.json` **顶层手工补** `constraints`（索引生成器不写这一段；写进 `ngm.json` 无效）：

```json
{
  "constraints": {
    "ignoreSharedRefs": ["github:my-org/shared"]
  }
}
```

豁免名单里的每个名字都必须至少被一个模块声明，否则报 `stale exemption` 而不是悄悄放行。改完再跑一次带 `--index` 的 `validate`。

## 6. 从消费方验证一次

发布前用一份临时消费项目走完闭环，别只信本地单测：

```bash
ngm add github:my-org/utils@v1.2.3 --ref-type=tag
ngm add github:my-org/collection@v1.0.0 --ref-type=tag --path=packages/core
ngm install
ngm verify --content
ngm mappings validate
```

- `ngm verify` 默认只比结构与大小；`--content` 才逐字节重放 `archiveDigest` / `moduleDigest`。发布门禁用后者。
- 消费方 `ngm init` 默认写入 `minimumReleaseAge: "P1D"`：刚推的 tag 可能被晾晒期挡下。不要为了自测永久关掉它；理解行为即可（配置见[配置详解](./configuration.md)）。
- CI 冷启动用 `ngm install --frozen-lockfile --offline` + `ngm verify --json`，并提交 `ngm.json`、`ngm.lock`、`ngm.mappings.json`。
- 若你的包自身也有赋值构建回执的需求：`ngm build --provenance --engine=esbuild --outfile=dist/app.js` 生成旁车 `ngm.build.json`，`ngm verify --receipt` 只读复验；回执不进 lock，也不等于可复现构建认证（见[CLI 参考](./cli.md)）。

## 7. 常见坑

| 现象 | 原因 / 正确做法 |
|---|---|
| `ngm add` 后导入报找不到模块 | 入口三层都没命中，或导入字符串用了 `package.json` 的 `name`；改用 mappings specifier |
| 把 `exports` / `permissions` 写进 `ngm.json` | 两者都不属于项目清单；前者在 `package.json`，后者在消费方 `~/.ngm/config.json`，写错 exit 3 |
| 消费方 `verify` 报 tag 漂移 | tag 被移动或删除后重建；tag 应视为不可变 |
| 共享依赖报 ref 不一致 | 统一各模块的 `ref` + `refType`，或按第 5 节显式豁免，不要改 lock 绕过 |
| `verify` 绿了但字节被改 | 默认只查结构；发布门禁必须加 `--content` |
| `ngm.vendor/` 里的手改没被 install 发现 | vendor 是内容树的硬链接投影；不要就地修改，改动需回到上游仓库 |
| 子模块 / LFS 内容被拒绝 | ngm 不哈希指针，改用普通文件；见[依赖管理](./dependency-management.md) |

## 相关文档

- [发布指南](./publishing.md) · [依赖管理](./dependency-management.md) · [配置详解](./configuration.md) · [CLI 参考](./cli.md)
- [依赖解析](../architecture/dependency-resolution.md) · [锁定机制](../architecture/locking.md) · [生态定位](./positioning.md)
