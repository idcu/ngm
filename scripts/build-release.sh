#!/usr/bin/env bash
# 构建 v0.1 发布产物：交叉编译六个目标 + SHA256SUMS 校验文件。
#
# 产物命名（guides/installation.md）：ngm-<os>-<arch>[.exe]，<os>/<arch> 取
# Go 的 GOOS/GOARCH。刻意不用 `uname`：它给的是 x86_64 / aarch64，
# 与 Go 的 amd64 / arm64 不一致，拼出的下载地址会 404。
#
# 用法：
#   VERSION=0.1.0 ./scripts/build-release.sh
#
# 可覆盖的环境变量：VERSION / COMMIT / BUILT / OUT。
# 未提供 COMMIT / BUILT 时从 git 与当前时间推导，保证本地跑也能得到完整元信息。
set -euo pipefail

VERSION="${VERSION:-0.1.0}"
# 仓库尚无提交时 `git rev-parse` 会失败——那是合法输入，退回 "unknown" 即可
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
if [ -z "$COMMIT" ]; then COMMIT="unknown"; fi
BUILT="${BUILT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
OUT="${OUT:-dist}"

# 六个目标：三大平台 × 两种架构（与 installation.md 的命名表一一对应）
TARGETS=(
  linux/amd64 linux/arm64
  darwin/amd64 darwin/arm64
  windows/amd64 windows/arm64
)

# -trimpath：去掉构建机器上的绝对路径，使产物可复现（不含开发者的目录结构）。
# 刻意不 strip（-s -w）：CLI 出问题时 panic 栈里有符号更有用，几 MB 的体积换可诊断性值得。
LDFLAGS="-X github.com/idcu/ngm/internal/version.Version=${VERSION}"
LDFLAGS+=" -X github.com/idcu/ngm/internal/version.GitCommit=${COMMIT}"
LDFLAGS+=" -X github.com/idcu/ngm/internal/version.BuildTime=${BUILT}"

rm -rf "$OUT"
mkdir -p "$OUT"

for target in "${TARGETS[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  name="ngm-${os}-${arch}"
  if [ "$os" = "windows" ]; then
    name="${name}.exe"
  fi
  echo "building ${name}"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$LDFLAGS" -o "${OUT}/${name}" ./cmd/ngm
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
