# 依赖管理

ngm 依赖的来源是 Git ref 或本地目录，不是 registry 包。lock 钉住锚点与内容 digest，`ngm verify` 复核；registry 包交给 pnpm / npm / yarn。

## 添加依赖

```bash
ngm add github:my-org/utils@v1.2.3 --ref-type=tag
ngm add github:my-org/logger@main --ref-type=branch
ngm add github:my-org/legacy@abc123def --ref-type=commit
ngm add github:my-org/monorepo@v2.0.0 --ref-type=tag --path=packages/core
ngm add local:kernel --ref-type=local-dir --source-path=./packages/kernel
```

`refType` 通常必填：`@main` 分不清是 tag 还是 branch；完整 40 位 commit hash 是唯一的窄默认。`--infer-ref-type` 用一次 `ls-remote` 消歧，两者都存在时报错。字段定义见[配置详解](./configuration.md#dependencies)。

| refType | 示例 | 锁定行为 |
|---|---|---|
| `commit` | `@abc123def` | 锁定该 commit；不可变 |
| `tag` | `@v1.2.3` | 解析为当前指向的 commit；tag 移动会报漂移 |
| `branch` | `@main` | 解析为当前 tip；正常前进是 `expected` |
| `local-git` | `local:pkg@v1.0.0` | 复用 commit 证明链，可完全离线 |
| `local-dir` | `local:kernel@auto` | 钉住目录内容摘要 `dirDigest` |

## 安装

```bash
ngm install
```

流程：读取 `ngm.json` → 解析 refType → commit / dirDigest → 计算内容 digest → vendor 落地 → 写 `ngm.lock` 与 `ngm.mappings.json`。术语见[术语表](./glossary.md)。

CI 的两个开关管不同的事：

```bash
ngm install --frozen-lockfile            # 管"装什么"：不解析新 ref、不改 lock
ngm install --offline                    # 管"怎么拿"：不联网，只用本地内容
ngm install --frozen-lockfile --offline  # 完全离线且可复现（需 lock 已提交）
```

frozen 遇到 lock 缺失或与 `ngm.json` 不一致时 exit 3；offline 本地资源未命中 exit 4。

## 验证与更新

```bash
ngm verify
ngm verify --content
ngm update github:my-org/utils
ngm update --all
ngm remove github:my-org/utils
```

`ngm update` 只接受依赖名，不接受 `<name>@<ref>`，也没有 `--ref-type`——refType 是声明层信息，只写在 `ngm.json`。想换 ref 就先改 `ngm.json` 再 `update`。`ngm verify` 的漂移三态（`expected` / `unexpected` / `critical`）与退出码见[CLI 参考](./cli.md)。

## vendor 与 mappings

`ngm.vendor/` 默认本地隔离（`vendor.mode: local`），也可设为 `global` 共享内容。是否提交由 Git 侧决定，ngm 不执行 `git add`。

`ngm install` 自动生成 `ngm.mappings.json`，把裸导入映射到 vendor 路径。它**必须提交**：同一份 lock 与同一个 `linkMode` 生成逐字节相同的文件，而多阶段镜像与 CI 的 build job 通常不跑 `ngm install`。构建工具接入见[构建指南](./build.md)。

## 传递依赖与 registry 共存

Git 依赖内部声明的 Git 依赖会被递归解析，并一起进入 lock 与 verify 范围。

registry 包与 ngm 依赖可以共存：`node_modules/` 归 pnpm，`ngm.vendor/` 归 ngm；把 Git 依赖用 `ngm add` 声明出来即可。ngm 不解析 `.npmrc`，不接管 `node_modules`；`ngm adopt` 只读扫描已装好的依赖并出具内容证明，不安装、不修改。

## 边界

- 不解析 semver 区间、不做版本求解、不做 workspace / hoisting。
- 不支持 git submodule（gitlink）与 LFS 实体内容，它们会被拒绝而不是被哈希成一个指针。
- `verify` 默认只比结构与大小，字节级保证要加 `--content`。
- `ngm gc` 的可达性只看当前项目的 lock：层 1/2 是全机共享的，另一个 checkout 仍可能需要被回收的对象（可重新拉取，但离线 CI 会失败）；global vendor 条目一律保留并 warning。

## 相关文档

- [配置详解](./configuration.md) · [构建](./build.md) · [CLI 参考](./cli.md)
- [依赖解析](../architecture/dependency-resolution.md) · [锁定机制](../architecture/locking.md) · [生态定位](./positioning.md)
