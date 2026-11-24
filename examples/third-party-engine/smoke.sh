#!/usr/bin/env bash
# 离线冒烟骨架：不联网、不下载工具；缺工具时打印跳过说明而不是假装成功。
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v ngm >/dev/null 2>&1; then
  echo "SKIP: ngm not on PATH"
  exit 0
fi

ngm config validate

if ! command -v bash >/dev/null 2>&1; then
  echo "SKIP: bash not on PATH (no download attempted)"
  exit 0
fi

ngm engines validate
ngm build --engine=thirdparty-bundle --outfile=dist/index.js
test -f dist/index.js
