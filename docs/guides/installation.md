# 安装指南

> 本页的命令与输出示例取自实际运行，并由 CI 的快照测试守护。
> **预编译二进制已随 `v0.1.0` ~ `v0.4.0` 发布**：每个版本六个平台 + `SHA256SUMS`，
> **GitHub 与 Gitee 两个源都可取到**（附件逐个按 `SHA256SUMS` 校验过）。
> 国内下载优先用「方式一」里的 Gitee 发行版。

---

## 前置要求

| 工具 | 版本要求 | 说明 |
|------|---------|------|
| Go | >= 1.22（建议最新稳定版） | 编译 ngm（ngm 本身运行时不需要 Go）。注意：过旧的工具链构建出的二进制在较新的 macOS 上可能被内核拒绝加载（`missing LC_UID Load Command`），报 `signal: abort trap` |
| Git | >= 2.30 | 拉取依赖 |
| Node.js | >= 22.0.0 | 用户项目运行时（可选，Deno 项目不需要） |
| Deno | >= 2.0 | 用户项目运行时（可选，Node 项目不需要） |

---

## 安装方式

### 方式一：下载预编译二进制

> **先选下载源**：
>
> | 你的网络 | 用哪个 | 地址前缀 | 目前最新可取 |
> |---------|--------|---------|------------|
> | 国内 | **Gitee 发行版**（推荐） | `https://gitee.com/idcu/ngm/releases/download/v0.4.0` | **v0.4.0** |
> | 海外 / 想取最新 | GitHub Release（上游） | `https://github.com/idcu/ngm/releases/latest/download` | **v0.60.0** |
>
> 两个源的产物**字节完全相同**，共用同一份 `SHA256SUMS`，可以互相校验。
> Gitee 的附件在每次发布后**手动**上传，因此只有**版本固定**地址（Gitee 没有 `latest/download` 形态）；
> 若该地址 404，说明这一版的附件还没上传——改用 GitHub 或「方式二」。
>
> ✅ **当前可取性（2026-10-04 直接问两个源的 API 得到；Gitee 侧可用
> `bash scripts/check-gitee-release-status.sh` 随时复跑；Windows 用同名 `.ps1`，它转调同一个实现）**：
>
> | 发行源 | 有附件的版本 |
> |--------|------------|
> | **GitHub** | `v0.1.0` ~ `v0.4.0`、**`v0.9.0`** ~ **`v0.60.0`**（各 7 个附件） |
> | **Gitee** | `v0.2.0` ~ `v0.4.0`（各 7 个附件 + 2 个源码包；`SHA256SUMS` 已与 GitHub 逐字节比对） |
>
> ⚠️ **空缺有十九处，且补发已决定暂缓（2026-10-04，项目所有者决定）**——
> **有意延后，不是遗漏**。成因是凭据：
> ①`v0.5.0` ~ `v0.8.0` 的 GitHub release 未生成（镜像转发 tag 未触发发布工作流，
> 补发见[发布清单](../development/README.md#发布清单每个版本)第 2 步）；
> ② Gitee 侧缺 **`v0.1.0` 及 `v0.5.0` ~ `v0.60.0` 共五十七个版本**（需 `GITEE_TOKEN`）。
> 判据与恢复信号写在[发布清单](../development/README.md#发布清单每个版本)开头：
> 缺口涉及的版本**功能全在源码里**（下面的「方式二」），而最新的 `v0.12.0` 在 GitHub 上可取，
> 所以现在没有人在等这些二进制。**一旦有人要用 Gitee 取到 `v0.4.0` 之后的版本，它就值得恢复。**
>
> **换句话说：想要最新的产物请从 GitHub 取**（`v0.60.0`）；只在 Gitee 上取的话，
> 最新是 `v0.4.0`——而 `v0.5` ~ `v0.57` 的功能（含 `store usage`、`store prune --orphans`、
> 层 2 换布局）**都在源码里已交付**，用「方式二」编译即可拿到。

产物命名统一为 **`ngm-<os>-<arch>[.exe]`**，`<os>` / `<arch>` 取 Go 的 `GOOS` / `GOARCH`：

| 平台 | 文件名 |
|------|--------|
| Linux x86-64 | `ngm-linux-amd64` |
| Linux ARM64 | `ngm-linux-arm64` |
| macOS Intel | `ngm-darwin-amd64` |
| macOS Apple silicon | `ngm-darwin-arm64` |
| Windows x86-64 | `ngm-windows-amd64.exe` |
| Windows ARM64 | `ngm-windows-arm64.exe` |

> 命名刻意不依赖 `uname`：`uname -m` 给的是 `x86_64` / `aarch64`，与 Go 的 `amd64` / `arm64`
> 并不一致（且 macOS 的 `arm64` 与 Linux 的 `aarch64` 同名不同字），靠它拼出的下载地址会 404。
> 用 `GOOS`/`GOARCH` 与 Go 生态其余工具保持一致。

```bash
# 先把 BASE 换成你选的下载源前缀：
#   Gitee（国内推荐）: BASE=https://gitee.com/idcu/ngm/releases/download/v0.4.0
#                      （Gitee 侧最新有附件的版本就是 v0.4.0，原因见上面的空缺说明）
#   GitHub（上游）   : BASE=https://github.com/idcu/ngm/releases/latest/download
#                      （想钉版本就用 .../releases/download/v0.60.0）

# macOS (Apple silicon)
curl -L "$BASE/ngm-darwin-arm64" -o /usr/local/bin/ngm && chmod +x /usr/local/bin/ngm

# Linux (x86-64)
curl -L "$BASE/ngm-linux-amd64" -o /usr/local/bin/ngm && chmod +x /usr/local/bin/ngm
```

Windows（PowerShell）：

```powershell
# 国内用 Gitee；换成 GitHub 时改这一行即可
$BASE = "https://gitee.com/idcu/ngm/releases/download/v0.4.0"
Invoke-WebRequest -Uri "$BASE/ngm-windows-amd64.exe" -OutFile "$env:LOCALAPPDATA\ngm\ngm.exe"
```

每个 release 都附带 `SHA256SUMS`（每行 `<hex>  <文件名>`，上表六个产物全部在内）。
**两个下载源用的是同一份校验文件**，所以从哪里下载都一样：

```bash
sha256sum -c --ignore-missing SHA256SUMS      # Linux
shasum -a 256 -c --ignore-missing SHA256SUMS  # macOS
```

### 方式二：从源码编译

```bash
git clone https://gitee.com/idcu/ngm.git
cd ngm
go build -o ngm ./cmd/ngm
mv ngm /usr/local/bin/
```

> 主仓在 Gitee（国内可直连）；GitHub 镜像为 `https://github.com/idcu/ngm.git`，两者内容与 tag 一致（tag 由主仓镜像推送）。

### 方式三：包管理器（未来）

```bash
# Homebrew（规划中）
brew install ngm

# Scoop（规划中）
scoop install ngm

# npm（规划中，仅分发二进制）
npm install -g @ngm/cli
```

---

## 验证安装

```bash
ngm --version
# ngm 0.1.0 (git:abc1234, built: 2026-09-29T10:00:00Z)

ngm --help
# ngm — Git-first Dependency Provenance Layer
#
# USAGE:
#   ngm <command> [options]
#
# COMMANDS:
#   init           初始化项目
#   add            添加依赖
#   install        解析并安装依赖
#   update         更新依赖 ref
#   remove         移除依赖
#   verify         检查 ref 漂移与 digest 重放
#   audit          供应链审计（OSV.dev，v0.2）
#   why            为什么装了这个依赖
#   tree           依赖树可视化
#   outdated       检查新版本
#   typecheck      类型检查（adapter）
#   typedecl       生成 .d.ts 声明（adapter）
#   build          构建（adapter）
#   css            CSS 编译（adapter）
#   mappings       mappings 管理
#   integrations   构建工具集成脚手架（Vite / esbuild / Deno / Webpack）
#   cache          缓存维护
#   config         配置管理
#   engines        引擎管理
#
# FLAGS:
#   --version      输出版本信息（含 git 短哈希与构建时间）
#   --help, -h     输出本帮助
```

`--version` 的三个字段都由构建时注入（`-ldflags -X`）：`git:` 是**短**哈希，`built:` 是
构建时刻（RFC3339 UTC）。本地未经注入的构建输出 `ngm 0.0.0-dev (git:dev, built: unknown)`——
CI 用快照测试守护这份输出，任何格式漂移都会在 PR 阶段被拦下。

---

## 选择宿主运行时

ngm 不替你选运行时。初始化时指定：

```bash
# Node.js 项目
ngm init github.com/my-org/my-app --runtime=node

# Deno 项目
ngm init github.com/my-org/my-app --runtime=deno
```

`<name>` 是项目标识（推荐 Git URL 形式，写入 `ngm.json` 的 `name` 字段）；也可先用目录名，之后在 ngm.json 中补全。这会生成对应的 `ngm.json` 模板。

> Deno 版本注意：作为**用户项目运行时** Deno >= 2.0 即可；若使用 `deno bundle` 作为构建引擎，需要 Deno 2.4+（实验特性），2.0–2.3 请改用 esbuild adapter。

---

## 相关文档

- [快速上手](./quickstart.md)
- [配置详解](./configuration.md)
- [Node vs Deno](./node-vs-deno.md)
