# scripts/gitee-release.ps1 —— gitee-release.sh 的 Windows 入口。
#
# 唯一实现是 scripts/gitee-release.sh（API 调用、校验与幂等只有一种写法）；
# 本文件只转发参数并把退出码原样传回。
#
# 用法：powershell -NoProfile -File scripts/gitee-release.ps1 -Tag v0.99.0 -Assets dist [-DryRun]
#       powershell -NoProfile -File scripts/gitee-release.ps1 -Range v0.99.0..v0.101.0 -Assets builds

[CmdletBinding()]
param(
    [string[]]$Tag = @(),
    [string]$Range,
    [string]$Assets,
    [string]$Repo,
    [string]$ApiBase,
    [string]$TargetBranch,
    [switch]$Help,
    [switch]$DryRun,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'

function Find-Bash {
    # 字符串拼接而非 Join-Path：Linux/macOS 上 $env:ProgramFiles 不存在，
    # Join-Path 会因 $null 抛错（$ErrorActionPreference='Stop'）。
    $candidates = @(
        "$env:ProgramFiles\Git\bin\bash.exe",
        "$env:ProgramFiles\Git\usr\bin\bash.exe",
        "${env:ProgramFiles(x86)}\Git\bin\bash.exe",
        "$env:LOCALAPPDATA\Programs\Git\bin\bash.exe"
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
    Write-Host "bash not found: this uploader has exactly one implementation (scripts/gitee-release.sh)."
    Write-Host "Install Git for Windows (it ships bash). Refusing to report success without running it."
    exit 2
}

$sh = Join-Path $PSScriptRoot "gitee-release.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the uploader has no implementation to delegate to."
    exit 2
}

$shArgs = @()
foreach ($t in @($Tag)) { if ($t) { $shArgs += @('--tag', $t) } }
if ($Range) { $shArgs += @('--range', $Range) }
if ($Assets) { $shArgs += @('--assets', $Assets) }
if ($Repo) { $shArgs += @('--repo', $Repo) }
if ($ApiBase) { $shArgs += @('--api-base', $ApiBase) }
if ($TargetBranch) { $shArgs += @('--target-branch', $TargetBranch) }
if ($DryRun) { $shArgs += '--dry-run' }
if ($Force) { $shArgs += '--force' }
if ($Help) { $shArgs += '--help' }

& $bash $sh @shArgs
exit $LASTEXITCODE
