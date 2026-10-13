# scripts/check-mirror-coverage.ps1 —— 镜像覆盖读数的 Windows 入口。
#
# **唯一实现是 scripts/check-mirror-coverage.sh**；本文件只是转调它
# （与 check-library-standard.ps1 同一条纪律：要跑的东西只允许有一种写法——
# 两份实现会漂移，而漂移的代价是"你以为读到的数是同一个"）。
#
# 用法：powershell -NoProfile -File scripts/check-mirror-coverage.ps1 [-Repo OWNER/NAME]
#   需要 bash（Git for Windows 自带）+ curl。找不到 bash 时**明确失败**（exit 2）——
#   没有"降级成不做事"这种路径。

[CmdletBinding()]
param(
    [string]$Repo,
    # --api-base 是实现的选项（"指一个本地假 API"，只为判据存在）——入口必须一并转发，
    # 否则 Windows 上就**没法**用它做同样的验证（v0.90 实测到的第一处"入口吞参数"）。
    [string]$ApiBase
)

$ErrorActionPreference = 'Stop'

function Find-Bash {
    # 先找 Git for Windows 的 bash，再退到 PATH 上的 bash：PATH 里可能是 WSL 的
    # `C:\Windows\System32\bash.exe`，它与 Windows 路径不互通。
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
    Write-Host "bash not found: this reading has exactly one implementation (scripts/check-mirror-coverage.sh)."
    exit 2
}

$sh = Join-Path $PSScriptRoot "check-mirror-coverage.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the reading has no implementation to delegate to."
    exit 2
}

# 变量名用 $shArgs（不用 $args：那是 PowerShell 的自动变量，赋值会掩盖真正的未绑定参数）。
$shArgs = @()
if ($Repo) { $shArgs += @('--repo', $Repo) }
if ($ApiBase) { $shArgs += @('--api-base', $ApiBase) }

& $bash $sh @shArgs
exit $LASTEXITCODE
