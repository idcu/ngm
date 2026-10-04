<#
.SYNOPSIS
Gitee 侧的发布读数：每个 tag 上到底有没有发行版、我们自己传的 7 个附件齐不齐。

.DESCRIPTION
为什么需要它：v0.12 复核抓到一个空白——`scripts/check-release-status.sh --published` 只问 GitHub。
Gitee 侧"有没有这个发行版"此前只能靠人去点，于是状态页长出了一条错的
"`v0.1.0` ~ `v0.4.0` 两个源都可取到"（Gitee 上根本没有 `v0.1.0` 的发行版；2026-10-04 正是用
本脚本用的那个端点证伪的）。**一条读数胜过一句"已发布"。**

它**只读公开 API、不需要 token**，因此随时可跑；而"补发附件"仍然需要 `GITEE_TOKEN`
（那是 `upload-gitee-assets.ps1` 的事）。

判据与 GitHub 侧有一处不同，必须说清楚：Gitee 会给每个 release **自动附两个源码包**
（`<tag>.zip` / `<tag>.tar.gz`），所以 API 里看到的是 9 个 asset，而我们自己传的是 7 个。
本脚本按**我们自己传的那 7 个**判定，并把源码包单独报出来——否则"9 个"会让人以为多了两个。

为什么只有 .ps1（没有 .sh 孪生）：Gitee 侧的人工工序历来是 .ps1（理由同
`upload-gitee-assets.ps1`），而 CI 跑在 ubuntu 上、没有 PowerShell——所以这是一条**本地读数**。
两份实现只会带来"改了一边忘了另一边"的漂移，本项目已经吃过这类亏。

用法：

    powershell -File scripts/check-gitee-release-status.ps1                  # 本仓库全部 v* tag
    powershell -File scripts/check-gitee-release-status.ps1 -Tags v0.12.0    # 只看指定的几个
    powershell -File scripts/check-gitee-release-status.ps1 -TimeoutSec 5    # 网络抖动时缩短等待

退出码：0 = 全部就位；1 = 有缺（列出缺什么、以及补它的那条命令）；
3 = **查不动**（网络 / 限额）——查不动**不报成功**，与 GitHub 侧同一条纪律。
#>
[CmdletBinding()]
param(
    [string]$GiteeOwner = 'idcu',
    [string]$GiteeRepo = 'ngm',
    [string[]]$Tags = @(),
    [int]$TimeoutSec = 20,
    # API 基址可注入：一是能实测"查不动"那条分支（指向一个死地址，必须 exit 3 而不是报成功），
    # 二是 Gitee 企业版有别的基址。默认是公开的 gitee.com。
    [string]$ApiBase = 'https://gitee.com/api/v5'
)

$ErrorActionPreference = 'Stop'

# 我们自己传的 7 个附件：六个平台二进制 + SHA256SUMS。
$ExpectedAssets = @(
    'ngm-darwin-amd64', 'ngm-darwin-arm64',
    'ngm-linux-amd64', 'ngm-linux-arm64',
    'ngm-windows-amd64.exe', 'ngm-windows-arm64.exe',
    'SHA256SUMS'
)

function Note([string]$Msg) { Write-Host "   $Msg" -ForegroundColor DarkGray }
function Good([string]$Msg) { Write-Host "   $Msg" -ForegroundColor Green }
function Bad([string]$Msg) { Write-Host "   $Msg" -ForegroundColor Yellow }

# `powershell -File script.ps1 -Tags a,b` 会把整个 `a,b` 作为**一个字符串**传进来
# （-File 不解析 PowerShell 的数组字面量），因此在这里按逗号拆开。
#
# 不拆的后果实测过：`-Tags v0.2.0,v0.5.0` 会被当成一个名叫 `v0.2.0,v0.5.0` 的 tag，
# 于是脚本报"没有发行版"——一个**看起来合理、实际错误**的读数。
if ($Tags.Count -gt 0) {
    $Tags = @($Tags | ForEach-Object { $_ -split ',' } | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne '' })
}

# tag 清单默认取本仓库的 v* tag：git 是事实源，新版本自动进来（不必维护第二份列表）。
if ($Tags.Count -eq 0) {
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
        Write-Host 'ERROR: 取 tag 清单需要 git（也可以用 -Tags 显式指定）。' -ForegroundColor Red
        exit 3
    }
    $Tags = @(& git tag --list 'v*' --sort=version:refname)
    if ($LASTEXITCODE -ne 0 -or $Tags.Count -eq 0) {
        Write-Host 'ERROR: 从 git 取不到 v* tag，无法判定；不做"看起来像成功"的空跑。' -ForegroundColor Red
        exit 3
    }
}

# 只读公开 API。三种形态都要接住：
#   - 有 release：对象，且带 id
#   - 没有 release：Gitee 返回 JSON `null`（HTTP 200），而 PowerShell 5.1 会把它变成**字符串** "null"
#   - 连不上 / 被限额：抛错
# 前两种的判据与 upload-gitee-assets.ps1 的 Get-GiteeRelease 一致——那个坑已经踩过一次。
function Read-GiteeRelease([string]$Tag) {
    $uri = "$ApiBase/repos/$GiteeOwner/$GiteeRepo/releases/tags/$Tag"
    try {
        $r = Invoke-RestMethod -Method Get -Uri $uri -TimeoutSec $TimeoutSec `
            -Headers @{ 'User-Agent' = 'ngm-release-read' }
    } catch {
        return @{ state = 'unreachable'; detail = $_.Exception.Message }
    }
    if ($null -eq $r -or $r -is [string]) { return @{ state = 'missing' } }
    if (-not $r.id) { return @{ state = 'missing' } }
    return @{ state = 'present'; release = $r }
}

Write-Host ''
Write-Host "Gitee 发布读数 — $GiteeOwner/$GiteeRepo（$($Tags.Count) 个 tag，只读公开 API）" -ForegroundColor Cyan
Write-Host ''

$missing = @()
$unchecked = @()
$ok = 0

foreach ($tag in $Tags) {
    $res = Read-GiteeRelease $tag

    if ($res.state -eq 'unreachable') {
        $unchecked += $tag
        Write-Host ("{0,-9} {1}" -f $tag, 'NOT CHECKED') -ForegroundColor Yellow
        Note "原因：$($res.detail)"
        continue
    }
    if ($res.state -eq 'missing') {
        $missing += @{ tag = $tag; reason = 'no release'; names = @() }
        Write-Host ("{0,-9} {1}" -f $tag, 'MISSING (没有发行版)') -ForegroundColor Yellow
        Note "补它：powershell -File scripts/upload-gitee-assets.ps1 -Tag $tag"
        continue
    }

    $rel = $res.release
    $assets = @($rel.assets)
    $names = @($assets | ForEach-Object { $_.name })

    # 源码包是 Gitee 自动附的，不算我们传的：照名字排除，而不是按数量相减。
    $autoSources = @("$tag.zip", "$tag.tar.gz")
    $ours = @($names | Where-Object { $autoSources -notcontains $_ })
    $absent = @($ExpectedAssets | Where-Object { $ours -notcontains $_ })

    if ($absent.Count -eq 0) {
        $ok++
        Write-Host ("{0,-9} {1}" -f $tag, "OK (7 个附件 + $($autoSources.Count) 个源码包)") -ForegroundColor Green
    } else {
        $missing += @{ tag = $tag; reason = 'incomplete'; names = $absent }
        Write-Host ("{0,-9} {1}" -f $tag, "INCOMPLETE (缺 $($absent.Count) 个)") -ForegroundColor Yellow
        Note ('缺：' + ($absent -join ', '))
        Note "补它：powershell -File scripts/upload-gitee-assets.ps1 -Tag $tag"
    }
}

Write-Host ''
if ($unchecked.Count -gt 0) {
    Write-Host "查不动 $($unchecked.Count) 个：$($unchecked -join ', ')" -ForegroundColor Yellow
    Write-Host '这不是成功：网络或限额让读数缺失，稍后重跑即可。' -ForegroundColor Yellow
    exit 3
}
if ($missing.Count -gt 0) {
    Write-Host "有缺：$($missing.Count) 个（就位 $ok 个）" -ForegroundColor Yellow
    exit 1
}
Good "全部就位：$ok 个 tag 的 7 个附件都在 Gitee 上。"
exit 0
