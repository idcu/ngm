# scripts/release-notes.ps1 —— release-notes.sh 的 Windows 入口。
#
# 唯一实现是 scripts/release-notes.sh（CHANGELOG 的取段规则只有一种写法）；
# 本文件只转发参数并把退出码原样传回，避免"Windows 上取不到发布说明"。
#
# 用法：powershell -NoProfile -File scripts/release-notes.ps1 <TAG> [-Out FILE]
#   退出码：0 = 找到且非空；3 = 没有这个版本的段落。

[CmdletBinding()]
param(
    [Parameter(Position = 0, Mandatory = $true)]
    [string]$Tag,
    [string]$Out
)

$ErrorActionPreference = 'Stop'

function Find-Bash {
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
    Write-Host "bash not found: release notes have exactly one implementation (scripts/release-notes.sh)."
    Write-Host "Install Git for Windows (it ships bash). Refusing to print an empty note as success."
    exit 2
}

$sh = Join-Path $PSScriptRoot "release-notes.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — release notes have no implementation to delegate to."
    exit 2
}

$shArgs = @($Tag)
if ($Out) { $shArgs += @('--out', $Out) }

& $bash $sh @shArgs
exit $LASTEXITCODE
