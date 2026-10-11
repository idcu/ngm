#!/usr/bin/env bash
#
# 扫你自己的 vendored 产物 —— 对 `ngm.mappings.json` 里的**每一个依赖**跑一遍
# `scripts/check-library-standard.sh`（v0.79）。
#
# 为什么事实源是 mappings 而不是"遍历 ngm.vendor/"：
# mappings 是"我 vendor 了什么"的**权威**（ngm 自己生成、`ngm mappings validate`
# 可校验）。遍历目录会把"prune 掉的残留""手工放进去的东西"也算成依赖——
# 同一份产物、同一份判据，结论却能飘。**一个事实只有一个来源。**
#
# 用法：
#   scripts/check-vendored-standard.sh <project-dir> [--strict] [--json]
#
#   --strict   推荐项也算失败（与库标准自检器同义）
#   --json     机器读（每个依赖一段，合并成一份；id 前缀 = 依赖名）
#
# 退出码：0 = 全部合格 · 1 = 有不合格 · 2 = 用法不对 / 读不到 mappings
#         （**2 不是"合格"**：没有 mappings 时"一个依赖都没查"与"全查过且都合格"必须能分开）
set -uo pipefail

usage() {
	cat <<'EOF'
ngm 库标准 · 扫 vendored 产物

USAGE:
  scripts/check-vendored-standard.sh <project-dir> [--strict] [--json]

对 ngm.mappings.json 里的每个依赖跑一遍库标准自检（scripts/check-library-standard.sh）。

EXIT CODES:
  0  全部依赖合格
  1  有依赖不合格
  2  用法不对 / 项目里没有 ngm.mappings.json（**没有查过 ≠ 查过且合格**）
EOF
}

DIR=""
STRICT=0
JSON_OUT=0
while [ $# -gt 0 ]; do
	case "$1" in
	--strict) STRICT=1 ;;
	--json) JSON_OUT=1 ;;
	-h | --help) usage; exit 0 ;;
	-*)
		echo "unknown flag: $1" >&2
		usage >&2
		exit 2
		;;
	*)
		[ -z "$DIR" ] || {
			echo "只接受一个项目目录（多余的：$1）" >&2
			exit 2
		}
		DIR="$1"
		;;
	esac
	shift
done

if [ -z "$DIR" ]; then
	usage >&2
	exit 2
fi
if [ ! -d "$DIR" ]; then
	echo "not a directory: $DIR" >&2
	exit 2
fi

HERE=$(cd "$(dirname "$0")" && pwd)
CHECKER="$HERE/check-library-standard.sh"
if [ ! -f "$CHECKER" ]; then
	echo "找不到自检器：$CHECKER" >&2
	exit 2
fi

MAP="$DIR/ngm.mappings.json"
if [ ! -f "$MAP" ]; then
	echo "没有 $MAP —— 先跑 \`ngm install\`（没有 mappings 就没有\"我 vendor 了什么\"的事实源）" >&2
	exit 2
fi

# win_path：bash 的路径 → Windows 版 node 认得的路径（见 check-library-standard.sh 的注释）。
win_path() {
	if command -v cygpath >/dev/null 2>&1; then
		cygpath -w "$1"
	else
		printf '%s' "$1"
	fi
}

# mappings：每行 `from|to`（`|` —— 本项目里唯一被证明不需要转义的分隔符）。
PAIRS=$(node -e '
	const fs = require("fs");
	const o = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
	const list = Array.isArray(o.mappings) ? o.mappings : [];
	for (const m of list) {
		if (!m || !m.from || !m.to) continue;
		console.log(String(m.from) + "|" + String(m.to));
	}
' "$MAP" 2>/dev/null)
if [ -z "$PAIRS" ]; then
	echo "$MAP 里没有可用的映射（mappings 为空或读不出来）—— 没有查过 ≠ 查过且合格" >&2
	exit 2
fi

count=0
failed_list=""
CHILD_JSON_FILES=""
child_fail=0
child_args=""
[ "$STRICT" = 1 ] && child_args="--strict"

while IFS='|' read -r from to; do
	[ -n "$from" ] || continue
	count=$((count + 1))
	rel="${to#./}"
	root="$DIR/$rel"

	if [ ! -d "$root" ]; then
		# 映射指向一个不存在的目录：**必须说出来**（它既不是"合格"也不是"跳过"）。
		if [ "$JSON_OUT" = 1 ]; then
			child_fail=1
			failed_list="$failed_list $from"
			tmp=$(mktemp)
			printf '{"dir":"%s","ok":false,"mustFail":1,"shouldFail":0,"skipped":0,"strict":false,"checks":[{"id":"vendored-dir-missing","level":"must","status":"fail","why":"mappings 指向的目录不存在：%s"}]}\n' \
				"$root" "$rel" >"$tmp"
			CHILD_JSON_FILES="$CHILD_JSON_FILES $from::$tmp"
		else
			child_fail=1
			failed_list="$failed_list $from"
			printf '\n── %s（%s）\n  ✖ %-22s %s\n' "$from" "$rel" "vendored-dir-missing" \
				"mappings 指向的目录不存在 —— 重新 \`ngm install\`，或先 \`ngm verify\` 看漂移"
		fi
		continue
	fi

	if [ "$JSON_OUT" = 1 ]; then
		tmp=$(mktemp)
		if ! bash "$CHECKER" "$root" $child_args --json >"$tmp" 2>/dev/null; then
			child_fail=1
			failed_list="$failed_list $from"
		fi
		CHILD_JSON_FILES="$CHILD_JSON_FILES $from::$tmp"
	else
		printf '\n── %s（%s）\n' "$from" "$rel"
		if ! bash "$CHECKER" "$root" $child_args; then
			child_fail=1
			failed_list="$failed_list $from"
		fi
	fi
done <<EOF
$PAIRS
EOF

if [ "$JSON_OUT" = 1 ]; then
	own=$(mktemp)
	printf '{"dir":"%s","ok":%s,"dependencies":%d,"strict":%s,"checks":[' \
		"$DIR" "$([ "$child_fail" -eq 0 ] && echo true || echo false)" "$count" \
		"$([ "$STRICT" = 1 ] && echo true || echo false)" >"$own"
	first=1
	for spec in $CHILD_JSON_FILES; do
		from="${spec%%::*}"
		file="${spec#*::}"
		[ "$first" = 1 ] || printf ',' >>"$own"
		first=0
		node -e '
			const fs = require("fs");
			const from = process.argv[1];
			const j = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
			for (const c of j.checks) process.stdout.write(JSON.stringify({
				id: from + "/" + c.id, level: c.level, status: c.status, why: c.why,
			}) + "\n");
		' "$from" "$(win_path "$file")" | paste -sd, - >>"$own"
	done
	printf ']}\n' >>"$own"
	cat "$own"
	rm -f "$own"
	for spec in $CHILD_JSON_FILES; do rm -f "${spec#*::}"; done
	[ "$child_fail" -eq 0 ] || exit 1
	exit 0
fi

echo
if [ "$child_fail" -eq 0 ]; then
	echo "全部合格 —— $count 个 vendored 依赖都符合库标准$([ "$STRICT" = 1 ] && echo "（--strict）")"
	exit 0
fi
echo "不合格的依赖：$failed_list"
echo "—— $count 个 vendored 依赖里有不合格项（判据：docs/guides/js-git-library-standard.md）"
exit 1
