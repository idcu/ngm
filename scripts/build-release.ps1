# Windows 入口：转调 scripts/build-release.sh（唯一实现）。
#
# v0.136：此前这里是 .sh 的**完整第二实现**（自己 ldflags / 命名 / SHA256SUMS），
# 无任何 CI 调用，只在注释里出现——"run-or-explain"判据因此放行，而两份实现会漂移。
# 现在与其余 11 对 .sh/.ps1 一致：唯一实现是 .sh，这里只做参数转发。
#
# 用法：
#   powershell -File scripts/build-release.ps1 -Version 0.102.0-dev.2
#   pwsh -File scripts/build-release.ps1 -Version 0.1.0 -Targets linux/amd64
param(
    [string]$Version = "",
    [string]$Commit = "",
    [string]$Built = "",
    [string]$Out = "",
    [string[]]$Targets = @(),
    [switch]$NoVersionedNames
)

$ErrorActionPreference = "Stop"
$script = Join-Path $PSScriptRoot "build-release.sh"

# bash 必须在 PATH 上：CI 与本机都用 Git Bash / MSYS bash 跑 .sh。
# 只在本入口进程里设置环境变量（子进程继承），不给调用方的 shell 留下状态。
if ($Version) { $env:VERSION = $Version }
if ($Commit) { $env:COMMIT = $Commit }
if ($Built) { $env:BUILT = $Built }
if ($Out) { $env:OUT = $Out }
if ($Targets.Count -gt 0) { $env:TARGETS = ($Targets -join ' ') }
if ($NoVersionedNames) { $env:VERSIONED_NAMES = "0" }

& bash $script
$rc = $LASTEXITCODE

# 清理本入口设置过的变量，避免污染后续同进程调用
foreach ($name in @("VERSION", "COMMIT", "BUILT", "OUT", "TARGETS", "VERSIONED_NAMES")) {
    Remove-Item "Env:$name" -ErrorAction SilentlyContinue
}

if ($rc -ne 0) { throw "build-release.sh failed (exit $rc)" }
