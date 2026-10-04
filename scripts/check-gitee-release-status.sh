#!/usr/bin/env bash
#
# scripts/check-gitee-release-status.sh —— Gitee 侧的发布读数（**唯一实现**）。
#
# Windows 入口是 scripts/check-gitee-release-status.ps1，它**转调本文件**
# （与 check-docs-links 同一形状：要检查的规则只允许有一种写法，
# 两份实现会漂移，而漂移的代价是"你以为检查过了"）。
#
# 为什么需要这条读数：v0.12 复核抓到一个空白——check-release-status.sh --published 只问 GitHub。
# Gitee 侧"有没有这个发行版"此前只能靠人去点，于是状态页长出了一条错的
# "v0.1.0 ~ v0.4.0 两个源都可取到"（Gitee 上根本没有 v0.1.0 的发行版；2026-10-04 正是用
# 本检查调用的那个端点证伪的）。**一条读数胜过一句"已发布"。**
#
# 它**只读公开 API、不需要 token**，因此随时可跑；而"补发附件"仍然需要 GITEE_TOKEN
# （那是 upload-gitee-assets.ps1 的事）。
#
# 判据与 GitHub 侧有一处不同：Gitee 会给每个 release **自动附两个源码包**
# （<tag>.zip / <tag>.tar.gz），所以 API 里看到的是 9 个 asset，而我们自己传的是 7 个。
# 本检查按**我们自己传的那 7 个**判定，并把源码包单独计数——否则"9 个"会让人以为多了两个。
#
# 用法：
#     bash scripts/check-gitee-release-status.sh              # 本仓库全部 v* tag
#     bash scripts/check-gitee-release-status.sh v0.12.0 v0.13.0
#
# 退出码：0 = 全部就位；1 = 有缺（列出缺什么）；3 = **查不动**（缺工具 / 网络 / 限额）
# ——查不动**不报成功**，与 GitHub 侧同一条纪律。
#
# 匿名 API 有限额：短时间内连跑多次会被限流，此时它会把那些 tag 标成 NOT CHECKED 并 exit 3。
# **那不是"缺"，也不是失败**——它是"这次的读数不完整"，过一会儿重跑即可
# （这正是它不肯把"不知道"写成"没有"的地方）。
#
# 依赖：curl + python3。缺任何一个就**明确失败**（exit 3），
# 不做"正则切 JSON"的近似解析——那只能在今天的数据形状上凑对，
# 而这类"看起来在工作"的检查正是本项目反复吃亏的地方（见 check-docs-links.ps1 头部的事故）。
set -u

OWNER="${GITEE_OWNER:-idcu}"
REPO="${GITEE_REPO:-ngm}"
API_BASE="${GITEE_API_BASE:-https://gitee.com/api/v5}"
TIMEOUT="${GITEE_TIMEOUT:-20}"

# 我们自己传的 7 个附件：六个平台二进制 + SHA256SUMS。
EXPECTED=(
  ngm-darwin-amd64 ngm-darwin-arm64
  ngm-linux-amd64 ngm-linux-arm64
  ngm-windows-amd64.exe ngm-windows-arm64.exe
  SHA256SUMS
)

die() { echo "ERROR: $*" >&2; exit 3; }

command -v curl >/dev/null 2>&1 || die "curl is required (this reading is HTTP; refusing to approximate it)"
command -v python3 >/dev/null 2>&1 || die "python3 is required to parse the API response (refusing to regex-split JSON)"

# tag 清单：给了参数就用参数，否则取本仓库的 v* tag（git 是事实源，新版本自动进来）。
if [ "$#" -gt 0 ]; then
  tags=("$@")
else
  command -v git >/dev/null 2>&1 || die "git is required to list tags (or pass tags as arguments)"
  tags=()
  while IFS= read -r t; do
    [ -n "$t" ] && tags+=("$t")
  done < <(git tag --list 'v*' --sort=version:refname)
  [ "${#tags[@]}" -gt 0 ] || die "no v* tags found; refusing to report success on an empty list"
fi

# 把一个 release 响应折成两段信息：状态 + asset 名单。
# 三种形态都要接住：有 id（PRESENT）· JSON null / 没有 id（MISSING）· 根本不是 JSON（UNPARSEABLE）。
# 前两种的判据与 upload-gitee-assets.ps1 的 Get-GiteeRelease 一致——那个坑已经踩过一次。
read_release() {
  python3 -c '
import json, sys
raw = sys.stdin.read()
try:
    d = json.loads(raw)
except Exception:
    print("UNPARSEABLE")
    sys.exit(0)
if not isinstance(d, dict) or not d.get("id"):
    print("MISSING")
    sys.exit(0)
print("PRESENT")
for a in d.get("assets") or []:
    name = (a or {}).get("name", "")
    if name:
        print(name)
'
}

echo
echo "Gitee release status — $OWNER/$REPO (${#tags[@]} tag(s), public API, read-only)"
echo

ok=0
missing=()
unchecked=()

for tag in "${tags[@]}"; do
  url="$API_BASE/repos/$OWNER/$REPO/releases/tags/$tag"
  if ! body=$(curl -fsS --max-time "$TIMEOUT" -H 'User-Agent: ngm-release-read' "$url" 2>/dev/null); then
    unchecked+=("$tag")
    printf '%-9s %s\n' "$tag" 'NOT CHECKED'
    echo "          (request failed: network or rate limit — this is NOT a success)"
    continue
  fi

  out=$(printf '%s' "$body" | read_release)
  state=$(printf '%s\n' "$out" | head -n 1)

  case "$state" in
    UNPARSEABLE)
      unchecked+=("$tag")
      printf '%-9s %s\n' "$tag" 'NOT CHECKED'
      echo "          (the response was not JSON — refusing to guess whether a release exists)"
      continue
      ;;
    MISSING)
      missing+=("$tag")
      printf '%-9s %s\n' "$tag" 'MISSING (no release)'
      echo "          fix: powershell -File scripts/upload-gitee-assets.ps1 -Tag $tag"
      continue
      ;;
  esac

  # 源码包是 Gitee 自动附的，不算我们传的：按**名字**排除，而不是按数量相减
  # （相减会在 asset 形态变化时静默算错）。
  absent=()
  for want in "${EXPECTED[@]}"; do
    if ! printf '%s\n' "$out" | tail -n +2 | grep -Fxq "$want"; then
      absent+=("$want")
    fi
  done
  auto_sources=$(printf '%s\n' "$out" | tail -n +2 | grep -Fxc "$tag.zip" || true)
  auto_sources=$((auto_sources + $(printf '%s\n' "$out" | tail -n +2 | grep -Fxc "$tag.tar.gz" || true)))

  if [ "${#absent[@]}" -eq 0 ]; then
    ok=$((ok + 1))
    printf '%-9s %s\n' "$tag" "OK (7 attachments + $auto_sources source archive(s))"
  else
    missing+=("$tag")
    printf '%-9s %s\n' "$tag" "INCOMPLETE (missing ${#absent[@]})"
    echo "          missing: ${absent[*]}"
    echo "          fix: powershell -File scripts/upload-gitee-assets.ps1 -Tag $tag"
  fi
done

echo
if [ "${#unchecked[@]}" -gt 0 ]; then
  echo "NOT CHECKED: ${unchecked[*]}"
  echo "This is not a success: the reading is incomplete (network / rate limit / response shape)."
  exit 3
fi
if [ "${#missing[@]}" -gt 0 ]; then
  echo "missing: ${#missing[@]} of ${#tags[@]} (ok: $ok)"
  # 指针不是政策：这里只说明"缺"是否已经在别处被拍板过，以免有人把一条已知延后
  # 当成回归信号去查（而"会误报的门禁会被忽略"）。读数本身照旧只报状态。
  echo "note: back-filling historical releases is deferred by decision (2026-10-04, see"
  echo "      docs/development/README.md) — this reading reports state, it does not nag."
  exit 1
fi
echo "all present: $ok tag(s) carry the 7 attachments on Gitee."
exit 0
