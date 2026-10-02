# scripts/check-docs-links.ps1 —— docs 链接检查的 Windows 入口。
#
# **唯一实现是 scripts/check-docs-links.sh**；本文件只是它的 Windows 入口（转调 bash）。
#
# 为什么不各写一份规则（本文件曾经就是这么做的，2026-10-02 改掉）：
# 两份实现会漂移，而漂移的代价是**你以为检查过了**。实测事故：
# .ps1 用 `Get-Content -Raw`（PowerShell 5.1 默认按系统 ANSI 解码，本机是 GBK）读 UTF-8 文件，
# 而 GBK 是**双字节**编码——一个错位的引导字节会把紧跟在后的 ASCII `)` 一起吞掉。
# 于是 `[ADR-018](./adr/adr-018-store-reclaim.md)` 这样的链接里，结尾的 `)` 消失，
# 正则 `\(([^)]+)\)` 便跨行吞掉整段文本、**那条链接在它眼里凭空消失**：
# CI（跑 .sh）因此连红四次，而本地（跑 .ps1）一直打印"all docs links OK"。
# 教训与本项目其它几处一致：**要检查的规则只允许有一种写法**；
# 两个检查器结论不一致时，你并不知道哪一个在说谎。
#
# 用法：powershell -NoProfile -File scripts/check-docs-links.ps1 [docs_root]
#   需要 bash（Git for Windows 自带）。找不到 bash 时**明确失败**（exit 2）——
#   没有"降级成不做事"这种路径：那正是上面那次事故的形状。
#
# 注意：本检查只看"目标文件是否存在"，**不校验锚点**（`#xxx` 部分被剥掉）。
# 中文标题的 GitHub 锚点算法不适合在这里复刻——复刻一个近似的实现，
# 只会再制造一对会互相矛盾的检查器。

[CmdletBinding()]
param(
    # 默认查**根 README + docs/**：README 是最多人读的一份文档，
    # 而它此前不在任何检查范围内（v0.10 B 组补上）。
    [string[]]$Root = @("docs", "README.md")
)

$ErrorActionPreference = 'Stop'

function Find-Bash {
    # **先找 Git for Windows 的 bash，再退到 PATH 上的 bash**：装了 WSL 的机器上，
    # PATH 里会有 `C:\Windows\System32\bash.exe`（WSL 的入口），而它与 Windows 路径
    # 不互通——那样 .sh 会走到自己的"no docs directory; skipping"分支并 exit 0，
    # 于是这条检查会**静默地什么都没查**。这正是本文件尽力避免的那种失败。
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
    Write-Host "bash not found: this check has exactly one implementation (scripts/check-docs-links.sh)."
    Write-Host "Install Git for Windows (it ships bash) or run the .sh yourself. Refusing to"
    Write-Host "report success without running the real check."
    exit 2
}

$sh = Join-Path $PSScriptRoot "check-docs-links.sh"
if (-not (Test-Path -LiteralPath $sh)) {
    Write-Host "missing $sh — the check has no implementation to delegate to."
    exit 2
}

# 根不存在时**自己**报错，而不是把它交给 .sh：.sh 在**无参数**时对缺失的默认根是
# "skipping"+exit 0（那是给 CI 用的，CI 里 docs/ 一定在）。这里逐个点名，缺一个就报错。
$abs = @()
foreach ($r in $Root) {
    if (-not (Test-Path -LiteralPath $r)) {
        Write-Host "no $r to scan (nothing was checked for it)"
        exit 2
    }
    # 传绝对路径（正斜杠，Git Bash 与 WSL 都能认）；相对路径在 bash 的 CWD 与 PS 不一致时会错位。
    $abs += ((Resolve-Path -LiteralPath $r).Path -replace '\\', '/')
}

& $bash $sh @abs
exit $LASTEXITCODE
