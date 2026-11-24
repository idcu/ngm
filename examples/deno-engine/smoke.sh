#!/usr/bin/env bash
# 骨架：不联网、不下载工具；工具缺失时打印跳过说明而不是假装成功。
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v ngm >/dev/null 2>&1; then
  echo "SKIP: ngm not on PATH"
  exit 0
fi

ngm config validate
ngm engines list --json

if command -v deno >/dev/null 2>&1; then
  ngm engines validate
  ngm typecheck --engine=deno
  ngm build --engine=deno
else
  echo "SKIP: deno not on PATH (no download attempted)"
fi
