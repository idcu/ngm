# scripts/check-release-status.ps1 —— "已交付/已发布"成对检查的 Windows 入口。
#
# **唯一实现是 scripts/check-release-status.sh**；本文件只是转调它
# （与 check-library-standard.ps1 同一条纪律：要检查的规则只允许有一种写法——
# 两份实现会漂移，而漂移的代价是"你以为检查过了"）。
#
# 用法：powershell -NoProfile -File scripts/check-release-status.ps1 [-Published] [-Repo OWNER/NAME]
#   无参数       离线三条（复盘 ⇄ tag 的对称 + 附注 tag）——与 CI 的门禁同一份实现。
#   -Published   **额外**去 GitHub 问一句：每个 tag 到底有没有 release、资产齐不齐（需要网络与 curl）。
#
# 退出码（与实现一致）：0 = 全部就绪；1 = 有缺；3 = 查不动（网络/限额，不判缺也不报成功）。
#   需要 bash（Git for Windows 自带）。找不到 bash 时**明确失败（exit 2）**——
#   没有"降级成不做事"这种路径。

[CmdletBinding()]
param(
    [switch]$Published,
    [string]$Repo
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
    Write-Host "bash not found: this check has exactly one implementation (scripts/check-release-status.sh)."
    Write-Host "Install Git for Windows (it ships bash) or run the .sh yourself. Refusing to"
    Write-Host "report success without running the real check."
    exit 2
}

$sh = Join-Path $PSScriptRoot "check-release-status.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the check has no implementation to delegate to."
    exit 2
}

$shArgs = @()
if ($Published) { $shArgs += '--published' }
if ($Repo) { $shArgs += @('--repo', $Repo) }

& $bash $sh @shArgs
exit $LASTEXITCODE
