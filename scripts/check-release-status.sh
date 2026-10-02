#!/usr/bin/env bash
# scripts/check-release-status.sh — "已交付"与"已发布"必须成对出现。
#
# 规则（对称三条）：
#   1. 有 docs/development/vX.Y-retrospective.md  ⇒ 必须有 vX.Y.* 的 tag
#   2. 有 vX.Y.* 的 tag                            ⇒ 必须有那一版的复盘
#   3. 每个 release tag 都是**附注 tag**（`git tag -a`）——轻量 tag 带不了说明
#
# 为什么判据是**复盘文件**，而不是计划里那行"状态：已交付"：
# 那一行是散文，格式历来不统一（v0.1 写"本计划已全部完成"，v0.2~v0.4 干脆没有这一行）。
# 用一个会漏的判据当门禁，得到的是"看起来在检查"——本项目把这种状态叫**仪器说谎**
# （见 docs/internals/metrics.md 与 v0.6 复盘 §5.1）。而复盘的位置是发布清单里写死的：
# **版本的最后一次提交是该版的复盘提交**，那正是第 0 步要打 tag 的地方。两条因此可以互推。
#
# 为什么需要这条检查：v0.2~v0.4 交付时没打 tag（结果这三版在仓库里存在、
# 在任何发行源上都取不到），发布清单因此补上了第 0 步"交付即打 tag"；
# **然后是 v0.5 ~ v0.8 又一次四版都没打**。写进清单挡不住它，因为这一步
# 不与代码同源、没有编译错误会提醒你，也没有任何自动检查会红。
# 这个脚本就是那个自动检查。
#
# 用法：scripts/check-release-status.sh [--published] [--repo OWNER/NAME]
#
#   无参数       离线三条（上面的规则 1~3）——CI 的门禁。
#   --published  **额外**去 GitHub 问一句：每个 tag 到底有没有 release、资产齐不齐。
#
# 为什么要有 --published：上面的规则只能证明"tag 存在"，**证明不了"产物能被下载"**。
# 2026-10-02 实测：`v0.5.0` ~ `v0.8.0` 四个 tag 都在 GitHub 上（附注对象完整），
# 但 `release.yml` **一次都没跑**（镜像转发了 tag，却没有产生那个 push 事件），
# 于是四条"已打 tag"的记录下面，一个 release 都没有——而当时没有任何检查会红。
# 离线检查与在线检查是**两件事**：前者管"仓库内部自洽"，后者管"对外真的发布了"。
#
# --published 需要网络与 curl；有 GITHUB_TOKEN / GH_TOKEN 时带上（匿名 API 限 60 次/小时）。
# 退出码：0 = 全部就绪；1 = 有缺（release 缺失或资产不全）；3 = 查不动（网络/限额/无法确定仓库，
# 此时**不**判为缺，但也**不**报成功）。
#
# CI 里需要 tag 在本地可见（workflow 用 actions/checkout 的 fetch-depth: 0）。
# 看不到 tag 时它会把**所有版本**判为"缺 tag"并说明原因——宁可吵闹，也不要静默通过。
#
# 实现约束（都是被反例测出来的）：**不要在命令替换里用 grep**。
# grep 在"没有匹配"时返回 1，而本脚本开着 `set -e -o pipefail`，那个 1 会让
# 整个脚本**静默退出、一个字都不打印**——恰好是"看不到 tag"时最该吵闹的场合。
# 所以下面一律用 awk（没有匹配也返回 0）做集合查询。
set -euo pipefail

PUBLISHED=0
REPO_SLUG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --published) PUBLISHED=1 ;;
    --repo)
      shift
      REPO_SLUG="${1:-}"
      if [ -z "$REPO_SLUG" ]; then
        echo "ERROR: --repo needs OWNER/NAME" >&2
        exit 2
      fi
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      echo "usage: scripts/check-release-status.sh [--published] [--repo OWNER/NAME]" >&2
      exit 2
      ;;
  esac
  shift
done

cd "$(git rev-parse --show-toplevel)"
RETRO_DIR="docs/development"

# ---------------------------------------------------------------- 收集两边
shopt -s nullglob
retro_files=("$RETRO_DIR"/v*-retrospective.md)
shopt -u nullglob

retro_list=""   # 每行一个 vX.Y
unrecognised=0
for f in "${retro_files[@]}"; do
  base="${f##*/}"
  if [[ "$base" =~ ^(v[0-9]+\.[0-9]+)-retrospective\.md$ ]]; then
    retro_list+="${BASH_REMATCH[1]}"$'\n'
  else
    echo "UNRECOGNISED retrospective filename: $f" >&2
    echo "  expected the form vX.Y-retrospective.md" >&2
    unrecognised=1
  fi
done

tag_list=""     # 每行 "vX.Y<TAB>vX.Y.Z"
tag_count=0
lightweight=""
while IFS= read -r tag; do
  [ -n "$tag" ] || continue
  tag_count=$((tag_count + 1))
  if [[ "$tag" =~ ^(v[0-9]+\.[0-9]+)\.[0-9]+ ]]; then
    tag_list+="${BASH_REMATCH[1]}"$'\t'"$tag"$'\n'
  else
    echo "UNRECOGNISED release tag: $tag" >&2
    echo "  expected the form vX.Y.Z (this check compares per minor version)" >&2
    unrecognised=1
  fi
  if [ "$(git cat-file -t "$tag" 2>/dev/null || true)" != "tag" ]; then
    lightweight+=" $tag"
  fi
done < <(git tag --list 'v*' | sort)

# ---------------------------------------------------------------- 查询（awk，不用 grep）
versions() {
  {
    printf '%s' "$retro_list" | awk -F'\t' 'NF { print $1 }'
    printf '%s' "$tag_list" | awk -F'\t' 'NF { print $1 }'
  } | sort -u
}
has_retro() { printf '%s' "$retro_list" | awk -F'\t' -v v="$1" 'NF && $1 == v { f = 1 } END { exit !f }'; }
has_tag() { printf '%s' "$tag_list" | awk -F'\t' -v v="$1" 'NF && $1 == v { f = 1 } END { exit !f }'; }
tag_name() { printf '%s' "$tag_list" | awk -F'\t' -v v="$1" 'NF && $1 == v { print $2; exit }'; }
count() { printf '%s' "$1" | awk 'NF' | wc -l | tr -d ' '; }

# ---------------------------------------------------------------- 报告
echo "release status (one retrospective per released version):"
echo "  saw $tag_count release tag(s), $(count "$retro_list") retrospective(s)"
echo

missing_tag=""
missing_retro=""
while IFS= read -r v; do
  [ -n "$v" ] || continue
  if has_retro "$v"; then r="retro OK "; else r="retro -- "; fi
  if has_tag "$v"; then t="tag $(tag_name "$v")"; else t="tag --"; fi
  verdict="OK"
  if ! has_retro "$v"; then
    verdict="MISSING RETROSPECTIVE"
    missing_retro+=" $v"
  fi
  if ! has_tag "$v"; then
    verdict="MISSING TAG"
    missing_tag+=" $v"
  fi
  printf '  %-7s %s  %-16s %s\n' "$v" "$r" "$t" "$verdict"
done < <(versions)

status=0

if [ -n "$missing_tag" ]; then
  status=1
  echo
  echo "ERROR: delivered but NOT tagged:$missing_tag"
  echo "  Release checklist step 0 (docs/development/README.md): a delivered version must be"
  echo "  tagged at its retrospective commit — otherwise it exists in the repo but is"
  echo "  downloadable from nowhere. That is exactly how v0.2~v0.4 and then v0.5~v0.8 went"
  echo "  missing on every release source."
  echo "  Fix (one per version): git tag -a vX.Y.0 -m 'ngm vX.Y.0 - <what changed>' <commit>"
  echo "                         git push origin vX.Y.0"
fi

if [ -n "$missing_retro" ]; then
  status=1
  echo
  echo "ERROR: tagged but NO retrospective:$missing_retro"
  echo "  The project's rule is to finish the previous version's retrospective before starting"
  echo "  the next one; a tag therefore implies a retrospective file"
  echo "  (docs/development/vX.Y-retrospective.md). v0.7 was delivered without one and it was"
  echo "  only noticed two versions later (see v0.7-retrospective.md §5.2)."
fi

if [ -n "$lightweight" ]; then
  status=1
  echo
  echo "ERROR: lightweight tag(s):$lightweight"
  echo "  Release tags must be annotated (git tag -a) so they can carry what the release is."
fi

if [ "$unrecognised" -ne 0 ]; then
  status=1
fi

if [ "$tag_count" -eq 0 ]; then
  status=1
  echo
  echo "ERROR: no tags visible at all. In CI the checkout must fetch them"
  echo "  (actions/checkout with fetch-depth: 0). Refusing to report success when the"
  echo "  instrument cannot see its input."
fi

if [ "$status" -eq 0 ]; then
  echo
  echo "every delivered version has a release tag, and every tag has a retrospective"
fi

# ---------------------------------------------------------------- 在线：真的发布了吗
if [ "$PUBLISHED" -eq 1 ]; then
  echo
  echo "published status (asks GitHub; this is the part that a tag cannot prove):"

  if [ -z "$REPO_SLUG" ]; then
    origin="$(git remote get-url origin 2>/dev/null || true)"
    if [ -n "$origin" ]; then
      # `host:owner/name.git` 与 `https://host/owner/name.git` 都兼容：取最后两段。
      REPO_SLUG="$(printf '%s' "$origin" | sed -e 's#\.git$##' -e 's#:#/#' \
        | awk -F/ 'NF >= 2 { print $(NF-1) "/" $NF }')"
    fi
  fi
  if [ -z "$REPO_SLUG" ]; then
    echo "  could not determine the repository — pass --repo OWNER/NAME"
    exit 3
  fi
  if ! command -v curl >/dev/null 2>&1; then
    echo "  curl not found: cannot ask GitHub whether the releases exist"
    exit 3
  fi

  auth=()
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  elif [ -n "${GH_TOKEN:-}" ]; then
    auth=(-H "Authorization: Bearer ${GH_TOKEN}")
  fi

  expected_assets=7   # 6 个平台二进制 + SHA256SUMS（发布清单第 3 步）
  missing_release=""
  thin_release=""
  inconclusive=""
  while IFS= read -r tag; do
    [ -n "$tag" ] || continue
    body="$(mktemp)"
    code="$(curl -sS -o "$body" -w '%{http_code}' \
      -H 'Accept: application/vnd.github+json' ${auth[@]+"${auth[@]}"} \
      "https://api.github.com/repos/${REPO_SLUG}/releases/tags/${tag}" || echo 000)"
    case "$code" in
      200)
        assets="$(awk '/"browser_download_url"/ { n++ } END { print n + 0 }' "$body")"
        sums="$(awk '/"browser_download_url"/ && /SHA256SUMS/ { n++ } END { print n + 0 }' "$body")"
        if [ "$assets" -ge "$expected_assets" ] && [ "$sums" -ge 1 ]; then
          printf '  %-8s release OK   (%s assets, SHA256SUMS present)\n' "$tag" "$assets"
        else
          printf '  %-8s INCOMPLETE   (%s assets, expected %s; SHA256SUMS present: %s)\n' \
            "$tag" "$assets" "$expected_assets" "$([ "$sums" -ge 1 ] && echo yes || echo no)"
          thin_release+=" $tag"
          echo "::warning::release $tag is incomplete: $assets assets, SHA256SUMS present: $sums"
        fi
        ;;
      404)
        printf '  %-8s MISSING      (tag exists, no release)\n' "$tag"
        missing_release+=" $tag"
        echo "::warning::no GitHub release for $tag — Actions → Release → Run workflow (input: $tag)"
        ;;
      *)
        printf '  %-8s COULD NOT CHECK (HTTP %s)\n' "$tag" "$code"
        inconclusive+=" $tag"
        ;;
    esac
    rm -f "$body"
  done < <(printf '%s' "$tag_list" | awk -F'\t' 'NF { print $2 }' | sort)

  if [ -n "$missing_release" ]; then
    status=1
    echo
    echo "ERROR: tagged but NOT published:$missing_release"
    echo "  A tag alone is not a release: the assets come from .github/workflows/release.yml,"
    echo "  and the mirror forwarding a tag does not reliably start it (measured 2026-10-02)."
    echo "  Fix: Actions → Release → Run workflow, with the tag as the input."
  fi
  if [ -n "$thin_release" ]; then
    status=1
    echo
    echo "ERROR: published but incomplete:$thin_release"
  fi
  if [ -n "$inconclusive" ]; then
    if [ -z "$missing_release$thin_release" ]; then
      echo
      echo "could not determine:$inconclusive (network or API rate limit?)"
      echo "  refusing to report success when the check could not run"
      exit 3
    fi
  fi
fi

exit "$status"
