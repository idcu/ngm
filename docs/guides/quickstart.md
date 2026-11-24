# 快速上手

本文是一条可逐字复制的流程：init → add → install → verify → build → sbom。它只依赖两样外部条件：

- **Git**（ngm 自己不实现 Git，靠它拉仓库与重放对象）；
- **外部构建引擎**（下面用 esbuild；装成全局命令或装在项目里，缺了 ngm 返回 exit 5，不会静默产出空文件）。

需要 Node.js 还是 Deno 由你的项目决定，ngm 不替你选运行时。

## 1. 初始化

```bash
ngm init github.com/my-org/my-app --runtime=node
```

生成 `ngm.json` 与 `src/index.ts`（入口已存在时不覆盖）。`--runtime` 只接受 `node` / `deno`；目录里已有 `ngm.json` 时返回 exit 3。

`ngm.json` 的关键字段：

```json
{
  "schemaVersion": 3,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "./src/index.ts",
  "dependencies": [],
  "engines": { "transform": "esbuild", "bundle": "esbuild" },
  "vendor": { "mode": "local", "linkMode": "auto" }
}
```

`schemaVersion` 必须是 3；`vendor.mode` 默认 `local`，依赖落在项目的 `ngm.vendor/`。

## 2. 添加依赖

```bash
# tag：解析为 tag 当前指向的 commit
ngm add github:my-org/utils@v1.2.3 --ref-type=tag

# branch：解析为分支当前 tip
ngm add github:my-org/logger@main --ref-type=branch

# 完整 40 位 commit：refType 可省略
ngm add github:my-org/legacy@5b28ce4d1a2b3c4d5e6f708192a3b4c5d6e7f809

# monorepo 子路径
ngm add github:my-org/monorepo@v2.0.0 --ref-type=tag --path=packages/core

# 只打印结果，不写文件
ngm add github:my-org/utils@v1.2.3 --ref-type=tag --dry-run
```

`@main` 无法区分"叫 main 的 tag"和"叫 main 的分支"，所以 `refType` 通常必填；完整 40 位 hash 是唯一的窄默认。`ngm add --infer-ref-type` 会用一次远端 `ls-remote` 消歧，两者都存在时报错而不猜。

依赖也可来自本地：`ngm add local:kernel --ref-type=local-dir --source-path=./packages/kernel`，这类来源用目录摘要 `dirDigest` 作锚点，可完全离线。

## 3. 安装

```bash
ngm config validate
ngm install
```

流程：解析 refType → commit（或 dirDigest）→ 计算内容 digest → 落地 `ngm.vendor/` → 写 `ngm.lock` 与 `ngm.mappings.json`。

第一次访问远端会失败，这是**默认拒绝**的网络权限：

```
ConfigInvalid: permission denied: net:github.com
  hint:  add "net:github.com" to `permissions.allow` in ~/.ngm/config.json;
         paste {"permissions":{"allow":["net:github.com"]}}
```

把提示里的 JSON 写进 `~/.ngm/config.json`（文件不存在就新建，已存在就往 `allow` 数组里加一项），再重跑 `ngm install`。漏洞查询另需 `net:api.osv.dev`。

CI 里用这两个 flag 做门禁：

```bash
ngm install --frozen-lockfile            # 禁止解析新 ref、改 lock；允许网络
ngm install --offline                    # 禁止联网，只用本地内容
ngm install --frozen-lockfile --offline  # 完全离线且可复现（需 lock 已提交）
```

## 4. 验证

```bash
ngm verify
ngm verify --content        # 重放 digest + 逐文件字节比对（--deep 是旧别名）
ngm verify --json           # CI 读 summary.exitCode
ngm verify --format=github  # PR 注解
```

默认只比结构与大小，**等长字节篡改检不出来**；发布门禁用 `--content`。每个依赖的漂移分为 `expected`（分支正常前进）、`unexpected`（tag 被移动等）、`critical`（digest 不符，exit 2）；资源缺失记为 `operational`，exit 4。

## 5. 构建

```bash
ngm build --engine=esbuild --outfile=dist/app.js
ngm build --dry-run     # 只打印 ngm 会怎么调引擎，不执行
```

前置：esbuild 已安装，且 `~/.ngm/config.json` 里放行了 `run:esbuild`。`ngm build` 把 `ngm.mappings.json` 的每条映射变成引擎别名，所以 `import utils from "github:my-org/utils"` 能解析到 `ngm.vendor/` 里那份可证明的代码。除此之外它只是对外部引擎的薄封装；构建、压缩、类型检查都不由 ngm 实现。

## 6. 导出审计产物

```bash
ngm sbom --format=cyclonedx --out=sbom.cdx.json
ngm attest --out=provenance.intoto.json
```

两条命令只读 `ngm.lock`：不联网、不重解析、不写项目。lock 与 `ngm.json` 不一致时默认 exit 3、不产出；显式加 `--allow-stale` 才导出。`ngm attest` 的语句**未签名**，签名配方见[签名指南](./signing.md)。

## 提交什么

```bash
git add ngm.json ngm.lock ngm.mappings.json
git commit -m "chore: add dependencies"
```

`ngm.vendor/` 是否入库由你决定（离线交付 / 审计门禁时才需要）；ngm 自己从不执行 `git add`。

## 相关文档

- [安装指南](./installation.md) · [CLI 参考](./cli.md) · [配置详解](./configuration.md)
- [依赖管理](./dependency-management.md) · [构建](./build.md) · [测试](./test.md)
- [生态定位](./positioning.md) · [术语表](./glossary.md)
