#!/usr/bin/env bash
# gitee-release.sh —— 把**已有的**构建产物发布到 Gitee（唯一实现）。
#
# Windows 入口是 scripts/gitee-release.ps1，它**转调本文件**，不另写一份规则。
#
# 它不构建、不下载、不猜字节：输入是一个已经有 `SHA256SUMS` 的产物目录。
# 因此它既能接在 GitHub release 之后（下载同一批字节再上传），也能在本地对
# 一个刚 dry-run 出来的 `dist/` 目录先做完整校验。
#
# 用法：
#   scripts/gitee-release.sh --tag v0.99.0 --assets dist
#   scripts/gitee-release.sh --range v0.99.0..v0.101.0 --assets builds   # 每版 builds/<tag>/
#   scripts/gitee-release.sh --tag v0.99.0 --assets dist --dry-run       # 不联网、不写
#
# 选项：
#   --tag TAG             可重复；单个 tag 时 --assets 直接指向产物目录
#   --range A..B          按本地 tag 的版本序取闭区间（两端必须存在）
#   --assets DIR          产物目录；多 tag 时为父目录，实际用 DIR/<tag>
#   --repo OWNER/NAME     默认 idcu/ngm
#   --api-base URL        默认 https://gitee.com/api/v5
#   --target-branch NAME  创建 release 时的 target_commitish，默认 main
#                         （**必须是分支名而不是 tag 名**：Gitee 用 tag 名会返回
#                         一个没有 id 的响应，而不是报错）
#   --dry-run             打印将发出的请求与附件清单；**不做任何网络请求**
#   --force               同名附件已存在时先删后传（默认跳过，重跑安全）
#   -h, --help
#
# 凭据：只从环境变量 `GITEE_TOKEN` 读（不放进命令行，避免出现在进程列表/日志）。
# dry-run 不需要 token。
#
# 退出码：
#   0  成功（dry-run 时表示"计划已生成且产物自洽"）
#   1  上传/复核失败（已发生的写操作没有被当作成功）
#   2  用法、产物或凭据问题（**没有改动远端**）
#   3  查不动（网络/API/解析工具缺失；不报成功）
#
# 上传集合：6 个既有名字的二进制 + `SHA256SUMS`；版本化别名是 GitHub 侧给
# `.ngm-version` 拼地址用的，不重复传到 Gitee（Gitee 侧检查按 7 个判定）。
set -euo pipefail

REPO="idcu/ngm"
API_BASE="https://gitee.com/api/v5"
ASSETS=""
TARGET_BRANCH="main"
DRY_RUN=0
FORCE=0
RANGE=""
TAGS=()

usage() {
  sed -n '2,45p' "$0" | sed 's/^# \{0,1\}//'
}
usage_die() {
  echo "ERROR: $*" >&2
  usage >&2
  exit 2
}

while [ $# -gt 0 ]; do
  case "$1" in
    --tag)
      shift
      [ $# -gt 0 ] || usage_die "--tag needs a value"
      TAGS+=("$1")
      ;;
    --range)
      shift
      [ $# -gt 0 ] || usage_die "--range needs a value (A..B)"
      RANGE="$1"
      ;;
    --assets)
      shift
      [ $# -gt 0 ] || usage_die "--assets needs a directory"
      ASSETS="$1"
      ;;
    --repo)
      shift
      [ $# -gt 0 ] || usage_die "--repo needs OWNER/NAME"
      REPO="$1"
      ;;
    --api-base)
      shift
      [ $# -gt 0 ] || usage_die "--api-base needs a URL"
      API_BASE="${1%/}"
      ;;
    --target-branch)
      shift
      [ $# -gt 0 ] || usage_die "--target-branch needs a name"
      TARGET_BRANCH="$1"
      ;;
    --dry-run) DRY_RUN=1 ;;
    --force) FORCE=1 ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage_die "unknown argument: $1"
      ;;
  esac
  shift
done

case "$REPO" in
  */*) ;;
  *) usage_die "--repo must look like OWNER/NAME" ;;
esac
[ -n "$ASSETS" ] || usage_die "--assets is required"
[ -d "$ASSETS" ] || usage_die "assets directory does not exist: $ASSETS"

TOKEN="${GITEE_TOKEN:-}"
if [ "$DRY_RUN" -eq 0 ] && [ -z "$TOKEN" ]; then
  echo "ERROR: GITEE_TOKEN is not set (needs a Gitee token with releases permission)." >&2
  echo "       Nothing was changed. Use --dry-run to inspect the plan without a token." >&2
  exit 2
fi

# JSON 解析用 Python：Gitee 的“不存在”是 HTTP 200 + JSON null，正则切 JSON
# 会在嵌套对象上犯错（旧实现已经踩过一次）。缺 python 时明确 exit 3。
PY=""
if command -v python3 >/dev/null 2>&1; then
  PY="python3"
elif command -v python >/dev/null 2>&1; then
  PY="python"
fi
if [ -z "$PY" ]; then
  echo "ERROR: python3/python is required to parse the Gitee API response." >&2
  exit 3
fi

sha256_file() {
  # Git Bash 的 sha256sum 有时会在哈希前加 `\`（二进制模式标记）；
  # 去掉它，否则同一条清单在 Windows 上会被判成“不一致”。
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{ h = $1; sub(/^\\/, "", h); print h }'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{ h = $1; sub(/^\\/, "", h); print h }'
  else
    echo "ERROR: neither sha256sum nor shasum is available" >&2
    exit 3
  fi
}

redact() {
  # token 不在任何输出里留下（包括 API 的错误响应）。
  if [ -n "$TOKEN" ]; then
    printf '%s' "$1" | sed "s#${TOKEN}#<redacted>#g"
  else
    printf '%s' "$1"
  fi
}

# matrix 是构建与校验的**唯一目标声明**；Gitee 上传必须覆盖它的每一项。
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MATRIX="$ROOT/scripts/release-matrix.txt"
[ -f "$MATRIX" ] || { echo "ERROR: missing $MATRIX (the release target declaration)" >&2; exit 2; }

# ---------- 1. tag 选择 ----------
if [ -n "$RANGE" ]; then
  case "$RANGE" in
    *..*) ;;
    *) usage_die "--range must look like v0.99.0..v0.101.0" ;;
  esac
  start="${RANGE%%..*}"
  end="${RANGE#*..}"
  command -v git >/dev/null 2>&1 || { echo "ERROR: git is required for --range" >&2; exit 3; }
  git rev-parse --verify "refs/tags/$start" >/dev/null 2>&1 || usage_die "tag not found: $start"
  git rev-parse --verify "refs/tags/$end" >/dev/null 2>&1 || usage_die "tag not found: $end"
  picked="$(git tag --list 'v*' --sort=version:refname | awk -v a="$start" -v b="$end" '
    $0 == a { on = 1 }
    on { print }
    on && $0 == b { exit }
  ')"
  [ -n "$picked" ] || usage_die "range $RANGE selects no tags"
  while IFS= read -r t; do
    [ -n "$t" ] && TAGS+=("$t")
  done <<EOF
$picked
EOF
fi

[ "${#TAGS[@]}" -gt 0 ] || usage_die "give at least one --tag or --range"

# ---------- 2. 每个 tag 的产物完整性 ----------
# UPLOAD_NAMES 由 validate_tree 填充：6 个既有名字 + SHA256SUMS。
UPLOAD_NAMES=()
validate_tree() { # validate_tree <dir> <tag>
  local dir="$1" tag="$2" version sums line hash name names legacy versioned
  version="${tag#v}"
  sums="$dir/SHA256SUMS"
  [ -f "$sums" ] || { echo "ERROR: $dir/SHA256SUMS is missing" >&2; exit 2; }

  local -a lines legacy_names versioned_names
  lines=()
  legacy_names=()
  versioned_names=()
  while IFS= read -r line; do
    line="${line%$'\r'}"
    [ -n "$line" ] || continue
    hash="$(printf '%s' "$line" | awk '{ print $1 }')"
    name="$(printf '%s' "$line" | awk '{ print $2 }')"
    name="${name#\*}"
    case "$hash" in
      *[!0-9a-fA-F]*|'') echo "ERROR: $sums has a bad hash line: $line" >&2; exit 2 ;;
    esac
    [ "${#hash}" -eq 64 ] || { echo "ERROR: $sums has a non-sha256 line: $line" >&2; exit 2; }
    [ -n "$name" ] || { echo "ERROR: $sums has an empty filename: $line" >&2; exit 2; }
    [ -f "$dir/$name" ] || { echo "ERROR: $dir/$name is listed in SHA256SUMS but missing" >&2; exit 2; }
    got="$(sha256_file "$dir/$name")"
    [ "$got" = "$hash" ] || {
      echo "ERROR: $dir/$name sha256 mismatch: file=$got sums=$hash" >&2
      exit 2
    }
    lines+=("$name")
    case "$name" in
      "ngm-${version}-"*) versioned_names+=("$name") ;;
      ngm-*) legacy_names+=("$name") ;;
      *) echo "ERROR: unexpected asset name in SHA256SUMS: $name" >&2; exit 2 ;;
    esac
  done < "$sums"

  # matrix 的每一项都必须有一个既有名字的二进制。
  while IFS= read -r target; do
    target="${target%$'\r'}"
    case "$target" in ''|\#*) continue ;; esac
    os="${target%/*}"
    arch="${target#*/}"
    want="ngm-${os}-${arch}"
    [ "$os" = "windows" ] && want="${want}.exe"
    found=0
    for name in "${legacy_names[@]}"; do
      [ "$name" = "$want" ] && found=1
    done
    [ "$found" -eq 1 ] || { echo "ERROR: $tag is missing matrix asset $want" >&2; exit 2; }
  done < "$MATRIX"

  # 每个既有名字必须有一个同哈希的版本化别名（或整批都没有——旧布局）。
  if [ "${#versioned_names[@]}" -gt 0 ]; then
    [ "${#versioned_names[@]}" -eq "${#legacy_names[@]}" ] || {
      echo "ERROR: $tag has ${#legacy_names[@]} legacy names but ${#versioned_names[@]} versioned aliases" >&2
      exit 2
    }
    for name in "${legacy_names[@]}"; do
      want="ngm-${version}-${name#ngm-}"
      [ -f "$dir/$want" ] || { echo "ERROR: $tag is missing versioned alias $want" >&2; exit 2; }
      [ "$(sha256_file "$dir/$want")" = "$(sha256_file "$dir/$name")" ] || {
        echo "ERROR: $tag alias $want differs from $name" >&2
        exit 2
      }
    done
  fi

  [ "${#legacy_names[@]}" -eq 6 ] || {
    echo "ERROR: $tag has ${#legacy_names[@]} legacy binaries, expected 6 from $MATRIX" >&2
    exit 2
  }
  [ "${#lines[@]}" -eq "$(( ${#legacy_names[@]} + ${#versioned_names[@]} ))" ] || {
    echo "ERROR: $tag SHA256SUMS line count does not match asset count" >&2
    exit 2
  }

  UPLOAD_NAMES=("${legacy_names[@]}" "SHA256SUMS")
  echo "checked ${#lines[@]} checksum line(s): ${#legacy_names[@]} binaries + ${#versioned_names[@]} alias(es) + SHA256SUMS"
}

# ---------- 3. Gitee API 小工具 ----------
resp_file="$(mktemp)"
trap 'rm -f "$resp_file"' EXIT

api_get() { # api_get <url> -> body; sets HTTP_CODE
  body="$(curl -sS -o "$resp_file" -w '%{http_code}' \
    --connect-timeout 10 --max-time 60 \
    -H 'Accept: application/json' -H 'User-Agent: ngm-gitee-release' "$1" || echo 000)"
}

json_id() {
  printf '%s' "$1" | "$PY" -c 'import json,sys
try: d=json.load(sys.stdin)
except Exception: d=None
print(d.get("id","") if isinstance(d,dict) else "")'
}
json_field() { # json_field <json> <key>
  printf '%s' "$1" | "$PY" -c 'import json,sys
try: d=json.load(sys.stdin)
except Exception: d=None
v=(d or {}).get(sys.argv[1],"") if isinstance(d,dict) else ""
print("" if v is None else v)' "$2"
}
json_asset_names() {
  printf '%s' "$1" | "$PY" -c 'import json,sys
try: d=json.load(sys.stdin)
except Exception: d=None
for a in ((d or {}).get("assets") or []) if isinstance(d,dict) else []:
    n=(a or {}).get("name","")
    if n: print(n)'
}
json_asset_id() { # json_asset_id <json> <name>
  printf '%s' "$1" | "$PY" -c 'import json,sys
try: d=json.load(sys.stdin)
except Exception: d=None
for a in ((d or {}).get("assets") or []) if isinstance(d,dict) else []:
    if (a or {}).get("name")==sys.argv[1]:
        print((a or {}).get("id","")); break' "$2"
}

release_body() {
  printf '镜像自 GitHub 发行版 %s；附件按 SHA256SUMS 逐个校验。' "$1"
}
release_json() { # release_json <tag> <create|update>
  "$PY" -c 'import json,sys
tag, body, target, mode = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
d={"name":tag,"body":body}
if mode=="create":
    d.update({"tag_name":tag,"target_commitish":target})
print(json.dumps(d, ensure_ascii=False))' "$1" "$(release_body "$1")" "$TARGET_BRANCH" "$2"
}

# ---------- 4. 每个 tag：查 release → 建/更新 → 逐个上传并复核 ----------
for tag in "${TAGS[@]}"; do
  if [ "${#TAGS[@]}" -gt 1 ]; then
    dir="$ASSETS/$tag"
  else
    dir="$ASSETS"
  fi
  [ -d "$dir" ] || { echo "ERROR: assets directory for $tag not found: $dir" >&2; exit 2; }
  echo
  echo "== $tag =="
  validate_tree "$dir" "$tag"

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "[dry-run] target: $API_BASE/repos/$REPO/releases/tags/$tag"
    echo "[dry-run] GET    $API_BASE/repos/$REPO/releases/tags/$tag"
    echo "[dry-run] write  PATCH .../releases/<id> (if exists) OR POST .../releases (if JSON null)"
    echo "[dry-run] body   $(release_json "$tag" create)"
    echo "[dry-run] uploads (${#UPLOAD_NAMES[@]}):"
    for name in "${UPLOAD_NAMES[@]}"; do
      printf '  %-32s sha256=%s\n' "$name" "$(sha256_file "$dir/$name")"
    done
    continue
  fi

  echo "Gitee: $REPO release for $tag"
  api_get "$API_BASE/repos/$REPO/releases/tags/$tag?access_token=$TOKEN"
  case "$body" in
    200) ;;
    *) echo "ERROR: GET release returned HTTP $body: $(redact "$(cat "$resp_file")")" >&2; exit 3 ;;
  esac
  release_json_body="$(cat "$resp_file")"
  rid="$(json_id "$release_json_body")"
  if [ -n "$rid" ]; then
    echo "release exists (id=$rid) — updating, not creating a second one"
    code="$(curl -sS -o "$resp_file" -w '%{http_code}' -X PATCH \
      --connect-timeout 10 --max-time 60 \
      -H 'Content-Type: application/json; charset=utf-8' \
      -H 'Accept: application/json' -H 'User-Agent: ngm-gitee-release' \
      "$API_BASE/repos/$REPO/releases/$rid?access_token=$TOKEN" \
      --data "$(release_json "$tag" update)" || echo 000)"
    case "$code" in 2*) ;; *) echo "ERROR: PATCH release returned HTTP $code: $(redact "$(cat "$resp_file")")" >&2; exit 1 ;; esac
  else
    echo "no release yet — creating one"
    code="$(curl -sS -o "$resp_file" -w '%{http_code}' -X POST \
      --connect-timeout 10 --max-time 60 \
      -H 'Content-Type: application/json; charset=utf-8' \
      -H 'Accept: application/json' -H 'User-Agent: ngm-gitee-release' \
      "$API_BASE/repos/$REPO/releases?access_token=$TOKEN" \
      --data "$(release_json "$tag" create)" || echo 000)"
    case "$code" in 2*) ;; *) echo "ERROR: POST release returned HTTP $code: $(redact "$(cat "$resp_file")")" >&2; exit 1 ;; esac
    rid="$(json_id "$(cat "$resp_file")")"
    [ -n "$rid" ] || { echo "ERROR: create release returned no id: $(redact "$(cat "$resp_file")")" >&2; exit 1; }
  fi

  # 重新取一次，拿到**当前**附件清单（create 与 update 两条路径共用）。
  api_get "$API_BASE/repos/$REPO/releases/tags/$tag?access_token=$TOKEN"
  [ "$body" = "200" ] || { echo "ERROR: re-reading release returned HTTP $body" >&2; exit 3; }
  current="$(cat "$resp_file")"
  existing_names="$(json_asset_names "$current")"

  for name in "${UPLOAD_NAMES[@]}"; do
    exists=0
    while IFS= read -r n; do
      [ -n "$n" ] && [ "$n" = "$name" ] && exists=1
    done <<EOF
$existing_names
EOF
    if [ "$exists" -eq 1 ] && [ "$FORCE" -eq 0 ]; then
      echo "  $name already present — skipping (use --force to replace)"
      continue
    fi
    if [ "$exists" -eq 1 ]; then
      aid="$(json_asset_id "$current" "$name")"
      [ -n "$aid" ] || { echo "ERROR: cannot find asset id for $name" >&2; exit 1; }
      code="$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE \
        --connect-timeout 10 --max-time 60 -H 'User-Agent: ngm-gitee-release' \
        "$API_BASE/repos/$REPO/releases/$rid/attach_files/$aid?access_token=$TOKEN" || echo 000)"
      case "$code" in 2*) ;; *) echo "ERROR: DELETE $name returned HTTP $code" >&2; exit 1 ;; esac
      echo "  $name replaced (old attachment deleted)"
    fi

    want="$(sha256_file "$dir/$name")"
    code="$(curl -sS -o "$resp_file" -w '%{http_code}' -X POST \
      --connect-timeout 10 --max-time 300 \
      -H 'Accept: application/json' -H 'User-Agent: ngm-gitee-release' \
      "$API_BASE/repos/$REPO/releases/$rid/attach_files?access_token=$TOKEN" \
      -F "file=@$dir/$name" || echo 000)"
    case "$code" in 2*) ;; *) echo "ERROR: upload $name returned HTTP $code: $(redact "$(cat "$resp_file")")" >&2; exit 1 ;; esac
    url="$(json_field "$(cat "$resp_file")" browser_download_url)"
    [ -n "$url" ] || { echo "ERROR: upload $name returned no browser_download_url" >&2; exit 1; }
    back="$(mktemp)"
    curl -sS -o "$back" --connect-timeout 10 --max-time 120 "$url" || { rm -f "$back"; echo "ERROR: cannot download $name from Gitee" >&2; exit 1; }
    got="$(sha256_file "$back")"
    rm -f "$back"
    [ "$got" = "$want" ] || {
      echo "ERROR: $name read back from Gitee has sha256 $got, expected $want" >&2
      exit 1
    }
    echo "  $name uploaded and verified"
  done
done

echo
if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry-run complete (${#TAGS[@]} tag(s)); no request was sent."
else
  echo "Gitee release upload complete (${#TAGS[@]} tag(s))."
fi
