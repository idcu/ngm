#!/usr/bin/env bash
# scripts/check-docs-links.sh —— 检查 docs/ 下的 Markdown 文件中的相对链接。
#
# 规则：
#   1. 仅扫描 docs/**/*.md
#   2. 匹配形如 [label](path) 的内联链接：path 若是 .md 相对路径，必须存在
#   3. 跳过：绝对 URL、纯锚点（#xxx）、跨仓链接（http/https）
#   4. CI 友好：发现首个坏链接即 exit 1，并打印文件:行号
#
# 用法：scripts/check-docs-links.sh [docs_root]
#   docs_root 默认 docs/
#
# 这是 M0 阶段的最小可用版本；M3+ 可替换为 lychee 等成熟工具。
set -euo pipefail

ROOT="${1:-docs}"
if [ ! -d "$ROOT" ]; then
  echo "no $ROOT directory; skipping"
  exit 0
fi

shopt -s globstar nullglob

bad=0
files=( "$ROOT"/**/*.md )
for f in "${files[@]}"; do
  # 跳过 node_modules / vendor 等
  case "$f" in
    */node_modules/*|*/vendor/*) continue ;;
  esac

  # 提取所有 [label](path) 中的 path
  while IFS= read -r line; do
    # 从 grep 输出 "行号:[label](target)" 中取出 target：
    # 截到最后一个 '('，再剥掉末尾的 ')'。
    #
    # 这里曾经截到 '['（而非 '('），于是 label 被当成路径的一部分——
    # 例如 [架构总览](../architecture/overview.md) 会解析为 "架构总览](../architecture/overview.md"。
    # 缺陷一直潜伏，因为 docs/ 此前不存在、本脚本从未真正执行过；
    # 文档并入 docs/ 后它第一次运行就红了 CI。
    target="${line##*\(}"
    target="${target%%)*}"
    # 忽略带 http(s)/ / mailto: / # 的
    case "$target" in
      ""|"#"*) continue ;;
      http*|https*|mailto:*|tel:*|file:*) continue ;;
    esac
    # 去掉锚点部分
    target="${target%%#*}"
    # 只校验 .md / .txt 形式（仓库内文档）
    case "$target" in
      *.md|*.txt) ;;
      *) continue ;;
    esac

    base="$(dirname "$f")"
    resolved="$base/$target"
    # 规范化 ../
    resolved="$(cd "$(dirname "$resolved" 2>/dev/null || echo .)" 2>/dev/null && pwd)/$(basename "$resolved")" 2>/dev/null || resolved=""
    if [ -z "$resolved" ] || [ ! -f "$resolved" ]; then
      echo "BROKEN LINK in $f: $line"
      bad=$((bad + 1))
    fi
  done < <(grep -nEo '\[[^]]+\]\([^)]+\)' "$f" 2>/dev/null || true)
done

if [ "$bad" -gt 0 ]; then
  echo "found $bad broken links"
  exit 1
fi
echo "all docs links OK"