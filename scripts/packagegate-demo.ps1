# scripts/packagegate-demo.ps1 —— PackageGate 对照 demo 的 Windows 入口。
#
# **唯一实现是 scripts/packagegate-demo.sh**；本文件只是它的 Windows 入口（转调 bash）。
#
# 与 check-docs-links / check-gitee-release-status 同一形状：demo 的判据（断言什么、
# 怎么断言）只允许有一种写法——两份实现会漂移，而这里的漂移意味着
# "你以为看过了对比，其实看的是另一份脚本"。
#
# 用法：powershell -NoProfile -File scripts/packagegate-demo.ps1 [--with-npm]
#   需要 bash（Git for Windows 自带）。找不到 bash 时**明确失败（exit 2）**。

[CmdletBinding()]
param(
    [switch]$WithNpm
)

$ErrorActionPreference = 'Stop'

function Find-Bash {
    # 先找 Git for Windows 的 bash，再退到 PATH：装了 WSL 的机器上 PATH 里的
    # bash.exe 是 WSL 入口，与 Windows 路径不互通——宁可明确失败，
    # 也不拿一个跑不了这台机器上 git 的解释器去跑 demo。
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
    Write-Host "bash not found: this demo has exactly one implementation (scripts/packagegate-demo.sh)."
    Write-Host "Install Git for Windows (it ships bash) or run the .sh yourself."
    exit 2
}

$sh = Join-Path $PSScriptRoot "packagegate-demo.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the demo has no implementation to delegate to."
    exit 2
}

$argsList = @()
if ($WithNpm) { $argsList += '--with-npm' }

& $bash $sh @argsList
exit $LASTEXITCODE
