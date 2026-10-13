#!/usr/bin/env bash
# scripts/check-mirror-coverage.sh — 镜像侧的**实测**读数（v0.86）。
#
# 为什么要有它：仓库里那几处"GitHub 有 65 个 tag / 60 个 release"是**写死的事实**
# （2026-10-10 问过 API）。写死的读数会过期，而它过期的方式很安静——
# `undone 数` 那处就漏过两次（v0.84 复盘）。这条命令让同一件事**随时可再测一次**：
# 两次请求，把镜像的真实覆盖与"未推送 = 已交付 − 镜像 tag"算给人看。
#
# 它是**读数**，不是门禁（v0.85 那条教训的第二处应用）：
# 推送与否是仓库所有者的动作，本地树里没有任何东西能证明它做没做；
# 因此这里只打印（`UNPUSHED: N`），只有"问不到"才退出 3。
#
# 用法：scripts/check-mirror-coverage.sh [--repo OWNER/NAME] [--api-base URL]
#   --api-base 只为判据存在（指一个本地假 API），默认 https://api.github.com
#
# 退出码：0 = 读到了（哪怕有缺口）· 3 = 问不到（网络/限额/仓库不明 ⇒ **不报成功**）
set -euo pipefail

REPO_SLUG=""
API_BASE="https://api.github.com"
while [ $# -gt 0 ]; do
  case "$1" in
    --repo)
      shift
      REPO_SLUG="${1:-}"
      [ -n "$REPO_SLUG" ] || { echo "ERROR: --repo needs OWNER/NAME" >&2; exit 2; }
      ;;
    --api-base)
      shift
      API_BASE="${1:-}"
      [ -n "$API_BASE" ] || { echo "ERROR: --api-base needs a URL" >&2; exit 2; }
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      echo "usage: scripts/check-mirror-coverage.sh [--repo OWNER/NAME] [--api-base URL]" >&2
      exit 2
      ;;
  esac
  shift
done

cd "$(git rev-parse --show-toplevel)"

# 已交付版本数 = 复盘文件数（与既有的两支读法同源：文件系统派生，不靠记忆）。
delivered="$(ls -1 docs/development/v*-retrospective.md 2>/dev/null | wc -l | tr -d ' ')"
latest="$(ls -1 docs/development/v*-retrospective.md 2>/dev/null \
  | sed -e 's#.*/##' -e 's/-retrospective\.md$//' | sort -V | tail -1)"

if [ -z "$REPO_SLUG" ]; then
  origin="$(git remote get-url origin 2>/dev/null || true)"
  if [ -n "$origin" ]; then
    REPO_SLUG="$(printf '%s' "$origin" | sed -e 's#\.git$##' -e 's#:#/#' \
      | awk -F/ 'NF >= 2 { print $(NF-1) "/" $NF }')"
  fi
fi

echo "mirror coverage (live reading — the written-down numbers in docs/ are snapshots of this):"
echo "  local: delivered $delivered version(s), newest $latest"
if [ -z "$REPO_SLUG" ]; then
  echo "  could not determine the repository — pass --repo OWNER/NAME"
  exit 3
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "  curl not found: cannot ask the mirror"
  exit 3
fi

auth=()
if [ -n "${GITHUB_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
elif [ -n "${GH_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GH_TOKEN}"); fi

fetch() { # fetch <path> <body-file> -> http code
  curl -sS -o "$2" -w '%{http_code}' -H 'Accept: application/vnd.github+json' \
    ${auth[@]+"${auth[@]}"} "${API_BASE}/repos/${REPO_SLUG}$1" || echo 000
}

body_tags="$(mktemp)"
body_rel="$(mktemp)"
code_tags="$(fetch '/git/refs/tags' "$body_tags")"
code_rel="$(fetch '/releases?per_page=100' "$body_rel")"

if [ "$code_tags" != "200" ] || [ "$code_rel" != "200" ]; then
  echo "  could not read the mirror (tags HTTP $code_tags, releases HTTP $code_rel)"
  echo "  refusing to report success when the reading could not be taken"
  rm -f "$body_tags" "$body_rel"
  exit 3
fi

# tag 数：`/git/refs/tags` 一次给全（不翻页）；release 数：一页 100 条够用。
#
# 按**出现次数**数，不按行数：真实 API 是美化输出（一行一个字段），
# 但"一行一条"不是契约——紧凑 JSON 也是合法响应，按行数会把 65 个数成 1
# （v0.86 的判据就是这样把它抓出来的：假 API 回的是紧凑 JSON）。
tags="$(awk '{ n += gsub(/"ref":/, "") } END { print n + 0 }' "$body_tags")"
releases="$(awk '{ n += gsub(/"tag_name":/, "") } END { print n + 0 }' "$body_rel")"
newest_release="$(awk 'match($0, /"tag_name": *"([^"]+)"/, m) { print m[1]; exit }' "$body_rel" 2>/dev/null || true)"
if [ -z "$newest_release" ]; then
  newest_release="$(sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' "$body_rel" | head -1)"
fi
rm -f "$body_tags" "$body_rel"

unpushed=$((delivered - tags))
echo "  mirror (GitHub ${REPO_SLUG}): $tags tag(s), $releases release(s), newest release ${newest_release:-<none>}"
echo "  UNPUSHED: $unpushed tag(s)  (= delivered $delivered − mirror $tags)"
if [ "$unpushed" -gt 0 ]; then
  echo "  note: 推送是仓库所有者的动作；这条命令只报数，不判失败（读数 ≠ 门禁）"
fi
if [ "$tags" -lt "$delivered" ]; then
  echo "  hint: git push origin main --tags   （以及镜像/第二源的对应命令）"
fi
