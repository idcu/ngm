#!/usr/bin/env bash
# scripts/check-release-status.sh — "已交付"与"已发布"必须成对出现。
#
# 规则（对称三条）：
#   1. 有 docs/development/vX.Y-retrospective.md  ⇒ 必须有 vX.Y.* 的 tag
#   2. 有 vX.Y.* 的 tag                            ⇒ 必须有那一版的复盘
#      **例外：预发布 tag（vX.Y.Z-<suffix>，如 v0.102.0-dev.1）不要求复盘**。
#      它不标记"一个版本交付了"，而是给下游（ngm-repo 的 `.ngm-version`）一个
#      可直接下载的固定点；硬要它配一份复盘，等于把预发布读成正式交付。
#      例外只放开规则 2：预发布仍受规则 3（附注 tag）与 `--published`（真的发布了）约束。
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
#        scripts/check-release-status.sh --drift-guard [--drift-last N]
#            [--fixtures DIR] [--ngm-version-file PATH | --ngm-version-url URL]
#
#   无参数       离线三条（上面的规则 1~3）——CI 的门禁。
#   --published  **额外**去 GitHub 问一句：每个 tag 到底有没有 release、资产齐不齐。
#   --drift-guard 门禁：最近 N 个正式 tag（默认 3，且排除已声明不回填的历史区间）
#                 的 release 必须存在、附件与 `scripts/release-matrix.txt` 一致、
#                 `SHA256SUMS` 行数一致；并检查 ngm-repo 的 `.ngm-version`
#                 指向的版本确实有可下载产物。**这是 CI 的真门禁**，不是告警。
#   --fixtures DIR 离线判定：从 DIR 读 `<tag>.json`、`SHA256SUMS-<tag>`、
#                 `ngm-version`，用于验证判定逻辑本身（不联网）。
#
# --drift-guard 需要网络与 curl；CI 里带 GITHUB_TOKEN。查不动时 exit 3（不报成功）。
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
DRIFT=0
DRIFT_LAST=3
FIXTURES=""
NGM_VERSION_FILE=""
NGM_VERSION_URL=""
REPO_SLUG=""
while [ $# -gt 0 ]; do
  case "$1" in
    --published) PUBLISHED=1 ;;
    --drift-guard) DRIFT=1 ;;
    --drift-last)
      shift
      DRIFT_LAST="${1:-}"
      case "$DRIFT_LAST" in
        [1-9]|[1-9][0-9]*) ;;
        *) echo "ERROR: --drift-last needs a positive integer" >&2; exit 2 ;;
      esac
      ;;
    --fixtures)
      shift
      FIXTURES="${1:-}"
      if [ -z "$FIXTURES" ] || [ ! -d "$FIXTURES" ]; then
        echo "ERROR: --fixtures needs an existing directory" >&2
        exit 2
      fi
      ;;
    --ngm-version-file)
      shift
      NGM_VERSION_FILE="${1:-}"
      if [ -z "$NGM_VERSION_FILE" ] || [ ! -f "$NGM_VERSION_FILE" ]; then
        echo "ERROR: --ngm-version-file needs an existing file" >&2
        exit 2
      fi
      ;;
    --ngm-version-url)
      shift
      NGM_VERSION_URL="${1:-}"
      [ -n "$NGM_VERSION_URL" ] || { echo "ERROR: --ngm-version-url needs a URL" >&2; exit 2; }
      ;;
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
      echo "usage: scripts/check-release-status.sh [--published] [--drift-guard] [--repo OWNER/NAME]" >&2
      exit 2
      ;;
  esac
  shift
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
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

tag_list=""     # 每行 "vX.Y<TAB>vX.Y.Z"（正式版：参与 tag ↔ 复盘配对）
pre_list=""     # 每行 "vX.Y<TAB>vX.Y.Z-suffix"（预发布：只受附注 tag 与发布检查约束）
tag_count=0
pre_count=0
lightweight=""
while IFS= read -r tag; do
  [ -n "$tag" ] || continue
  tag_count=$((tag_count + 1))
  # 第四组捕获预发布后缀（`-dev.1` / `-rc.2` …）。后缀存在 ⇒ 不是交付版本。
  if [[ "$tag" =~ ^(v[0-9]+\.[0-9]+)\.[0-9]+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$ ]]; then
    if [ -n "${BASH_REMATCH[2]}" ]; then
      pre_list+="${BASH_REMATCH[1]}"$'\t'"$tag"$'\n'
      pre_count=$((pre_count + 1))
    else
      tag_list+="${BASH_REMATCH[1]}"$'\t'"$tag"$'\n'
    fi
  else
    echo "UNRECOGNISED release tag: $tag" >&2
    echo "  expected the form vX.Y.Z or vX.Y.Z-<pre-release suffix>" >&2
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

# ---------------------------------------------------------------- 工作树状态（v0.84）
#
# 把"开工 / 收口先看工作树"变成一处**读数**（不是门禁）。
#
# 起因是一次真实事故：v0.83 开工时没看工作树（`git log` 只说明最后一个提交在哪），
# 覆盖了**并行工作线**的两份**未跟踪**文件（docs/development/v0.81-{plan,retrospective}.md）
# ——未提交 ⇒ 没有 Git 副本、也没有 IDE 历史条目（工具直写磁盘）⇒ 只能重建。
# 教训写在 v0.83 复盘 §1：**未提交的工作等于没有版本保护**。
#
# 为什么不做成失败：开发中途工作树本来就不干净（未提交是常态），
# 一个恒红的门禁等于没有门禁。它的价值在于"每次收口都打印出来"——
# 于是"这里还有别人没提交的东西"会被看见，而不再依赖谁的记性。
if git rev-parse --git-dir >/dev/null 2>&1; then
  echo "worktree state (read this before you touch anything):"
  dirty_list="$(git status --porcelain)"
  dirty_count="$(printf '%s\n' "$dirty_list" | awk 'NF' | wc -l | tr -d ' ')"
  echo "  uncommitted (modified or untracked): $dirty_count file(s)"

  # 版本工件（plan / retrospective）里**未提交**的那些：
  # 它们是"没有版本保护"的高危文件——本行的存在理由就是那次覆盖事故。
  uncommitted_dev="$(git status --porcelain -- "$RETRO_DIR" \
    | awk 'NF { s = substr($0, 1, 2); p = substr($0, 4); if (s == "??" || s ~ /M/) print p }')"
  if [ -n "$uncommitted_dev" ]; then
    echo "  version artifacts NOT committed:"
    printf '%s\n' "$uncommitted_dev" | awk 'NF { print "    " $0 }'
    echo "    ^ 这些文件**没有版本保护**：被覆盖就永久丢失（v0.83 复盘 §1 的教训）。"
    echo "      收口时先提交它们，或至少复制到仓库外再动手。"
  fi

  # 已跟踪的复盘/计划被就地修改：本项目的纪律是"复盘是快照，不随后续删改修订"。
  edited_snapshots="$(git status --porcelain -- "$RETRO_DIR" | awk 'NF { s = substr($0, 1, 2); p = substr($0, 4); if (s ~ /M/) print p }')"
  if [ -n "$edited_snapshots" ]; then
    echo "  snapshot docs edited in place (expected to be immutable):"
    printf '%s\n' "$edited_snapshots" | awk 'NF { print "    " $0 }'
  fi
  echo
fi

# 收口清单：本脚本只查"tag ↔ 复盘"这一件事。**其余判据必须真的跑过**——
# v0.83 的撞号（两个文件都叫 ADR-028）本会被 v0.71 那张网抓到
# （`ADR 编号 028 被两个文件使用`），它没有红的原因不是判据缺失，而是**收口时没跑它**。
# 所以这里把"什么时候跑什么"打印出来，而不是留给记性。
echo "before you call it delivered:"
echo "  go test ./...                    # 全部判据（含 ADR 索引、读数、契约网）"
echo "  bash scripts/check-docs-links.sh # 文档链接"
echo

# ---------------------------------------------------------------- 报告
echo "release status (one retrospective per released version):"
echo "  saw $tag_count tag(s) ($pre_count pre-release), $(count "$retro_list") retrospective(s)"
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

if [ -n "$pre_list" ]; then
  echo
  echo "pre-release tag(s) (not a delivered version: no retrospective required):"
  printf '%s' "$pre_list" | awk -F'\t' 'NF { printf "  %-8s %s\n", $1, $2 }'
fi

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

  # 下限而非精确值：旧 release 是 6 个平台二进制 + SHA256SUMS = 7；v0.102 起
  # 同一份构建另带 6 个版本化别名 ⇒ 13。这里只判"资产齐不齐"，不把新旧读数混成一个。
  expected_assets=7
  missing_release=""
  thin_release=""
  inconclusive=""
  rate_limited=0
  while IFS= read -r tag; do
    [ -n "$tag" ] || continue
    if [ "$rate_limited" -eq 1 ]; then
      # 限额撞上之后**不再逐个去撞**：每个 tag 一次请求，10 个版本会把匿名配额
      # （60 次/小时）一次用掉六分之一，而结果全是同一句 403。
      printf '  %-8s NOT CHECKED (rate limit hit earlier)\n' "$tag"
      inconclusive+=" $tag"
      continue
    fi
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
      403|429)
        # 限额：说明一次，然后**不再逐个去撞**（见循环开头）。
        printf '  %-8s COULD NOT CHECK (HTTP %s)\n' "$tag" "$code"
        inconclusive+=" $tag"
        rate_limited=1
        echo "  note: GitHub 的匿名 API 限额是 60 次/小时（本检查每个 tag 一次请求）；"
        echo "       带上 GITHUB_TOKEN / GH_TOKEN 可到 5000 次/小时，否则等一会儿再试。"
        ;;
      *)
        printf '  %-8s COULD NOT CHECK (HTTP %s)\n' "$tag" "$code"
        inconclusive+=" $tag"
        ;;
    esac
    rm -f "$body"
  # 预发布同样必须真的发布（ngm-repo 的下载路径直接指向它），所以一并检查。
  done < <({
    printf '%s' "$tag_list"
    printf '%s' "$pre_list"
  } | awk -F'\t' 'NF { print $2 }' | sort)

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

# ---------------------------------------------------------------- 漂移守卫
#
# 这一节是**门禁**，不是读数：最近 N 个正式 tag 的 release 必须存在，附件集合
# 必须与 `scripts/release-matrix.txt` 一致，`SHA256SUMS` 行数必须等于
# “矩阵项 × 2”（既有名字 + 版本化别名）。另外校验 ngm-repo 的 `.ngm-version`
# 指向的版本真的有可下载产物——2026-11 那次断供的根因就是“清单钉了一个没有
# 产物的版本”。
#
# 已声明不回填的历史区间（v0.5~v0.8、v0.65~v0.98）全部早于 v0.99，
# 因此在选择“最近 N 个正式 tag”时被显式排除：只有 major>0 或 minor>=99
# 的正式 tag 进入候选。这是对决定的执行，不是对覆盖度的假装。
if [ "$DRIFT" -eq 1 ]; then
  echo
  echo "release drift guard (last ${DRIFT_LAST} formal tag(s) + ngm-repo pin):"
  echo "  excluded by decision: v0.5.0-v0.8.0 and v0.65.0-v0.98.0 (no backfill)"

  drift_fail=0
  drift_unknown=0

  if [ -z "${REPO_SLUG:-}" ]; then
    origin="$(git remote get-url origin 2>/dev/null || true)"
    if [ -n "$origin" ]; then
      REPO_SLUG="$(printf '%s' "$origin" | sed -e 's#\.git$##' -e 's#:#/#' \
        | awk -F/ 'NF >= 2 { print $(NF-1) "/" $NF }')"
    fi
  fi
  if [ -z "${REPO_SLUG:-}" ]; then
    echo "  ERROR: could not determine the repository — pass --repo OWNER/NAME"
    exit 3
  fi

  py=""
  if command -v python3 >/dev/null 2>&1; then py="python3"
  elif command -v python >/dev/null 2>&1; then py="python"; fi
  if [ -z "$py" ]; then
    echo "  ERROR: python3/python is required to parse release JSON (refusing to regex-split it)"
    exit 3
  fi

  # 用脚本自身位置而不是 cwd：本判据会在**合成仓库**里跑（cwd 是那个仓库），
  # 但目标矩阵属于这份实现。
  matrix_file="$SCRIPT_DIR/release-matrix.txt"
  if [ ! -f "$matrix_file" ]; then
    echo "  ERROR: missing $matrix_file — the build/guard target declaration"
    drift_fail=1
  fi
  matrix_targets="$(awk 'NF && $1 !~ /^#/ { print $1 }' "$matrix_file" 2>/dev/null || true)"
  matrix_count="$(printf '%s\n' "$matrix_targets" | awk 'NF' | wc -l | tr -d ' ')"
  if [ "$matrix_count" -eq 0 ]; then
    echo "  ERROR: $matrix_file declares no targets"
    drift_fail=1
  fi
  expected_sum_lines=$((matrix_count * 2))

  auth=()
  if [ -n "${GITHUB_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  elif [ -n "${GH_TOKEN:-}" ]; then auth=(-H "Authorization: Bearer ${GH_TOKEN}"); fi

  formal_tags="$(git tag --list 'v*' --sort=version:refname | awk -F. '
    /^v[0-9]+\.[0-9]+\.[0-9]+$/ {
      major = $1; sub(/^v/, "", major)
      if (major + 0 > 0 || $2 + 0 >= 99) print
    }')"
  selected_tags="$(printf '%s\n' "$formal_tags" | awk 'NF' | tail -n "$DRIFT_LAST")"
  selected_count="$(printf '%s\n' "$selected_tags" | awk 'NF' | wc -l | tr -d ' ')"
  if [ "$selected_count" -lt "$DRIFT_LAST" ]; then
    echo "  ERROR: only $selected_count candidate tag(s) visible; need $DRIFT_LAST (fetch tags with fetch-depth: 0)"
    drift_fail=1
  fi

  # check_one <tag> <label>：release 存在 / 附件集合 / SHA256SUMS 行数。
  check_one() {
    local tag="$1" label="$2" body_file sums_file code names count missing name
    local expected_names expected_count url sum_lines sum_names
    expected_names=""
    expected_count=0
    while IFS= read -r target; do
      [ -n "$target" ] || continue
      local os_name arch legacy versioned
      os_name="${target%/*}"
      arch="${target#*/}"
      legacy="ngm-${os_name}-${arch}"
      versioned="ngm-${tag#v}-${os_name}-${arch}"
      if [ "$os_name" = "windows" ]; then
        legacy="${legacy}.exe"
        versioned="${versioned}.exe"
      fi
      expected_names+="${legacy}"$'\n'"${versioned}"$'\n'
      expected_count=$((expected_count + 2))
    done <<EOF_MATRIX
$matrix_targets
EOF_MATRIX
    expected_names+="SHA256SUMS"
    expected_count=$((expected_count + 1))

    if [ -n "$FIXTURES" ]; then
      if [ ! -f "$FIXTURES/releases/$tag.json" ]; then
        printf '  %-18s MISSING release (fixture has no %s.json)\n' "$label" "$tag"
        drift_fail=1
        return 0
      fi
      body_file="$FIXTURES/releases/$tag.json"
    else
      body_file="$(mktemp)"
      code="$(curl -sS -o "$body_file" -w '%{http_code}' \
        --connect-timeout 10 --max-time 60 \
        -H 'Accept: application/vnd.github+json' ${auth[@]+"${auth[@]}"} \
        "https://api.github.com/repos/${REPO_SLUG}/releases/tags/${tag}" || echo 000)"
      case "$code" in
        200) ;;
        404)
          printf '  %-18s MISSING release (tag exists, no release)\n' "$label"
          drift_fail=1
          rm -f "$body_file"
          return 0
          ;;
        *)
          printf '  %-18s COULD NOT CHECK (HTTP %s)\n' "$label" "$code"
          drift_unknown=1
          rm -f "$body_file"
          return 0
          ;;
      esac
    fi

    names_file="$(mktemp)"
    if ! "$py" -c '
import json,sys
try:
    d=json.load(open(sys.argv[1], encoding="utf-8"))
except Exception:
    sys.exit(2)
if not isinstance(d, dict):
    sys.exit(2)
for a in d.get("assets") or []:
    n=(a or {}).get("name","")
    if n: print(n)
' "$body_file" > "$names_file" 2>/dev/null; then
      printf '  %-18s COULD NOT CHECK (release JSON is not readable)\n' "$label"
      drift_unknown=1
      rm -f "$names_file"
      [ -n "$FIXTURES" ] || rm -f "$body_file"
      return 0
    fi
    names="$(cat "$names_file")"
    rm -f "$names_file"

    count="$(printf '%s\n' "$names" | awk 'NF' | wc -l | tr -d ' ')"
    missing=""
    while IFS= read -r name; do
      [ -n "$name" ] || continue
      if ! printf '%s\n' "$names" | grep -Fxq "$name"; then
        missing+=" $name"
      fi
    done <<EOF_EXPECTED
$expected_names
EOF_EXPECTED
    if [ "$count" -ne "$expected_count" ] || [ -n "$missing" ]; then
      printf '  %-18s INCOMPLETE (%s assets, expected %s; missing:%s)\n' \
        "$label" "$count" "$expected_count" "${missing:- (extra assets)}"
      drift_fail=1
      [ -n "$FIXTURES" ] || rm -f "$body_file"
      return 0
    fi

    # SHA256SUMS：行数与“矩阵 × 2”一致，且每一行都是矩阵声明的二进制名。
    sums_file=""
    if [ -n "$FIXTURES" ]; then
      sums_file="$FIXTURES/SHA256SUMS-$tag"
      if [ ! -f "$sums_file" ]; then
        printf '  %-18s COULD NOT CHECK (fixture has no SHA256SUMS-%s)\n' "$label" "$tag"
        drift_unknown=1
        return 0
      fi
    else
      url="$("$py" -c '
import json,sys
d=json.load(open(sys.argv[1], encoding="utf-8"))
for a in d.get("assets") or []:
    if (a or {}).get("name")=="SHA256SUMS":
        print((a or {}).get("browser_download_url","")); break
' "$body_file" 2>/dev/null || true)"
      if [ -z "$url" ]; then
        printf '  %-18s COULD NOT CHECK (SHA256SUMS has no download URL)\n' "$label"
        drift_unknown=1
        rm -f "$body_file"
        return 0
      fi
      sums_file="$(mktemp)"
      if ! curl -fsSL --connect-timeout 10 --max-time 60 "$url" -o "$sums_file"; then
        printf '  %-18s COULD NOT CHECK (SHA256SUMS download failed)\n' "$label"
        drift_unknown=1
        rm -f "$body_file" "$sums_file"
        return 0
      fi
    fi

    sum_lines="$(awk 'NF' "$sums_file" | wc -l | tr -d ' ')"
    sum_names="$(awk 'NF { n=$2; sub(/^\*/, "", n); print n }' "$sums_file")"
    sum_missing=""
    while IFS= read -r name; do
      [ -n "$name" ] || continue
      if ! printf '%s\n' "$sum_names" | grep -Fxq "$name"; then
        sum_missing+=" $name"
      fi
    done <<EOF_SUMS
$(printf '%s\n' "$expected_names" | awk 'NF && $0 != "SHA256SUMS"')
EOF_SUMS
    if [ "$sum_lines" -ne "$expected_sum_lines" ] || [ -n "$sum_missing" ]; then
      printf '  %-18s INCOMPLETE (SHA256SUMS %s lines, expected %s; missing:%s)\n' \
        "$label" "$sum_lines" "$expected_sum_lines" "${sum_missing:- (extra lines)}"
      drift_fail=1
    else
      printf '  %-18s release OK (%s assets, SHA256SUMS %s lines)\n' \
        "$label" "$count" "$sum_lines"
    fi
    [ -n "$FIXTURES" ] || rm -f "$body_file"
    if [ -n "$FIXTURES" ]; then
      :
    else
      rm -f "$sums_file"
    fi
  }

  while IFS= read -r tag; do
    [ -n "$tag" ] || continue
    check_one "$tag" "$tag"
  done <<EOF_SELECTED
$selected_tags
EOF_SELECTED

  # ngm-repo 的固定版本：这是“门禁会不会再次断供”的直接判据。
  pin_file=""
  if [ -n "$NGM_VERSION_FILE" ]; then
    pin_file="$NGM_VERSION_FILE"
  elif [ -n "$FIXTURES" ]; then
    pin_file="$FIXTURES/ngm-version"
    if [ ! -f "$pin_file" ]; then
      echo "  pin              COULD NOT CHECK (fixture has no ngm-version)"
      drift_unknown=1
      pin_file=""
    fi
  else
    tmp_pin="$(mktemp)"
    pin_url="${NGM_VERSION_URL:-https://raw.githubusercontent.com/idcu/ngm-repo/master/.ngm-version}"
    if ! curl -fsSL --connect-timeout 10 --max-time 60 "$pin_url" -o "$tmp_pin"; then
      if [ -z "$NGM_VERSION_URL" ]; then
        pin_url="https://gitee.com/idcu/ngm-repo/raw/master/.ngm-version"
        if ! curl -fsSL --connect-timeout 10 --max-time 60 "$pin_url" -o "$tmp_pin"; then
          echo "  pin              COULD NOT CHECK (cannot read ngm-repo .ngm-version)"
          drift_unknown=1
          rm -f "$tmp_pin"
        fi
      else
        echo "  pin              COULD NOT CHECK (cannot read $pin_url)"
        drift_unknown=1
        rm -f "$tmp_pin"
      fi
    fi
    if [ -f "$tmp_pin" ] && [ -s "$tmp_pin" ]; then pin_file="$tmp_pin"; fi
  fi
  if [ -n "$pin_file" ]; then
    want="$(tr -d ' \r\n' < "$pin_file")"
    if [ -z "$want" ]; then
      echo "  pin              COULD NOT CHECK (empty .ngm-version)"
      drift_unknown=1
    else
      echo "  pin              ngm-repo .ngm-version = ${want}"
      check_one "v${want#v}" "pin:${want}"
    fi
    [ -n "$FIXTURES" ] || rm -f "$pin_file"
  fi

  if [ "$drift_fail" -eq 1 ]; then
    echo
    echo "ERROR: release drift guard failed — see the tag(s) above."
    status=1
  elif [ "$drift_unknown" -eq 1 ] && [ "$status" -eq 0 ]; then
    echo
    echo "could not determine part of the drift guard; refusing to report success."
    status=3
  fi
fi

exit "$status"
