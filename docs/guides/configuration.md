# 配置详解

ngm 的配置全部是 JSON：便于机器生成、schema 校验与 CI diff。写错字段会被拒绝，不会被静默忽略；完整校验入口是 `ngm config validate`。

## 配置文件

| 文件 | 作用 | 位置 |
|------|------|------|
| `ngm.json` | 依赖声明（refType 条件必填）+ 引擎选择 + 供应链策略 + 运行时 | 项目根目录 |
| `ngm.lock` | 锁定文件：commit + 内容 digest（根 archiveDigest / 模块 moduleDigest）+ resolvedAt | 项目根目录，**必须提交** |
| `ngm.mappings.json` | 构建工具映射（供外部构建工具读取裸导入） | 项目根目录，**必须提交** |
| `ngm.engines.json` | 引擎清单（可选，覆盖内置清单） | 项目根目录或全局 `~/.ngm/` |
| `~/.ngm/config.json` | 全局配置（Git 凭证、默认引擎、权限） | 用户主目录 |

`ngm.mappings.json` 必须提交：它和 lock 一样是"由 lock + vendor 布局确定的投影"，同一份 lock 与同一个 `linkMode` 生成逐字节相同的文件；多阶段镜像与 CI 的 build job 通常不跑 `ngm install`，那时构建工具只能读仓库里的那份。`ngm mappings validate` 做"文件 ↔ lock ↔ vendor 树"三方对账。`ngm.vendor/` 是否提交由 Git 侧决定，ngm 从不执行 `git add`。

## ngm.json

`schemaVersion` 必须是 3；`ngm init` / `ngm add` 写出的就是 3，旧版清单用 `ngm migrate --dry-run` 预演、`ngm migrate` 落地（只搬能证明的字段，备份为 `.bak`）。超出范围的版本 exit 3。

```json
{
  "schemaVersion": 3,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "./src/index.ts",
  "types": "./src/index.d.ts",
  "dependencies": [
    { "name": "github:my-org/utils", "ref": "v1.2.3", "refType": "tag" },
    { "name": "github:my-org/monorepo", "ref": "v2.0.0", "refType": "tag", "path": "packages/core" },
    { "name": "local:kernel", "ref": "auto", "refType": "local-dir", "sourcePath": "./packages/kernel" }
  ],
  "engines": { "bundle": "esbuild", "transform": "esbuild", "typeCheck": "typescript", "css": "postcss" },
  "vendor": { "mode": "local", "linkMode": "auto" },
  "supplyChain": {
    "allowedGitHosts": ["github.com", "gitee.com"],
    "allowlistRepos": ["github.com/my-org/*"],
    "minimumReleaseAge": "P1D",
    "verifyOnLock": true
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 项目标识（Git URL 形式） |
| `version` | string | 是 | 项目版本 |
| `runtime` | `"node" \| "deno"` | 是 | 宿主运行时 |
| `main` | string | 否 | 入口文件（本项目作为被依赖方时，供 mappings 推断；见下"入口推断"） |
| `types` | string | 否 | 类型声明入口（同上；推断出的 `types` 会写进 `ngm.mappings.json`，并优先用于 tsconfig paths） |
| `publish` | boolean | 否 | 模块发布开关：nil 跟随 `--releases`；显式 true/false **完全覆盖**全局开关（[ADR-037](../adr/README.md)） |
| `dependencies` | array | 是 | 依赖列表 |
| `engines` | object | 否 | 引擎选择 |
| `vendor` | object | 否 | vendor 模式配置 |
| `supplyChain` | object | 否 | 供应链策略 |
| `scripts` | object | 否 | `ngm run` 的**回退**脚本来源：键是脚本名，值是命令串。`package.json.scripts` 是权威来源；同名脚本同时出现在两处是配置错误（exit 3，报错点名两处），不会静默覆盖 |

`version` 与 `schemaVersion` 是声明 / 门禁字段：被校验、被回显，不参与解析。`runtime` 同理，ngm 不干预宿主运行时。`scripts` 只在 `package.json` 没有同名脚本时生效。

### 入口推断（作为被依赖方）

本项目被其他仓库依赖时的三层入口顺序、候选文件名与 `exports` 条件优先级，统一写在[包开发指南](./package-authoring.md)；全部缺失时 `ngm install` 输出警告，映射仍生成但不带入口字段。

## dependencies

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 依赖标识：git 来源 `git-host:org/repo`；本地来源 `local:<name>` |
| `ref` | string | 是 | tag / branch / commit 名字；local-dir 为 `auto` 或 `sha256:<hex>` |
| `refType` | `"commit" \| "tag" \| "branch" \| "local-git" \| "local-dir"` | 条件必填 | 完整 40 位 commit hash 可省略（按 commit）；其余**必填** |
| `path` | string | 否 | monorepo 子路径 |
| `sourcePath` | string | local-* 必填 | 相对项目根的本地目录，不得含 `..` 逃逸 |
| `localRepo` | string | local-git 必填 | 相对项目根的本地 git 仓根路径 |

`@main` 无法区分"叫 main 的 tag"和"叫 main 的分支"，因此 `refType` 通常必填。唯一的窄默认（[ADR-035](../adr/README.md)）：`ref` 是完整 40 位十六进制 hash 时可省略，按 `commit` 处理并写回；tag、branch、7–39 位短 hash 仍必须显式。`--infer-ref-type` 做一次 `ls-remote`，只有 tag/branch 之一存在才写入，两者都有则报错。

本地来源用 `local:<name>` 命名空间，与 git 依赖隔离（[ADR-052](../adr/README.md)）：

```jsonc
{ "name": "local:kernel", "ref": "auto", "refType": "local-dir", "sourcePath": "./packages/kernel" }
{ "name": "local:payroll", "ref": "v1.0.0", "refType": "local-git", "sourcePath": "./packages/packs/payroll", "localRepo": "./" }
```

`local-dir` 的 `ref: auto` 让 `ngm install` 计算目录内容摘要并写进 lock；显式 `sha256:<hex>` 则钉住内容。`local-git` 复用 commit 证明链、可完全离线。`sourcePath` 不得逃逸项目根；不做 workspace 编排。

## engines

简写 `"transform": "esbuild"` 等价于 `{"primary": "esbuild", "fallbacks": []}`，不自动回退。完整写法按 `primary` → `fallbacks` 顺序尝试；省略 `fallbacks` 即不回退。

```json
{
  "engines": {
    "transform": { "primary": "esbuild", "options": { "target": "es2022" } },
    "typeCheck": { "primary": "typescript", "fallbacks": ["deno"] },
    "css": { "primary": "postcss" },
    "bundle": { "primary": "esbuild" }
  }
}
```

内置清单自带 `esbuild`（bundle / transform）、`typescript`（typeCheck / typeDecl，可选）与 `postcss`（css，可选）；没装可选引擎不算清单问题。`deno` 与 wasm 模块不在内置清单里，要在 `ngm.engines.json` 自行声明。`self` 只用于 dry-run 与离线 stub，不要放进生产 fallbacks。

`ngm.engines.json` 的每条 `engines[]` 除 `name` / `kind` / `adapter` / `command` / `version` 外，还可写三个可选字段（未知字段会被拒绝）：

```json
{
  "version": 1,
  "engines": [
    {
      "name": "postcss",
      "kind": "css",
      "adapter": "subprocess",
      "command": "postcss",
      "expectedVersion": "8.4.0",
      "expectedDigest": "sha256:<工具文件字节摘要>",
      "supportsMinify": true
    }
  ]
}
```

| 字段 | 类型 | 留空时 | 写入后 |
|------|------|--------|--------|
| `expectedVersion` | string | 维持当前探测行为；`version` 与实装不符仍只是信息 | 与探测到的版本不符 → `ngm engines validate` exit **5** |
| `expectedDigest` | string | 不核对工具字节 | 与工具文件字节的 sha256 不符 → exit **2**（压过 5 与 3；允许省略 `sha256:` 前缀） |
| `supportsMinify` | bool | 视为 false | 声明该 CSS 条目支持 `--minify`；未声明却给 `--minify` → `ngm css` exit **3** |

摘要由 Go 标准库读取工具文件字节计算，不 spawn `sha256sum`，也不把工具路径当身份（[ADR-058](../adr/README.md)）。压缩的确定性口径见[构建指南](./build.md)。

## supplyChain

| 字段 | 类型 | 说明 | 示例 |
|------|------|------|------|
| `allowedGitHosts` | string[] | 限制可接受的 Git host | `["github.com", "gitee.com"]` |
| `allowlistRepos` | string[] | 白名单仓库（glob） | `["github.com/my-org/*"]` |
| `minimumReleaseAge` | string | 最小晾晒期：基于 commit 的 committer date（ISO 8601 duration）。`ngm init` 写入 `"P1D"`；显式 `"PT0S"` 是项目级关闭，`"P0D"` 被拒绝 | `"P1D"` |
| `trustPolicy` | `"" \| "no-downgrade"` | `no-downgrade` 时，update 若把已有依赖从强锚点（commit/dirDigest）退到 tag/branch 就 exit 3 | `"no-downgrade"` |
| `osvIgnoreSeverities` | string[] | OSV 忽略的严重级别 | `["LOW"]` |
| `postInstallPolicy` | `"deny" \| "prompt" \| "allow"` | postinstall 策略：`allow` 时才在**沙箱内**执行 `postinstall.js`（`prompt` 在非交互工具里等于不执行） | `"deny"` |
| `verifyOnLock` | boolean | 更新 lock 时自动 verify | `true` |

写了 `supplyChain` 段但没写晾晒期时默认 P1D；完全不配这一段则不做晾晒期门禁。`allowedGitHosts` / `allowlistRepos` / `minimumReleaseAge` 在解析阶段判定，命中即 exit 3 并给出来源链。

`patches` 只替换锚点，不改依赖名与来源身份，也不是 semver override。每条要求 `name` / `from` / `to` / `why` 齐全，同一个 `name` 只能有一条；`to` 必须是完整 commit、tag/branch 或 `sha256:<64hex>`。生效锚点与理由写进 lock 的 `patchFrom` / `patchWhy`，供审计"为何与声明不同"。

```json
"patches": [
  { "name": "gitee:org/repo", "from": "v1.0.0", "to": "v1.0.1", "why": "上游 tag 撤回" }
]
```

## vendor

| 模式 | 配置 | 适用场景 | 代价 |
|------|------|---------|------|
| 全局缓存 | `"mode": "global"` | 多项目共享，省磁盘 | 削弱隔离收益 |
| 本地 vendor/ | `"mode": "local"`（默认） | 隔离清晰，CI 可复现 | 跨项目重复存储 |

`vendor.globalPath` 只接受 `~/.ngm/global-vendor` 或其子目录；相对路径、`..`、项目外绝对路径都会被 `ngm config validate` 与 `install` / `update` 拒绝。`vendor.commit` 字段已移除，写入会被拒绝（exit 3）；是否提交 `ngm.vendor/` 改用 Git 侧做法。

| linkMode | 行为 |
|------|------|
| `auto`（默认） | hardlink 优先，跨卷或不支持时降级复制 |
| `hardlink` | 强制 hardlink，失败即报错 |
| `copy` | 普通复制（网络文件系统、需要就地修改依赖时用） |
| `symlink` | v2 布局下退化为逐文件 hardlink，CLI 会如实打印实际模式 |

`auto` / `hardlink` 落地的是指向 content store 的硬链接：**直接编辑 `ngm.vendor/` 里的文件会同时改写 content store**，且后续 `ngm install` 不会察觉。检测路径是 `ngm verify --content`。

## NGM_HOME 与全局配置

`NGM_HOME` 就是 `~/.ngm` 的位置，不是另一个 home：未设置时 mirror / module-cache / content store / global vendor 在 `<home>/.ngm/`，全局配置在 `<home>/.ngm/config.json`；设置 `NGM_HOME=<dir>` 后两者都跟着走。相对路径按当前工作目录解析为绝对路径。

`~/.ngm/config.json`：

```json
{
  "git": {
    "defaultProtocol": "ssh",
    "tokenEnvVars": { "github.com": "GITHUB_TOKEN", "gitee.com": "GITEE_TOKEN" }
  },
  "engines": { "defaultTransform": "esbuild", "defaultBundle": "esbuild", "defaultTypeCheck": "typescript" },
  "permissions": {
    "allow": ["run:git", "run:esbuild", "run:tsc", "net:github.com"],
    "deny": ["run:npm", "run:yarn", "run:pnpm", "env:GITHUB_TOKEN"]
  }
}
```

`net:` 与 `run:` 默认档位是"需配置"：既没 `allow` 也没 `deny` 的权限按**拒绝**处理，第一次访问远端或调用引擎会 exit 3，提示里给出可直接粘贴的 JSON。漏洞查询走另一个端点：`ngm audit` / `ngm tree --osv` 需要 `net:api.osv.dev`；命中缓存与 `--offline` 不碰网络，因此不需要它。权限命名空间（`read:` / `write:` / `net:` / `run:` / `env:`）与默认值见[安全模型](../architecture/security-model.md)。`deny` 里的 `env:<NAME>` 对所有 ngm 启动的子进程生效，被拒绝的名字从子进程环境里剔除。

## 版本与校验

所有配置与协议文件都带版本标识（`ngm.json` / `ngm.lock` / `ngm.mappings.json` / `ngm.engines.json`）。同一大版本内只新增可选字段；破坏性变更递增大版本。v1/v2 清单与 2.x lock 用 `ngm migrate` 迁移；**`ngm lock migrate` 这个迁移命令仍不存在**——3.x 的 lock 直接废止旧版，重新生成即可（[ADR-052](../adr/README.md)）。旧格式被读取器以 exit 3 拒绝并写出支持区间。

```bash
ngm config validate                 # 校验当前项目
ngm config validate --dir=../other  # 校验另一个目录
ngm config show                     # 查看合并后的生效配置
ngm engines list
ngm engines info esbuild
ngm engines validate
```

退出码有机器可读契约 `contracts/exit-codes.json`（`{code, name, meaning, retryable, source}`）；脚本判断"该不该重试"读 `retryable`，不要 grep 人类可读输出。

## 相关文档

- [依赖管理](./dependency-management.md) · [构建](./build.md) · [CLI 参考](./cli.md)
- [锁定机制](../architecture/locking.md) · [供应链防护](../architecture/supply-chain.md) · [安全模型](../architecture/security-model.md)
- [ADR 索引](../adr/README.md)
