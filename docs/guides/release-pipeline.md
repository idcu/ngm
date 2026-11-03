# 发布流水线

> 规范来源：`.github/workflows/release.yml`、`scripts/build-release.sh`、
> `scripts/release-notes.sh`、`CHANGELOG.md`。本页解释**为什么这样发**，
> 命令与脚本才是事实源。

---

## 1. 现状实测（2026-11-03）

| 读数 | GitHub | Gitee |
|---|---|---|
| 正式 tag | 101（`v0.1.0` … `v0.101.0`） | 101（镜像转发） |
| 预发布 tag | 2（`v0.102.0-dev.1` 试跑失败；`v0.102.0-dev.2` 为下游固定点） | 2（镜像转发） |
| 有 release 的版本 | **60** 个正式版 + **1** 个预发布（`v0.102.0-dev.2`） | **3**（`v0.2.0` … `v0.4.0`） |
| 单个 release 的附件 | 7（旧版：六个平台二进制 + `SHA256SUMS`）；新版 13（另带六个版本化别名） | 9（旧版 7 个 + Gitee 自动附的两个源码包） |
| 最新有 release 的版本 | `v0.64.0`（正式）；`v0.102.0-dev.2`（预发布） | `v0.4.0` |

缺口是 **41 个**：`v0.5.0` … `v0.8.0`（4 个）与 `v0.65.0` … `v0.101.0`（37 个）。
GitHub 的 60 个 release 覆盖 `v0.1.0` … `v0.4.0` 与 `v0.9.0` … `v0.64.0`。
Gitee 的 3 个是 `v0.2.0` / `v0.3.0` / `v0.4.0`，`v0.1.0` 没有发行版。

读数来源：`api.github.com/repos/idcu/ngm/releases?per_page=100` 与
`gitee.com/api/v5/repos/idcu/ngm/releases?per_page=100`。**`gh` 在本机不存在**，
因此没有用 `gh release list`；上面两个 API 是匿名可读的，返回的 tag 与附件名
可与本地 101 个 tag 逐项比对。

---

## 2. 回填范围：只补四版

**补**：

| tag | 理由 |
|---|---|
| `v0.101.0` | 最新正式版 |
| `v0.100.0` | 最近三个正式版之一 |
| `v0.99.0` | 最近三个正式版之一 |
| `v0.102.0-dev.2` | `ngm-repo` 的 `.ngm-version` 钉住的开发版；与 dev.1 同一批功能，发布点因为 dev.1 的 tag 提交不可变而顺延 |

**不补**：其余 98 个正式 tag（101 − 3；预发布 tag 不计入正式版）。理由不是"永远不做"，而是**现在没有人在等那些二进制**：
它们的功能都在源码里，任何人都能按 tag 构建；而补发需要 GitHub/Gitee 写权限，
以及每版一次完整构建。这个决定写进 [发布清单](../development/README.md) 的历史补发段，
不假装"全部补齐"。

执行入口：`.github/workflows/backfill.yml`（`workflow_dispatch`，默认四个 tag）。
它用**当前分支**的构建脚本与 CHANGELOG，但 `go build` 站在各自 tag 的源码树里，
因此产物是那一版的代码，不是当前 HEAD 的代码。

---

## 3. 一个 tag 的流水线

`push: tags: ['v*']` 或手动 `workflow_dispatch`（输入已存在的 tag）：

1. **定 tag**：`v` 前缀校验；手动触发与 tag 推送走同一段解析。
   含 `-` 后缀的 tag（如 `v0.102.0-dev.1`）是**预发布**：release 标为 prerelease，
   且在 `check-release-status.sh` 里**不要求复盘**——它不标记"一个版本交付了"，
   而是给下游（ngm-repo 的 `.ngm-version`）一个可直接下载的固定点。
2. **checkout 该 tag**，`fetch-depth: 0`（短哈希与 tag 提交时间需要历史）。
3. **先跑全量测试**（`go test -count=1 -timeout 40m ./...`）。
4. **一次构建六个目标**：linux/darwin/windows × amd64/arm64，`CGO_ENABLED=0`
   （静态链接优先），`-trimpath -buildvcs=false`，`-ldflags -X` 注入
   `Version` / `GitCommit` / `BuildTime`。
   - `Version` = tag 去掉 `v`；
   - `BuildTime` = **tag 提交时间**（不是当前时间），同一 tag 重建只差 toolchain。
5. **产物命名**：
   - `ngm-<os>-<arch>[.exe]`：既有约定，安装指南与旧脚本按它取；
   - `ngm-<version>-<os>-<arch>[.exe]`：版本化别名，按 `.ngm-version` 直接拼地址。
   两个名字来自**同一次构建**（复制别名），`cmp` 在发布前验证字节相同。
6. **`SHA256SUMS`**：每个产物一行 `<hex>  <文件名>`，两空格；`sha256sum -c` 可消费。
7. **发布前验证**：`sha256sum -c SHA256SUMS`、`ngm-linux-amd64 --version` 含注入版本、
   版本化别名与既有名字字节相同。任一失败则不发布。
8. **发布说明**：`scripts/release-notes.sh <tag>` 从 `CHANGELOG.md` 的
   `## [<version>]` 段取文本；**缺失即 exit 3**，不会回退到按提交标题生成。
9. **GitHub release**：存在则 `edit + upload --clobber`，不存在则 `create`；
   绝不创建第二份 release。

---

## 4. Gitee 通道

GitHub 的同一批字节由 `scripts/upload-gitee-assets.ps1` 下载、按 `SHA256SUMS`
逐个校验后再上传到 Gitee。不是第二套构建。

- 默认关闭：仓库变量 `GITEE_UPLOAD=true` 且 secrets 里有 `GITEE_TOKEN` 时才运行。
- 没有 token 时 job 不运行，也不会报"双通道已发布"。
- 该脚本上传的是 **7 个既有名字**（Gitee 侧检查同样按这 7 个判定），
  版本化别名不上传；Gitee 下载地址仍按 release tag 取既有名字。

---

## 5. 本地 dry-run

```bash
# 版本取 .ngm-version，目标取 go env；只构建当前平台
bash scripts/release-dryrun.sh

# 指定版本/目标
bash scripts/release-dryrun.sh v0.101.0
TARGETS="linux/amd64" bash scripts/release-dryrun.sh
```

它真验：构建、版本注入、`sha256sum -c`、版本化别名与既有名字 `cmp`。
它**不**上传、不下载、不碰凭据。上传只有 `release.yml` / `backfill.yml` 在
有 `GITHUB_TOKEN`（以及可选 `GITEE_TOKEN`）时做。

---

## 6. 需要什么凭据

| 动作 | 凭据 | 没有时 |
|---|---|---|
| GitHub release 创建/附件上传 | Actions 自动的 `GITHUB_TOKEN`（`contents: write`） | workflow 无法运行；本地不能匿名创建 release |
| Gitee release/附件 | `GITEE_TOKEN`（releases 权限） | `gitee-assets` job 跳过；不声称已发布 |
| 触发 backfill | GitHub 写权限（dispatch workflow） | 只能本地构建产物，不能补 release |

**本轮本地环境**：没有 `gh`，没有 `GITHUB_TOKEN`，没有 `GITEE_TOKEN`。
因此 dry-run 与本地构建可执行，**上传没有执行，也不写成成功**。
