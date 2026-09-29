# scripts/check-docs-links.ps1 —— docs 链接检查的 PowerShell 版本。
#
# 在 Windows runner 上由 CI 调用（若 GitHub 启用了 pwsh 默认 shell）。
# 行为与 check-docs-links.sh 保持一致；二者规则须同步演进。

[CmdletBinding()]
param(
    [string]$Root = "docs"
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path $Root)) {
    Write-Host "no $Root directory; skipping"
    exit 0
}

$files = Get-ChildItem -Path $Root -Recurse -Filter *.md -File -ErrorAction SilentlyContinue
$bad = 0

foreach ($f in $files) {
    $content = Get-Content -LiteralPath $f.FullName -Raw -ErrorAction SilentlyContinue
    if (-not $content) { continue }
    $matches = [regex]::Matches($content, '\[[^\]]+\]\(([^)]+)\)')
    foreach ($m in $matches) {
        $target = $m.Groups[1].Value
        # skip empty / pure anchors / absolute URLs
        if ($target -match '^(#|https?://|mailto:|tel:|file:)') { continue }
        # strip fragment
        if ($target.Contains('#')) { $target = $target.Substring(0, $target.IndexOf('#')) }
        if ($target -notmatch '\.(md|txt)$') { continue }
        $base = Split-Path $f.FullName -Parent
        $resolved = Join-Path $base $target
        if (-not (Test-Path -LiteralPath $resolved)) {
            Write-Host "BROKEN LINK in $($f.FullName): $($m.Value)"
            $bad++
        }
    }
}

if ($bad -gt 0) {
    Write-Host "found $bad broken links"
    exit 1
}
Write-Host "all docs links OK"