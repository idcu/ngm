#!/usr/bin/env bash
# release-dryrun.sh —— 在**本机当前平台**跑一遍发布流水线的可验证部分。
#
# 真验四件事（任一失败即退出非 0）：
#   1. 交叉编译置换为当前平台的真实构建；
#   2. ldflags 注入的版本能在产物里读出来（`--version` 第二列）；
#   3. SHA256SUMS 能被 `sha256sum -c` 验证；
#   4. 版本化别名与既有名字是**同一份字节**（cmp）。
#
# 它不下载、不上传，也不碰任何凭据。上传动作只有 release.yml 在 tag 上做。
#
# 用法：
#   scripts/release-dryrun.sh                       # 版本取 .ngm-version，目标取 go env
#   scripts/release-dryrun.sh v0.101.0              # 指定版本（带不带 v 都行）
#   TARGETS="linux/amd64" scripts/release-dryrun.sh
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

tag="${1:-}"
if [ -n "$tag" ]; then
  version="${tag#v}"
elif [ -f .ngm-version ]; then
  version="$(tr -d '\r\n' < .ngm-version)"
else
  # ngm 主仓没有 .ngm-version（那是消费方的固定文件）；dry-run 默认对准
  # 准备回填的开发版本。
  version="0.102.0-dev.1"
fi
target="${TARGETS:-$(go env GOOS)/$(go env GOARCH)}"
goos="${target%/*}"
goarch="${target#*/}"
built="${BUILT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

echo "release dry-run: version=${version} target=${target} built=${built}"
VERSION="$version" TARGETS="$target" BUILT="$built" OUT=dist \
  bash scripts/build-release.sh

legacy="dist/ngm-${goos}-${goarch}"
versioned="dist/ngm-${version}-${goos}-${goarch}"
if [ "$goos" = "windows" ]; then
  legacy="${legacy}.exe"
  versioned="${versioned}.exe"
fi

[ -f "$legacy" ] || { echo "ERROR: missing $legacy" >&2; exit 1; }
[ -f "$versioned" ] || { echo "ERROR: missing $versioned" >&2; exit 1; }
cmp -s "$legacy" "$versioned" || { echo "ERROR: $legacy and $versioned are not byte-identical" >&2; exit 1; }

(
  cd dist
  sha256sum -c SHA256SUMS
)

got="$("$legacy" --version)"
echo "version: $got"
case "$got" in
  "ngm ${version} (git:"*) ;;
  *) echo "ERROR: --version does not carry the injected version ${version}: $got" >&2; exit 1 ;;
esac

echo "OK: built, checksummed and version-verified (${target})"
