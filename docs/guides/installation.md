# 安装指南

ngm 是单个 Go 二进制，运行时只依赖 Git。本文给出安装方式、版本前置与帮助输出。

## 前置要求

| 工具 | 版本要求 | 说明 |
|------|---------|------|
| Go | >= 1.25（`go.mod` 的 `go 1.25.0`） | 编译 ngm 时需要；运行 ngm 不需要 |
| Git | >= 2.30 | 拉取依赖、离线重放对象 |
| Node.js | >= 22.0.0 | 用户项目运行时（可选，Deno 项目不需要） |
| Deno | >= 2.0 | 用户项目运行时（可选，Node 项目不需要） |

## 从源码编译

```bash
git clone https://gitee.com/idcu/ngm.git
cd ngm
go build -o ngm ./cmd/ngm
mv ngm /usr/local/bin/
ngm --version
```

Windows 用 `go build -o bin\ngm.exe .\cmd\ngm`。主仓在 Gitee，GitHub 镜像为 `https://github.com/idcu/ngm.git`，两者内容一致。

未注入版本信息的本地构建输出 `ngm 0.0.0-dev (git:dev, built: unknown)`；发布构建通过 `-ldflags -X` 注入版本、git 短哈希与构建时间。

## 下载预编译二进制

产物命名统一为 `ngm-<os>-<arch>[.exe]`，`<os>` / `<arch>` 取 Go 的 `GOOS` / `GOARCH`：

| 平台 | 文件名 |
|------|--------|
| Linux x86-64 | `ngm-linux-amd64` |
| Linux ARM64 | `ngm-linux-arm64` |
| macOS Intel | `ngm-darwin-amd64` |
| macOS Apple silicon | `ngm-darwin-arm64` |
| Windows x86-64 | `ngm-windows-amd64.exe` |
| Windows ARM64 | `ngm-windows-arm64.exe` |

下载源：GitHub Release 的 `https://github.com/idcu/ngm/releases/latest/download`，或 Gitee 发行版的固定版本地址 `https://gitee.com/idcu/ngm/releases/download/<tag>`（Gitee 没有 `latest/download` 形态）。

```bash
# 先把 BASE 换成你选的下载源前缀
curl -L "$BASE/ngm-linux-amd64" -o /usr/local/bin/ngm && chmod +x /usr/local/bin/ngm
```

Windows（PowerShell）：

```powershell
$BASE = "https://github.com/idcu/ngm/releases/latest/download"
Invoke-WebRequest -Uri "$BASE/ngm-windows-amd64.exe" -OutFile "$env:LOCALAPPDATA\ngm\ngm.exe"
```

每个 release 附带 `SHA256SUMS`（每行 `<hex>  <文件名>`）。两个源产物字节相同，共用同一份校验文件，下载后务必校验：

```bash
sha256sum -c --ignore-missing SHA256SUMS      # Linux
shasum -a 256 -c --ignore-missing SHA256SUMS  # macOS
```

预编译产物可能落后于源码；需要当前代码的能力时优先从源码编译。

## 验证安装：`--help`

```bash
ngm --version
ngm --help
```

帮助输出中的命令与 flag 如下（与运行时逐字一致）：

```text
# COMMANDS:
#   init           初始化项目
#   adopt          为**已安装**的依赖（npm/pnpm 装的）出具内容证明：不接管安装，只描述磁盘上的字节
#   add            添加依赖
#   install        解析并安装依赖
#   run            运行项目脚本（package.json 权威、ngm.json 回退；在沙箱外执行，需 run:script 授权）
#   update         更新依赖 ref
#   migrate        把 v1/v2 清单与 2.x lock 迁移到 v3（--dry-run 可预演）
#   remove         移除依赖
#   modules        模块集合的发现与校验
#   doctor         诊断本地环境与项目状态
#   verify         检查 ref 漂移与 digest 重放
#   audit          供应链审计（OSV.dev，v0.2）
#   why            为什么装了这个依赖
#   tree           依赖树可视化
#   outdated       检查新版本
#   typecheck      类型检查（adapter）
#   typedecl       生成 .d.ts 声明（adapter）
#   build          构建（adapter）
#   transform      单文件转换（adapter）
#   css            CSS 编译（adapter）
#   mappings       mappings 管理
#   integrations   构建工具集成脚手架（v0.3）
#   cache          缓存维护
#   store          ngm 两层的磁盘占用报告与回收（mirror + content store，见 ADR-018/023/030）
#   gc             按当前项目的 lock 回收 store/mirror/vendor 里不可达的对象（默认 dry-run，见 ADR-041）
#   config         配置管理
#   engines        引擎管理
#   sbom           导出 SBOM（CycloneDX / SPDX，v0.69）
#   attest         导出 in-toto / SLSA v1 语句（未签名，v0.70）
#
# FLAGS:
#   --version      输出版本信息（含 git 短哈希与构建时间）
#   --help, -h     输出本帮助
```

`ngm <command> --help` 打印该命令自己的用法——就是它参数错误时看到的那份文本。`--help` 写在子命令**之前**才是根级；其他根级 flag 会被拒绝（exit 3），命令的 flag 要写在命令之后。

## 选择宿主运行时

ngm 不替你选运行时。初始化时指定：

```bash
ngm init github.com/my-org/my-app --runtime=node
ngm init github.com/my-org/my-app --runtime=deno
```

`<name>` 是项目标识，推荐 Git URL 形式，写入 `ngm.json` 的 `name` 字段。

Deno 版本注意：作为**用户项目运行时** Deno >= 2.0 即可；若使用 `deno bundle` 作为构建引擎，需要 Deno 2.4+（实验特性），2.0–2.3 请改用 esbuild adapter。Deno 不在内置引擎清单里，需自行声明，声明方法见[配置详解](./configuration.md)。

## 为什么这样设计

- 用 `GOOS` / `GOARCH` 命名而不是 `uname -m`：后者给的是 `x86_64` / `aarch64`，拼出的下载地址通常 404。
- 交付物必须可校验：`SHA256SUMS` 是单一事实源，两个下载源共用，校验命令与平台无关。
- 帮助文本是命令用法的一部分：`ngm <command> --help` 与参数错误输出同一份内容，避免"两份帮助各自漂移"。

## 相关文档

- [快速上手](./quickstart.md) · [CLI 参考](./cli.md)
- [配置详解](./configuration.md) · [生态定位](./positioning.md)
