// Package main 是 ngm CLI 入口。子命令文件与本目录并列：init.go、config.go 等。
//
// 实现纪律（来自 modules/p0-core.md 与 guides/cli.md）：
//   - 单一二进制入口；第一个非 flag 参数 = 子命令
//   - 标准库 flag（不引 cobra）
//   - 错误统一走 errs.NgmError，退出码对齐 observability.md
//   - --version / --help 在所有子命令上下文可用
package main

import "github.com/idcu/ngm/internal/version"

// rootUsage 是 `ngm --help` 输出。对齐 installation.md "验证安装"小节。
//
// 命令表必须与 guides/cli.md 保持一致：新增子命令时同时改文档与本字符串。
const rootUsage = `ngm — Git-first Dependency Provenance Layer

USAGE:
  ngm <command> [options]

COMMANDS:
  init           初始化项目
  add            添加依赖
  install        解析并安装依赖
  update         更新依赖 ref
  remove         移除依赖
  verify         检查 ref 漂移与 digest 重放
  audit          供应链审计（OSV.dev，v0.2）
  why            为什么装了这个依赖
  tree           依赖树可视化
  outdated       检查新版本
  typecheck      类型检查（adapter）
  typedecl       生成 .d.ts 声明（adapter）
  build          构建（adapter）
  transform      单文件转换（adapter）
  css            CSS 编译（adapter）
  mappings       mappings 管理
  integrations   构建工具集成脚手架（v0.3）
  cache          缓存维护
  store          内容寻址 store 的占用报告与回收（残骸 + 无人引用的 blob，见 ADR-018/023）
  config         配置管理
  engines        引擎管理

FLAGS:
  --version      输出版本信息（含 git 短哈希与构建时间）
  --help, -h     输出本帮助；子命令形式 ngm <command> --help 输出该命令自己的帮助
`

// versionLine 是 `ngm --version` 输出的一行，对齐 installation.md 示例：
// `ngm 0.1.0 (git:abc1234, built: 2026-09-29)`。
func versionLine() string {
	return "ngm " + version.String()
}
