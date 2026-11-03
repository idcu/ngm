#!/usr/bin/env bash
# 构建发布产物：交叉编译六个目标 + SHA256SUMS 校验文件。
#
# 产物命名（guides/installation.md 的既有约定，v0.102 起加版本化别名）：
#   ngm-<os>-<arch>[.exe]                 既有名字，install 指南与旧脚本按它取
#   ngm-<version>-<os>-<arch>[.exe]       版本化别名，按 .ngm-version 直接拼地址
#
# 两种名字**来自同一份构建**（先构建一次，再复制别名），不是两套流水线。
# <os>/<arch> 取 Go 的 GOOS/GOARCH。刻意不用 `uname`：它给的是 x86_64 / aarch64，
# 与 Go 的 amd64 / arm64 不一致，拼出的下载地址会 404。
#
# 用法：
#   VERSION=0.1.0 ./scripts/build-release.sh
#   TARGETS="linux/amd64" VERSION=0.102.0-dev.1 ./scripts/build-release.sh
#
# 可覆盖的环境变量：VERSION / COMMIT / BUILT / OUT / TARGETS / VERSIONED_NAMES。
# BUILT 可由发布流水线固定为 tag 的提交时间，使同一 tag 的重建字节一致。
set -euo pipefail

VERSION="${VERSION:-0.1.0}"
# 仓库尚无提交时 `git rev-parse` 会失败——那是合法输入，退回 "unknown" 即可
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
if [ -z "$COMMIT" ]; then COMMIT="unknown"; fi
BUILT="${BUILT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
OUT="${OUT:-dist}"
VERSIONED_NAMES="${VERSIONED_NAMES:-1}"

# 目标矩阵在 scripts/release-matrix.txt（**唯一声明**）：构建脚本与漂移守卫
# 都读它，避免"构建产出了 A、检查却按 B 判定"这类漂移。
# TARGETS 是本地 dry-run / CI 裁剪用的：只影响构建集合，不改变命名与校验格式。
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MATRIX="$ROOT/scripts/release-matrix.txt"
if [ -n "${TARGETS:-}" ]; then
  read -r -a TARGET_LIST <<< "$TARGETS"
elif [ -f "$MATRIX" ]; then
  TARGET_LIST=()
  while IFS= read -r target; do
    target="${target%$'\r'}"
    case "$target" in ''|\#*) continue ;; esac
    TARGET_LIST+=("$target")
  done < "$MATRIX"
else
  echo "error: missing $MATRIX (the release target declaration)" >&2
  exit 1
fi
[ "${#TARGET_LIST[@]}" -gt 0 ] || { echo "error: empty target matrix" >&2; exit 1; }

# -trimpath：去掉构建机器上的绝对路径。
# -buildvcs=false：不让 VCS 状态进入 buildinfo，排除"同一 tag 在不同 checkout 下
#   多出一个 dirty 标记"这类差异。
# 刻意不 strip（-s -w）：CLI 出问题时 panic 栈里有符号更有用。
LDFLAGS="-X github.com/idcu/ngm/internal/version.Version=${VERSION}"
LDFLAGS+=" -X github.com/idcu/ngm/internal/version.GitCommit=${COMMIT}"
LDFLAGS+=" -X github.com/idcu/ngm/internal/version.BuildTime=${BUILT}"

rm -rf "$OUT"
mkdir -p "$OUT"

for target in "${TARGET_LIST[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  legacy="ngm-${os}-${arch}"
  if [ "$os" = "windows" ]; then
    legacy="${legacy}.exe"
  fi
  echo "building ${legacy}"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath -buildvcs=false -ldflags "$LDFLAGS" -o "${OUT}/${legacy}" ./cmd/ngm
  if [ "$VERSIONED_NAMES" != "0" ]; then
    versioned="ngm-${VERSION}-${os}-${arch}"
    if [ "$os" = "windows" ]; then
      versioned="${versioned}.exe"
    fi
    cp "${OUT}/${legacy}" "${OUT}/${versioned}"
  fi
done

# SHA256SUMS：每行 `<hex>  <文件名>`（两空格），可直接被 `sha256sum -c` 消费。
# macOS 没有 sha256sum，退到 shasum。
cd "$OUT"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum ngm-* > SHA256SUMS
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 ngm-* > SHA256SUMS
else
  echo "error: neither sha256sum nor shasum is available" >&2
  exit 1
fi

echo "--- ${OUT} ---"
ls -l
echo "wrote ${OUT}/SHA256SUMS"
