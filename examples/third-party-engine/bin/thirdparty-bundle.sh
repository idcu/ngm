#!/usr/bin/env bash
# 最小第三方 subprocess 引擎，遵守 P4 契约（docs/modules/p4-ecosystem.md）：
#   --version                    → 版本探测（engines validate 会 spawn 它）
#   --kind=<bundle>              → 能力类别，本脚本只实现 bundle
#   --outfile=<path>             → 产物落盘；不给则写 stdout
#   --<key>=<value>              → 选项（本演示引擎接受但不解释）
#   最后一个位置参数             → 入口文件
#   stdout=产物 / stderr=诊断 / 退出码=成败
set -euo pipefail

for arg in "$@"; do
  case "$arg" in
    --version)
      printf '0.1.0\n'
      exit 0
      ;;
  esac
done

kind=""
outfile=""
entry=""
for arg in "$@"; do
  case "$arg" in
    --kind=*) kind="${arg#--kind=}" ;;
    --outfile=*) outfile="${arg#--outfile=}" ;;
    -*) ;;                      # 其余选项按 P4 传入，演示引擎不解释
    *) entry="$arg" ;;
  esac
done

if [ "$kind" != "bundle" ]; then
  printf 'thirdparty-bundle: unsupported --kind=%s\n' "$kind" >&2
  exit 1
fi
if [ -z "$entry" ] || [ ! -f "$entry" ]; then
  printf 'thirdparty-bundle: entry file not found: %s\n' "$entry" >&2
  exit 1
fi

artifact="/* thirdparty-bundle 0.1.0 */"$'\n'"/* entry: $entry */"$'\n'"$(cat "$entry")"$'\n'

if [ -n "$outfile" ]; then
  mkdir -p "$(dirname "$outfile")"
  printf '%s' "$artifact" > "$outfile"
else
  printf '%s' "$artifact"
fi
