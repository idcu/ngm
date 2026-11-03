# scripts/release-dryrun.ps1 —— release-dryrun.sh 的 Windows 入口。
#
# 唯一实现是 scripts/release-dryrun.sh（构建 / 校验 / 版本注入只有一种写法）；
# 本文件只负责找到 Git for Windows 的 bash 并转调，退出码原样传回。
#
# 用法：powershell -NoProfile -File scripts/release-dryrun.ps1 [VERSION]
#   VERSION 可带可不带 v；省略时实现自己决定（.ngm-version 或开发版本）。
#   环境变量 TARGETS / BUILT 由实现读取，本入口不拦截（用户可以直接设置）。

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$Version
)

$ErrorActionPreference = 'Stop'

function Find-Bash {
    # 先找 Git for Windows 的 bash，再退到 PATH：PATH 上的 bash 可能是 WSL，
    # 它与 Windows 路径不互通（本项目的 .ps1 入口都用这一份查找顺序）。
    $candidates = @(
        (Join-Path $env:ProgramFiles "Git\bin\bash.exe"),
        (Join-Path $env:ProgramFiles "Git\usr\bin\bash.exe"),
        (Join-Path ${env:ProgramFiles(x86)} "Git\bin\bash.exe"),
        (Join-Path $env:LOCALAPPDATA "Programs\Git\bin\bash.exe")
    )
    foreach ($c in $candidates) {
        if ($c -and (Test-Path -LiteralPath $c)) { return $c }
    }
    $cmd = Get-Command bash -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    return $null
}

$bash = Find-Bash
if (-not $bash) {
    Write-Host "bash not found: the dry-run has exactly one implementation (scripts/release-dryrun.sh)."
    Write-Host "Install Git for Windows (it ships bash). Refusing to report success without running it."
    exit 2
}

$sh = Join-Path $PSScriptRoot "release-dryrun.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the dry-run has no implementation to delegate to."
    exit 2
}

$shArgs = @()
if ($Version) { $shArgs += $Version }

& $bash $sh @shArgs
exit $LASTEXITCODE
