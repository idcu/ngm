# Changelog

本文件是**发布说明的来源**：`release.yml` 只从对应版本的段落取文本，不再让
`--generate-notes` 按提交标题猜。段落标题必须写成 `## [<version>]`，`<version>`
是不带 `v` 的版本号（与 `ngm --version` 的第二列一致）。

只记录**用户可观察的行为**；逐版工程细节见 `docs/development/` 的计划与复盘。

## [0.102.0-dev.2] - 2026-11-03

开发版本。它与 `0.102.0-dev.1` 是同一批功能，差别只在**发布链路本身**：
dev.1 的发布 run 在当时的 CI 环境上失败，而 tag 指向的提交不可变；这一版把
发布前必须先绿的测试与本项目自己的跨平台判据修好，作为可下载的固定点。

### Fixed

- 预发布 tag（`vX.Y.Z-<suffix>`）不再被 tag ⇄ 复盘门禁要求配对复盘；
  附注 tag 与 `--published`（真的发布）仍然适用。
- `release-dryrun.sh` / `release-notes.sh` 增加 Windows 入口；dry-run 进入 CI
  （ubuntu 跑 `.sh`，windows 跑 `.ps1`），每次推送都真构建、真校验和、真读版本。
- 十个 `.ps1` 入口在 POSIX 上不再因 `Join-Path $null` 直接失败；macOS 自带
  bash 3.2 与 BSD `sort` 的兼容问题修掉（`globstar` → `find`，`sort -V` → 数值排序）。
- `go test` 在 Windows 显式走 bash：默认 PowerShell 下 `set -o pipefail` 是
  找不到的命令，测试根本没跑却被记成失败。
- 反斜杠形态的 symlink 目标在所有平台都按分隔符判定；夹具的
  `core.quotePath` 固定为 false，非 ASCII 文件名不再是平台相关的读数。
- 回填工作流只做编译冒烟（历史 tag 当年已过 CI），发布前仍强制校验
  `SHA256SUMS`、版本注入与版本化别名。

### Notes

- 其余历史 tag 不回填，见 `docs/guides/release-pipeline.md` 的范围与理由。

## [0.102.0-dev.1] - 2026-11-03

开发版本，供 `ngm-repo` 的集合校验固定使用（`.ngm-version`）。

### Added

- `ngm verify` 的报告新增 `notes`：选择性 verify 裁剪 `requiredBy` 时，
  人读与 `--json` 都会说明丢掉了多少条边。
- `ngm tree` 优先读取 `ngm.lock` 的 `requiredBy` 图，并在输出与 `--json` 的
  `source` 里标明来源；旧 lock 才回退重新解析。
- 发布流水线：`v*` tag 触发一次构建，产出六个平台 × 两个架构的二进制、
  版本化别名与 `SHA256SUMS`；发布说明来自本文件的对应段落。

### Changed

- `ngm.json` / `ngm.engines.json` / `~/.ngm/config.json` 的读取器接受
  `schemaVersion` 1..2（写入仍为 1）；`ngm modules validate` 复用
  `ProjectFile.Validate`，生产方与消费方使用同一套判据。
- 默认 Git 协议统一为 `ssh`，与内置默认和 `ngm config show` 一致。
- SBOM / attestation 的 purl 按 host 分流为 `pkg:github` / `pkg:gitlab` /
  `pkg:gitee`，自建 host 使用 `pkg:generic?vcs_url=...`；CycloneDX 的
  `bom-ref` 保留稳定的旧形态。
- 资源缺失（未安装、冷 mirror、content store 缺条目）在 `verify` 中记为
  operational，退出码 4；只有已确证的内容不符才是 critical（退出码 2）。

### Fixed

- 选择性 `ngm verify <dep>` 不再对子集外的父节点报“幽灵边”。
- 重复根声明在 `config validate` 与解析阶段都被拒绝，并点名两条声明。
- `verify --json` 的 `summary` 不再出现 `critical: 0` 而 `exitCode: 2` 的自相矛盾。
- `tree`、`typecheck` 的帮助文本与实现对齐；文档中的 lock 版本、`--json`
  命令数、Go 版本、Action 退出码列表同步更新。
- 9 项输入/边界硬化：cat-file 对象上限、`ContentStore.Has` 验 blob、
  `css --outfile`、`transform` stdin 上限、严重度顺序、literal pathspec、
  integrations 临时文件、`modules index` 当前分支、`collectSource` 警告。

## [0.101.0] - 2026-10-30

### Changed

- 把三条对外承诺实测为**不成立**，并把它们写成可判别的状态，而不是继续
  用一句更强的结论掩盖：沙箱的模块加载、依赖对沙箱配置的影响、安装时的
  大小写冲突。
- 五个“输出正常、结论错”的缺陷被修正：`attest` 的双读、Git 签名把执行失败
  说成“不可信”、`mappings` 入口可逃出 vendor、`why` 的搜索上界、`atomicfile`
  的目录同步。
- `verify` 把“权限被拒”归为 operational，而不是 `unexpected` 漂移。
- 新增三个可匿名访问的 Gitee 夹具，并把生成器固定为确定性输出。

## [0.100.0] - 2026-10-21

### Added

- `ngm store usage` 报告层 1（mirror）：总体积、packed history、残骸，并点名
  “一个 ref 都没有”的目录。
- `ngm store gc [--dry-run]`：对每个可用 mirror 执行 `git gc`，不触网、不删内容。
- `store prune` 追加层 1 的临时包清理，年龄门槛 1 小时。

### Changed

- ADR-030 把“mirror 仍是全量历史”写成决定，并拒绝 `--single-branch` /
  部分克隆，理由是 refType 解析与离线 digest 重放。

## [0.99.0] - 2026-10-14

### Added

- 承诺账本：把仓库里 14 处“必须一致 / 逐字镜像 / 两份实现”一类承诺逐条登记，
  每条必须指向一个真实存在的判据；未登记的新承诺会让判据变红。

### Fixed

- adapter 的 `entry.Args` 被前置两次（注释还声称“两者必须一致”）。
- 两处以“由 CLI 的集成测试保证”为理由、实际上没有任何判据的承诺，补上了机械检查。
