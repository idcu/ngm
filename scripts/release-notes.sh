#!/usr/bin/env bash
# release-notes.sh —— 从 CHANGELOG.md 取某个版本的段落。
#
# 为什么不用 `gh release create --generate-notes`：那会按提交标题自动生成，
# 同一份发布说明会随提交顺序/合并方式漂移。发布说明应当是**写下来的**，
# 并且与 `ngm --version` 的版本号一一对应。
#
# 用法：
#   scripts/release-notes.sh v0.101.0                 # 输出到 stdout
#   scripts/release-notes.sh v0.101.0 --out notes.md  # 写到文件
#
# 退出码：0 = 找到且非空；3 = 没有这个版本的段落（发布前必须补 CHANGELOG）。
set -euo pipefail

tag="${1:-}"
out=""
if [ -z "$tag" ]; then
  echo "usage: scripts/release-notes.sh <tag> [--out FILE]" >&2
  exit 3
fi
shift || true
while [ $# -gt 0 ]; do
  case "$1" in
    --out)
      shift
      out="${1:-}"
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 3
      ;;
  esac
  shift
done

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="${tag#v}"

notes="$(awk -v ver="${version}" '
  BEGIN { pat = "## [" ver "]" }
  !found && index($0, pat) == 1 { found = 1; next }
  found && /^## / { exit }
  found { print }
' "${root}/CHANGELOG.md" | awk '
  NF { if (seen) { for (i = 0; i < blank; i++) print "" }; seen = 1; blank = 0; print; next }
  { if (seen) blank++ }
')"

if [ -z "${notes}" ]; then
  echo "ERROR: CHANGELOG.md has no section '## [${version}]'" >&2
  echo "       add it before publishing ${tag}; release notes are not generated from commit titles" >&2
  exit 3
fi

if [ -n "${out}" ]; then
  printf '%s\n' "${notes}" > "${out}"
  echo "wrote ${out} (${version})"
else
  printf '%s\n' "${notes}"
fi
