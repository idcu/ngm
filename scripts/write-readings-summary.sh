#!/usr/bin/env bash
# 把"两个源的覆盖读数"写成 markdown —— 给 CI 的 job summary 用。
#
# 为什么要有它：读数是**给人看的**，而埋在 CI 日志里的读数没人看。
# v0.86 把两处写死的读数换成了"随时可再测的命令"，v0.88 再把它们放到每次流水线的
# 摘要页上（`$GITHUB_STEP_SUMMARY`）——同一份事实，换个地方被看见。
#
# 两条纪律（与仓库里其他判据/读数一致）：
#   ① **读不出就写读不出**：某个源没读成，摘要里必须有 ⚠️ 那一段，不许静默少一块；
#   ② **读数 ≠ 门禁**：本脚本**永远 exit 0**（缺口不是失败），把状态写进摘要即可。
#      唯一的例外是参数写错（exit 2）——那是用法错误，不是读数。
#
# 用法：
#   scripts/write-readings-summary.sh [--repo OWNER/NAME] [--api-base URL] [--out FILE]
#   --api-base 只为判据存在（指一个本地假 API），默认 https://api.github.com
#   --out 默认 stdout；CI 里：--out "$GITHUB_STEP_SUMMARY"
set -uo pipefail

out=""
extra=()
while [ $# -gt 0 ]; do
	case "$1" in
	--out)
		out="${2:-}"
		[ -n "$out" ] || { echo "ERROR: --out needs a file" >&2; exit 2; }
		shift 2
		;;
	--repo | --api-base)
		[ -n "${2:-}" ] || { echo "ERROR: $1 needs a value" >&2; exit 2; }
		extra+=("$1" "$2")
		shift 2
		;;
	*)
		echo "ERROR: unknown argument: $1" >&2
		echo "usage: scripts/write-readings-summary.sh [--repo OWNER/NAME] [--api-base URL] [--out FILE]" >&2
		exit 2
		;;
	esac
done

root="$(cd "$(dirname "$0")/.." && pwd)"

# 网络读数的时限：摘要页**不能因为一个源卡住而永远不出来**。
# 没有 `timeout`（少数环境）就直接跑，并在摘要里注明这次没有时限。
timeout_bin=""
if command -v timeout >/dev/null 2>&1; then
	timeout_bin="timeout 25"
fi

mirror="$( ${timeout_bin} bash "$root/scripts/check-mirror-coverage.sh" "${extra[@]+"${extra[@]}"}" 2>&1 )"
mirror_code=$?
gitee="$( ${timeout_bin} bash "$root/scripts/check-gitee-release-status.sh" 2>&1 )"
gitee_code=$?

tmp=$(mktemp)
{
	echo "## ngm 读数摘要（UTC $(date -u +%Y-%m-%d)）"
	echo
	echo "两个源的覆盖是**本次现场实测**（问各自的 API），不是文档里的快照。"
	echo "文档里的数字是快照、并写明日期与方法；这里是**再测一次**的结果。"
	echo
	echo "### GitHub 镜像"
	echo
	echo '```'
	echo "$mirror"
	echo '```'
	echo
	if [ "$mirror_code" -ne 0 ]; then
		echo "⚠️ 这一段**没读成**（exit $mirror_code）—— 没读成就说没读成，不当成\"没问题\"。"
		echo
	fi
	echo "### Gitee"
	echo
	echo '```'
	echo "$gitee"
	echo '```'
	echo
	if [ "$gitee_code" -ne 0 ]; then
		echo "⚠️ 这一段**没读成**（exit $gitee_code：通常是匿名限流或网络限制；退 3 时它把不确定的 tag 标成 NOT CHECKED）—— 同上。"
		echo
	fi
	if [ -z "$timeout_bin" ]; then
		echo "_注：本环境没有 \`timeout\`，上面两段没有时限。_"
	fi
} >"$tmp"

if [ -n "$out" ]; then
	cat "$tmp" >>"$out"
else
	cat "$tmp"
fi
rm -f "$tmp"

# 读数 ≠ 门禁（见头部）：永不用读数决定流水线的红绿。
exit 0
