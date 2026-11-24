#!/usr/bin/env bash
# 骨架：不联网、不下载工具；工具缺失时打印跳过说明而不是假装成功。
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v ngm >/dev/null 2>&1; then
  echo "SKIP: ngm not on PATH"
  exit 0
fi

ngm config validate

if command -v esbuild >/dev/null 2>&1; then
  ngm build --engine=esbuild
  ngm transform src/index.ts --engine=esbuild --outfile=dist/index.transformed.js
else
  echo "SKIP: esbuild not on PATH (no download attempted)"
fi
