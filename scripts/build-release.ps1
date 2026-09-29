# 构建 v0.1 发布产物（Windows 本地用）。逻辑与 scripts/build-release.sh 对齐：
# 同样的六个目标、同样的命名、同样的 SHA256SUMS 格式。
#
# 用法：
#   pwsh -File scripts/build-release.ps1 -Version 0.1.0
param(
    [string]$Version = "0.1.0",
    [string]$Commit = "",
    [string]$Built = "",
    [string]$Out = "dist"
)

$ErrorActionPreference = "Stop"

if (-not $Commit) {
    $Commit = "unknown"
    # Windows PowerShell 5.1 的陷阱：原生命令往 stderr 写东西时，在
    # ErrorActionPreference=Stop 下会变成**终止错误**（"Needed a single revision"
    # 就会直接中断脚本）。而"仓库还没有提交"是合法输入，因此这里临时降级。
    $previous = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $rev = git rev-parse --short HEAD 2>$null
    if ($LASTEXITCODE -eq 0 -and $rev) { $Commit = "$rev".Trim() }
    $ErrorActionPreference = $previous
    if (-not $Commit) { $Commit = "unknown" }
}
if (-not $Built) {
    $Built = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
}

$ldflags = "-X github.com/idcu/ngm/internal/version.Version=$Version" +
           " -X github.com/idcu/ngm/internal/version.GitCommit=$Commit" +
           " -X github.com/idcu/ngm/internal/version.BuildTime=$Built"

# 六个目标，与 installation.md 的命名表一一对应
$targets = @(
    "linux/amd64", "linux/arm64",
    "darwin/amd64", "darwin/arm64",
    "windows/amd64", "windows/arm64"
)

if (Test-Path $Out) { Remove-Item -Recurse -Force $Out }
New-Item -ItemType Directory -Force -Path $Out | Out-Null

foreach ($target in $targets) {
    $parts = $target.Split("/")
    $goos = $parts[0]
    $goarch = $parts[1]
    $name = "ngm-$goos-$goarch"
    if ($goos -eq "windows") { $name += ".exe" }

    Write-Host "building $name"
    $env:GOOS = $goos
    $env:GOARCH = $goarch
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags $ldflags -o (Join-Path $Out $name) ./cmd/ngm
    if ($LASTEXITCODE -ne 0) { throw "go build failed for $name" }
}
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue

# SHA256SUMS：每行 `<hex>  <文件名>`（两空格），与 sha256sum 的输出格式一致
$lines = Get-ChildItem $Out -File |
    Where-Object { $_.Name -like "ngm-*" } |
    Sort-Object Name |
    ForEach-Object {
        $hash = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower()
        "$hash  $($_.Name)"
    }
Set-Content -Path (Join-Path $Out "SHA256SUMS") -Value $lines -Encoding ascii

Write-Host "--- $Out ---"
Get-ChildItem $Out | Select-Object Name, Length
Write-Host "wrote $Out/SHA256SUMS"
