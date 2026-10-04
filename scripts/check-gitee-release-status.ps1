# scripts/check-gitee-release-status.ps1 —— Gitee 发布读数的 Windows 入口。
#
# **唯一实现是 scripts/check-gitee-release-status.sh**；本文件只是它的 Windows 入口（转调 bash）。
#
# 为什么不让 .ps1 自带一份实现（本文件的第一版就是那样，v0.14 改掉）：
# 两份实现会漂移，而漂移的代价是**你以为检查过了**。这不是假想——
# check-docs-links 就为同一个理由改成了转调，并留下了事故记录
# （.ps1 按 GBK 解码吞掉 ASCII 标点 → 本地一直打印 OK，CI 连红四次）。
# 本项目对这件事的结论只有一句：**要检查的规则只允许有一种写法。**
#
# 转调的另一个好处是平台无关：读数现在在 Windows（Git for Windows 自带 bash）、
# macOS、Linux、CI 上是同一段代码——而 Gitee 的发布状态正是"谁在哪台机器上都该看到同一个答案"。
#
# 用法：powershell -NoProfile -File scripts/check-gitee-release-status.ps1 [-Tags v0.12.0,v0.13.0]
#   需要 bash（Git for Windows 自带）。找不到 bash 时**明确失败（exit 2）**——
#   没有"降级成不做事"这种路径：那正是上面那次事故的形状。
#
# 判据、退出码（0 全齐 / 1 有缺 / 3 查不动且**不报成功**）与"为什么 Gitee 会自动多两个源码包"
# 都写在 .sh 的头部——那里是唯一实现，因此也是唯一的说明处。

[CmdletBinding()]
param(
    [string[]]$Tags = @()
)

$ErrorActionPreference = 'Stop'

function Find-Bash {
    # **先找 Git for Windows 的 bash，再退到 PATH 上的 bash**：装了 WSL 的机器上，
    # PATH 里会有 `C:\Windows\System32\bash.exe`（WSL 的入口），而它与 Windows 路径
    # 不互通——那样 .sh 会走到自己的"取不到 tag"分支。宁可明确失败，也不拿一个
    # 连不上 Windows 路径的解释器去跑。
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
    Write-Host "bash not found: this reading has exactly one implementation (scripts/check-gitee-release-status.sh)."
    Write-Host "Install Git for Windows (it ships bash) or run the .sh yourself. Refusing to"
    Write-Host "report success without running the real check."
    exit 2
}

$sh = Join-Path $PSScriptRoot "check-gitee-release-status.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the reading has no implementation to delegate to."
    exit 2
}

# `powershell -File script.ps1 -Tags a,b` 会把整个 `a,b` 作为**一个字符串**传进来
# （-File 不解析 PowerShell 的数组字面量）。第一版把这个坑踩在了自己身上：
# `a,b` 会被当成一个名叫 `a,b` 的 tag，于是报出一个**看起来合理、实际错误**的读数。
# 这里先按逗号拆开，再原样交给 .sh。
$flat = @()
foreach ($t in $Tags) {
    foreach ($part in ($t -split ',')) {
        $trimmed = $part.Trim()
        if ($trimmed -ne '') { $flat += $trimmed }
    }
}

& $bash $sh @flat
exit $LASTEXITCODE
