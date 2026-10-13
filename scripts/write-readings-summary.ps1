# scripts/write-readings-summary.ps1 —— 读数摘要的 Windows 入口。
#
# **唯一实现是 scripts/write-readings-summary.sh**；本文件只是转调它
# （与 check-library-standard.ps1 同一条纪律：要跑的东西只允许有一种写法——
# 两份实现会漂移，而漂移的代价是"你以为看到的读数是同一个"）。
#
# 用法：powershell -NoProfile -File scripts/write-readings-summary.ps1 [-Repo OWNER/NAME] [-ApiBase URL] [-Out FILE]
#   需要 bash（Git for Windows 自带）。找不到 bash 时**明确失败**（exit 2）——
#   没有"降级成不做读数"这种路径。
#
# 注意：本脚本（和它的 .sh）**永远 exit 0**（读数 ≠ 门禁），除了用法错。
#   把摘要贴到 CI 的 `$GITHUB_STEP_SUMMARY` 时用 `-Out $env:GITHUB_STEP_SUMMARY`。

[CmdletBinding()]
param(
    [string]$Repo,
    [string]$ApiBase,
    [string]$Out
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
    Write-Host "bash not found: this reading has exactly one implementation (scripts/write-readings-summary.sh)."
    Write-Host "Install Git for Windows (it ships bash) or run the .sh yourself. Refusing to"
    Write-Host "pretend a summary was written."
    exit 2
}

$sh = Join-Path $PSScriptRoot "write-readings-summary.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the reading has no implementation to delegate to."
    exit 2
}

$shArgs = @()
if ($Repo) { $shArgs += @('--repo', $Repo) }
if ($ApiBase) { $shArgs += @('--api-base', $ApiBase) }
if ($Out) {
    # 相对路径要按 PowerShell 的 CWD 解析：bash 的 CWD 与它不一致时会写到别处。
    $dir = Split-Path -Parent $Out
    if ($dir -and (Test-Path -LiteralPath $dir)) {
        $leaf = Split-Path -Leaf $Out
        $Out = (Join-Path (Resolve-Path -LiteralPath $dir).Path $leaf)
    }
    $shArgs += @('--out', $Out)
}

& $bash $sh @shArgs
exit $LASTEXITCODE
