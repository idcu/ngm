<#
.SYNOPSIS
把某个 tag 的发行附件从 GitHub 镜像搬到 Gitee（v0.6 D 组）。

.DESCRIPTION
主仓在 Gitee，而**投递**只能由 GitHub Actions 完成（Gitee 侧没有可用的 API 凭据），
于是每次发布都留下同一道手工工序：把 7 个附件搬过去。v0.2 ~ v0.4 补发时，GitHub 侧齐了、
Gitee 侧 21 个附件仍缺——而安装指南把 Gitee 列为**国内推荐源**，那三版走推荐路径会 404。

为什么**从 GitHub 取字节**、而不是本地重新构建：**两个源必须是同一份字节**。
本地重编会得到另一份产物（工具链、时间、路径都可能不同），那样"两个源一致"
就成了一句没法核对的话。`SHA256SUMS` 是同一个文件，它同时是两边的校验清单——
本脚本按它逐个核对，并在上传后再从 Gitee 下载一遍比对。

用法：

    $env:GITEE_TOKEN = '<Gitee 私人令牌（需要 releases 权限）>'
    powershell -File scripts/upload-gitee-assets.ps1 -Tag v0.4.0            # 上传 + 逐个校验
    powershell -File scripts/upload-gitee-assets.ps1 -Tag v0.4.0 -DryRun    # 只列计划（不下载、不上传）
    powershell -File scripts/upload-gitee-assets.ps1 -Tag v0.4.0 -Force     # 覆盖已存在的附件

GitHub 连不上时（这个脚本的用途本身就意味着你多半在 GitHub 不好连的地方）可以给一个下载代理：

    powershell -File scripts/upload-gitee-assets.ps1 -Tag v0.2.0 -GitHubProxy https://ghproxy.net

只在 **Windows PowerShell 5.1** 上验证过（编写环境没有 `pwsh`）；它不用 7.x 专有语法，
若你装了 `pwsh` 也能跑。

**幂等**：同名附件已存在且 sha256 一致时跳过（重跑安全）；不一致时报错并告诉你怎么处置，
绝不静默覆盖。

为什么**只有 .ps1**（没有 .sh 孪生）：这是**本地人工**工序（Gitee 附件只能人工触发），
不是 CI 步骤；两份实现只会带来"改了一边忘了另一边"的漂移——本项目已经吃过这类亏。

退出码：0 = 附件全部就位且 sha256 一致；1 = 任何一步没做成（**绝不静默跳过**）。

.NOTES
本脚本的上传路径**未经真实令牌验证过**（编写环境没有 Gitee 凭据）。它按 Gitee API v5
的 `attach_files` 形态写，并且每一步都判定结果、不猜成败；第一次真跑时请先 `-DryRun`
看一眼清单，再正式跑。
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Tag,
    [string]$GiteeOwner = 'idcu',
    [string]$GiteeRepo = 'ngm',
    [string]$GitHubOwner = 'idcu',
    [string]$GitHubRepo = 'ngm',
    # 建 release 时用哪个分支作为 target_commitish。
    #
    # **必须是分支名，不是 tag 名**——第一版写成 `$Tag`，Gitee 会返回一个不带 `id`
    # 的响应（而不是抛错），于是脚本的防御性检查把它拦下来了：
    # 宁可停下说"响应形态与预期不同"，也不拿一个来路不明的 id 去传文件。
    [string]$TargetBranch = 'main',
    [string]$WorkDir = (Join-Path $env:TEMP "ngm-gitee-assets-$Tag"),
    # 可选的下载代理前缀（例如 `-GitHubProxy https://ghproxy.net`）。
    #
    # 为什么需要它：**这个脚本的用途本身就意味着"你在 GitHub 不好连的地方"**
    # （Gitee 是给国内用户的推荐源）。编写时就实地遇到 curl (28) 连不上 github.com，
    # 而"补 Gitee 附件"这件事不能因此卡住。
    #
    # 信任边界（重要）：走代理下载的**只有二进制**，校验始终是逐个比对 `SHA256SUMS`；
    # 而清单**优先直连 GitHub 获取**（下面的代码就是这么写的），只有直连失败时才会
    # 退到代理，并在那时明确警告——因为一旦清单也经第三方，校验就失去意义。
    [string]$GitHubProxy = '',
    [switch]$DryRun,
    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$ApiBase = 'https://gitee.com/api/v5'

function Fail([string]$Msg) { Write-Host "ERROR: $Msg" -ForegroundColor Red; exit 1 }
function Step([string]$Msg) { Write-Host ''; Write-Host "== $Msg" -ForegroundColor Cyan }
function Note([string]$Msg) { Write-Host "   $Msg" -ForegroundColor DarkGray }
function Good([string]$Msg) { Write-Host "   $Msg" -ForegroundColor Green }

# curl.exe 同时承担下载与 multipart 上传：Windows PowerShell 5.1 的 Invoke-WebRequest
# 依赖 IE 引擎，而 multipart 在 5.1 里没有 `-Form`。缺 curl 就明确失败，不做静默降级
# （"降级成不做事"正是本项目反复防的那类失败）。
$curlCmd = Get-Command curl.exe -ErrorAction SilentlyContinue
if (-not $curlCmd) {
    Fail 'PATH 上没有 curl.exe；本脚本用它下载与上传附件（Windows 10 1803+ 自带）。'
}
$curl = $curlCmd.Source

# Fetch 带重试：这个脚本要搬 ~90MB，而到 github.com 的连通性**本来就会抖**
# （编写时就实地遇到过 curl (28) 连接超时）。一次抖动不该让整轮白跑——
# 已下载的附件会被复用（见下面的 Test-Path），重跑的代价只是没完成的那几个。
function Fetch([string]$Url, [string]$Dest, [int]$Attempts = 4, [switch]$NoFail) {
    for ($i = 1; $i -le $Attempts; $i++) {
        if (Test-Path $Dest) { Remove-Item $Dest -Force }
        & $curl -L --fail --silent --show-error --connect-timeout 30 --retry 2 --retry-delay 3 -o $Dest $Url
        if ($LASTEXITCODE -eq 0 -and (Test-Path $Dest) -and (Get-Item $Dest).Length -gt 0) { return $true }
        if ($i -lt $Attempts) {
            Note "下载第 $i 次失败（$Url），$([int]($i * 3)) 秒后重试"
            Start-Sleep -Seconds ([int]($i * 3))
        }
    }
    # -NoFail 用于"我要自己决定失败了怎么办"的调用点（例如清单的直连/代理二选一）。
    if ($NoFail) { return $false }
    Fail "下载失败（试了 $Attempts 次）：$Url"
}

# ---------------- 1. GitHub 侧的附件清单（公开 API，不需要 token） ----------------
Step "GitHub 侧：$GitHubOwner/$GitHubRepo 的 $Tag"
try {
    $ghRel = Invoke-RestMethod -Uri "https://api.github.com/repos/$GitHubOwner/$GitHubRepo/releases/tags/$Tag" `
        -Headers @{ 'User-Agent' = 'ngm-gitee-uploader' }
} catch {
    Fail "取不到 GitHub 上 $Tag 的 release（先让 release 工作流跑完）：$($_.Exception.Message)"
}
$assets = @($ghRel.assets)
if ($assets.Count -ne 7) {
    Fail "GitHub 上 $Tag 有 $($assets.Count) 个附件，期望 7（6 个平台二进制 + SHA256SUMS）"
}
foreach ($a in $assets) { Note ('{0,-30} {1,12:N0} B' -f $a.name, $a.size) }

# ---------------- 2. 校验清单：SHA256SUMS ----------------
Step '校验清单（SHA256SUMS）'
New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
$sumsAsset = $assets | Where-Object { $_.name -eq 'SHA256SUMS' }
if (-not $sumsAsset) {
    Fail "GitHub 上 $Tag 没有 SHA256SUMS —— 它是两边的校验清单，缺了就无法核对。"
}
# GhUrl 把 GitHub 直链套上（可选的）代理前缀。空则原样返回。
function GhUrl([string]$Url) {
    if (-not $GitHubProxy) { return $Url }
    return $GitHubProxy.TrimEnd('/') + '/' + $Url
}

$sumsPath = Join-Path $WorkDir 'SHA256SUMS'
if (Test-Path $sumsPath) {
    # 复用上一轮取到的清单：发行附件是不可变的，重跑时不必再打扰 GitHub
    # （连通性抖动时，少一次外部依赖就少一次失败机会）。打印它自己的 sha256，
    # 让"用的是哪一份清单"可追溯。
    Note "复用本地清单：$sumsPath (sha256=$((Get-FileHash -Algorithm SHA256 -Path $sumsPath).Hash.Substring(0,16).ToLower()))"
} else {
    # 清单是**信任锚**，因此优先直连 GitHub；只有直连失败时才退到代理，并且明说。
    if (-not (Fetch $sumsAsset.browser_download_url $sumsPath -NoFail)) {
        if (-not $GitHubProxy) {
            Fail "直连取不到 SHA256SUMS，而本次没有给 -GitHubProxy。（清单是校验的依据，不能省。）"
        }
        Note '⚠️ 直连取清单失败，退到代理：**清单本身也经了第三方**，这次校验的意义因此打折，请人工核对结果。'
        Fetch (GhUrl $sumsAsset.browser_download_url) $sumsPath
    }
}

$expected = @{}
foreach ($line in (Get-Content -Path $sumsPath)) {
    $t = $line.Trim()
    if (-not $t) { continue }
    # 形态：`<64 位 hex><两个空格><文件名>`（sha256sum 的默认输出）
    if ($t -match '^([0-9a-fA-F]{64})\s+\*?(.+)$') {
        $expected[$Matches[2].Trim()] = $Matches[1].ToLower()
    } else {
        Fail "SHA256SUMS 里有一行认不出来：$t"
    }
}
if ($expected.Count -ne 6) {
    Fail "SHA256SUMS 有 $($expected.Count) 条，期望 6（6 个二进制；它自己不在其中）"
}

# 清单必须**恰好**覆盖 GitHub 上那 6 个附件：少一个说明清单过期，多一个说明清单对不上现实。
$wantNames = @($assets | Where-Object { $_.name -ne 'SHA256SUMS' } | ForEach-Object { $_.name } | Sort-Object)
$sumNames = @($expected.Keys | Sort-Object)
if (($wantNames -join ',') -ne ($sumNames -join ',')) {
    Fail ("SHA256SUMS 与 GitHub 上的附件对不上：`n" +
        "  清单: $($sumNames -join ', ')`n  附件: $($wantNames -join ', ')")
}
Good "6 条清单与 6 个附件一一对应 ✓"

# ---------------- 3. 没有 token 时到此为止（明确失败，不动远端） ----------------
$token = $env:GITEE_TOKEN
if (-not $DryRun -and -not $token) {
    Fail ('GITEE_TOKEN 未设置。上传需要 Gitee 私人令牌（releases 权限）：' +
        "`n       `$env:GITEE_TOKEN = '<token>'`n       （本次没有改动任何东西；也可以先加 -DryRun 看清单。）")
}

if ($DryRun) {
    Step 'DryRun：计划如下（未下载二进制、未上传）'
    foreach ($n in $wantNames) { Note ('{0,-30} sha256={1}' -f $n, $expected[$n]) }
    Note ('{0,-30} sha256={1}' -f 'SHA256SUMS', (Get-FileHash -Algorithm SHA256 -Path $sumsPath).Hash.ToLower())
    Note "共 7 个附件（6 个二进制 + SHA256SUMS）；目标：$ApiBase/repos/$GiteeOwner/$GiteeRepo/releases/tags/$Tag"
    Write-Host ''
    Good '清单自洽；设置 GITEE_TOKEN 后去掉 -DryRun 即可上传。'
    exit 0
}

# ---------------- 4. 目标 release：没有就建一个 ----------------
Step "Gitee 侧：$GiteeOwner/$GiteeRepo 的 $Tag"

# Get-GiteeRelease 把"查不到"统一成 `$null`。
#
# 为什么要单写一个函数：Gitee 对**不存在的 release** 返回 JSON `null`（HTTP 200），
# 而 **PowerShell 5.1 的 `Invoke-RestMethod` 把它变成字符串 `"null"`**——
# 既 truthy（于是 `-not $rel` 判不出来），又没有 `.id`。
# 第一版因此在这一步停下，报"响应里没有 id"，原始响应是一行带引号的 `"null"`。
# 这是又一个"跨工具边界上看起来一样、其实不一样"的例子（见复盘 §5.5）。
function Get-GiteeRelease([string]$Uri) {
    try { $r = Invoke-RestMethod -Method Get -Uri $Uri } catch { return $null }
    if ($null -eq $r -or $r -is [string]) { return $null }   # JSON null / 别的非对象响应
    if (-not $r.id) { return $null }
    return $r
}

$rel = Get-GiteeRelease "$ApiBase/repos/$GiteeOwner/$GiteeRepo/releases/tags/$Tag`?access_token=$token"
if (-not $rel) {
    Note 'Gitee 上没有该 tag 的 release，创建一个（内容与 tag 一致）'
    $payload = @{
        tag_name         = $Tag
        name             = $Tag
        target_commitish = $TargetBranch
        body             = "镜像自 GitHub 发行版 $Tag；附件按 SHA256SUMS 逐个校验。"
    } | ConvertTo-Json
    try {
        # body 必须按 **UTF-8 字节**发：PowerShell 5.1 对字符串 body 用 ISO-8859-1 编码，
        # 中文会被写成 `???`（实测：第一版创建的 release 说明就是这个样子）。
        $rel = Invoke-RestMethod -Method Post -Uri "$ApiBase/repos/$GiteeOwner/$GiteeRepo/releases?access_token=$token" `
            -ContentType 'application/json; charset=utf-8' `
            -Body ([Text.Encoding]::UTF8.GetBytes($payload))
    } catch {
        Fail "创建 release 失败：$($_.Exception.Message)"
    }
}
if (-not $rel.id) {
    # 把原始响应带出来：Gitee 在参数不对时**不抛错**，而是回一个没有 id 的 JSON。
    # 只写"形态与预期不同"会让人去猜，而这行响应本身通常就说明了原因。
    Fail ("Gitee 返回的 release 里没有 id，已停止以免误传。原始响应：`n      " +
        ($rel | ConvertTo-Json -Depth 3 -Compress))
}
Good "release id=$($rel.id)"

$existing = @{}
foreach ($a in @($rel.assets)) { if ($a.name) { $existing[$a.name] = $a } }

# ---------------- 5. 逐个附件：上传 + 双向校验 ----------------
#
# **7 个**都传：6 个二进制 + `SHA256SUMS`。
# 第一版只传了 6 个二进制，把清单当成了"本地校验用的工具"——但那与发布清单写的
# "7 个附件"不符，而且 Gitee 上的用户**没法自己核对**（清单不在那边）。
# 清单是"两个源一致"的凭据，它本身就应当在两个源上都能取到。
Step '逐个附件（7 个：6 个二进制 + SHA256SUMS）：取 GitHub 字节 → 核对 sha256 → 上传 → 从 Gitee 复核'
$done = 0
foreach ($asset in ($assets | Sort-Object name)) {
    $name = $asset.name
    $local = Join-Path $WorkDir $name

    if (-not (Test-Path $local)) { Fetch (GhUrl $asset.browser_download_url) $local }

    if ($name -eq 'SHA256SUMS') {
        # 它不在自己的清单里，核对对象就是"取到的字节"本身。
        $want = (Get-FileHash -Algorithm SHA256 -Path $local).Hash.ToLower()
    } else {
        $want = $expected[$name]
        $got = (Get-FileHash -Algorithm SHA256 -Path $local).Hash.ToLower()
        if ($got -ne $want) { Fail "$name 从 GitHub 取回后 sha256 不符（$got ≠ $want）—— 清单与字节不一致，先查这件事。" }
    }

    if ($existing.ContainsKey($name)) {
        if (-not $Force) {
            # 幂等：已存在就**核对**，不重复上传。
            $tmpCheck = Join-Path $WorkDir ("gitee-" + $name)
            if ($existing[$name].browser_download_url) {
                Fetch $existing[$name].browser_download_url $tmpCheck
                $there = (Get-FileHash -Algorithm SHA256 -Path $tmpCheck).Hash.ToLower()
                if ($there -eq $want) { Good "$name 已存在且 sha256 一致 ✓（跳过上传）"; $done++; continue }
                Fail "$name 在 Gitee 上已存在但 sha256 不同（$there ≠ $want）。确认要覆盖就加 -Force（脚本会先删后传），否则请人工核对。"
            }
            Note "$name 已存在（无法取回比对），用 -Force 可覆盖"
            continue
        }
        Note "$name 已存在，-Force：先删后传"
        try {
            Invoke-RestMethod -Method Delete -Uri "$ApiBase/repos/$GiteeOwner/$GiteeRepo/releases/$($rel.id)/attach_files/$($existing[$name].id)?access_token=$token" | Out-Null
        } catch {
            Fail "删除已存在的附件失败（id=$($existing[$name].id)）：$($_.Exception.Message)"
        }
    }

    $respFile = Join-Path $WorkDir ("upload-" + $name + ".json")
    # 路径转成正斜杠：curl（Windows 原生版）对 `-F file=@path` 里的反斜杠处理不如正斜杠稳。
    $localForCurl = $local -replace '\\', '/'
    $code = & $curl -sS -o $respFile -w '%{http_code}' -X POST `
        "$ApiBase/repos/$GiteeOwner/$GiteeRepo/releases/$($rel.id)/attach_files" `
        -F "access_token=$token" -F "file=@$localForCurl"
    if ($LASTEXITCODE -ne 0 -or $code -notmatch '^2') {
        $body = ''
        if (Test-Path $respFile) { $body = (Get-Content -Raw $respFile) }
        Fail "上传 $name 失败（HTTP $code）：$body"
    }

    # 从 Gitee 取回再比一次：这一条才是"两个源一致"的证据（不是"上传返回 200"）。
    $uploaded = Get-Content -Raw $respFile | ConvertFrom-Json
    $url = $uploaded.browser_download_url
    if (-not $url) { Fail "$name 上传后响应里没有 browser_download_url，无法复核（响应：$respFile）" }
    $back = Join-Path $WorkDir ("back-" + $name)
    Fetch $url $back
    $verified = (Get-FileHash -Algorithm SHA256 -Path $back).Hash.ToLower()
    if ($verified -ne $want) {
        Fail "$name 上传后从 Gitee 取回的字节与 GitHub 不一致（$verified ≠ $want）—— 这一版**没有**两个源一致。"
    }
    Good "$name 上传并从 Gitee 复核 sha256 一致 ✓"
    $done++
}

Write-Host ''
if ($done -eq 7) {
    Good "7/7 就位：6 个二进制的 sha256 均与 SHA256SUMS 一致，清单本身也已在 Gitee 上（$Tag）。"
    Note '把这次补传记进 docs/development/README.md 的发布清单（含日期与抽查结果）。'
    exit 0
}
Fail "$done/7 完成 —— 还有附件没有就位；重跑本脚本是安全的（已存在且一致的会跳过）。"
