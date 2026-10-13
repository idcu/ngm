# scripts/check-library-standard.ps1 —— JS Git 库标准自检的 Windows 入口。
#
# **唯一实现是 scripts/check-library-standard.sh**；本文件只是转调它
# （与 check-docs-links.ps1 同一条纪律：要检查的规则只允许有一种写法——
# 两份实现会漂移，而漂移的代价是"你以为检查过了"）。
#
# 用法：powershell -NoProfile -File scripts/check-library-standard.ps1 -Dir C:\src\my-lib [-Strict] [-Json]
#   需要 bash（Git for Windows 自带）+ node。找不到 bash 时**明确失败**（exit 2）——
#   没有"降级成不做事"这种路径。

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$Dir,
    [switch]$Strict,
    [switch]$Json
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
    Write-Host "bash not found: this check has exactly one implementation (scripts/check-library-standard.sh)."
    Write-Host "Install Git for Windows (it ships bash) or run the .sh yourself. Refusing to"
    Write-Host "report success without running the real check."
    exit 2
}

$sh = Join-Path $PSScriptRoot "check-library-standard.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the check has no implementation to delegate to."
    exit 2
}

if (-not (Test-Path -LiteralPath $Dir)) {
    Write-Host "no $Dir to check (nothing was checked)"
    exit 2
}
# 传绝对路径（正斜杠）：bash 的 CWD 与 PowerShell 的不一致时相对路径会错位。
$abs = ((Resolve-Path -LiteralPath $Dir).Path -replace '\\', '/')

$args = @($abs)
if ($Strict) { $args += '--strict' }
if ($Json) { $args += '--json' }

& $bash $sh @args
exit $LASTEXITCODE
