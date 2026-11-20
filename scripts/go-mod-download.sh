#!/usr/bin/env bash
# go-mod-download.sh — 带重试的 `go mod download`。
#
# 为什么要重试：gitee.com/idcu-go/modproof 的 fetch 会偶发 TLS 握手超时
# （2026-11-20：四个 Ubuntu job 在跑测试之前就失败在这里）。CI 已改为先走
# 代理（见 .github/workflows/ci.yml 的 GOPROXY），但网络抖动不会因此消失；
# 一次几十秒的重试比让整个 job 红掉便宜得多，而失败重试本身不影响产物——
# go.sum 仍逐字节校验模块内容。
#
# 用法：bash scripts/go-mod-download.sh [go mod download 的参数...]
set -uo pipefail

attempts="${GO_MOD_DOWNLOAD_ATTEMPTS:-3}"

for i in $(seq 1 "$attempts"); do
  if go mod download "$@"; then
    exit 0
  fi
  if [ "$i" -eq "$attempts" ]; then
    echo "go mod download failed after ${attempts} attempt(s)" >&2
    exit 1
  fi
  wait=$((i * 10))
  echo "go mod download failed (attempt ${i}/${attempts}); retrying in ${wait}s" >&2
  sleep "$wait"
done
