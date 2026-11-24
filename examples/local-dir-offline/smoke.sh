#!/usr/bin/env bash
# 骨架：不联网、不下载工具；缺少锁文件时只说明跳过原因。
set -euo pipefail
cd "$(dirname "$0")"

if ! command -v ngm >/dev/null 2>&1; then
  echo "SKIP: ngm not on PATH"
  exit 0
fi

ngm config validate

if [ -f ngm.lock ]; then
  ngm install --offline --frozen-lockfile
  ngm verify --offline --content
else
  echo "SKIP: no local lock file yet; generate and commit it once, then this script runs fully offline"
fi
