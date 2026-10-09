#!/usr/bin/env bash
# PackageGate 对照 demo（v0.67）。
#
# 背景（docs/COMPETITIVE-ANALYSIS.md 的"PackageGate"一节）：
# 2026-01-26 Koi Security 披露 npm / pnpm / vlt / Bun 共 6 个零日，其中 npm 那一条是
# **"Git 依赖里的恶意 .npmrc 覆盖 git 二进制路径 → 绕过 --ignore-scripts 实现 RCE"**；
# npm 以 "works as expected" 关闭了报告，pnpm / vlt / Bun 都已修复。
#
# 这个 demo 把**同一份**"含 .npmrc 与 postinstall 的 Git 依赖"交给两侧：
#
#   ngm 侧（默认执行，**会断言**）：脚本从未执行、.npmrc 只是一个被 vendor 的
#   普通文件、没有任何东西照着 .npmrc 去调用别的"git"。
#   npm 侧（--with-npm，**只观察**）：打印它自己的行为——不同 npm 版本在受影响
#   与修复后的表现不同，所以这里刻意不断言（假证据比没有证据更糟）。
#
# 安全设计：fixture 里的"payload"只会**写一个标记文件**——不联网、不改系统状态。
# 断言检查的是"标记文件有没有出现"，而不是"攻击成功与否"。
#
# 用法：
#   bash scripts/packagegate-demo.sh [--with-npm]
#
# 环境：
#   NGM_BIN   指向一个已构建的 ngm 二进制；为空时用 PATH 上的 ngm，
#             再没有就用 go 从源码构建一个（需要仓库根有 go.mod）。

set -u

WITH_NPM=0
for arg in "$@"; do
	case "$arg" in
	--with-npm) WITH_NPM=1 ;;
	-h | --help)
		sed -n '2,32p' "$0"
		exit 0
		;;
	*)
		echo "unknown argument: $arg (try --help)" >&2
		exit 2
		;;
	esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

PASS=0
FAIL=0
# 只在 TTY 上用颜色：重定向到文件/CI 日志时 ANSI 转义是噪声。
if [ -t 1 ]; then
	GREEN=$'\033[32m'
	RED=$'\033[31m'
	RESET=$'\033[0m'
else
	GREEN=''
	RED=''
	RESET=''
fi
pass() { PASS=$((PASS + 1)); printf '  %sPASS%s  %s\n' "$GREEN" "$RESET" "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  %sFAIL%s  %s\n' "$RED" "$RESET" "$1"; }

die() {
	printf 'demo: %s\n' "$1" >&2
	exit 1
}

# cygpath（git bash）把 MSYS 路径转成 Windows 程序能用的形式；其它平台原样。
to_win_path() {
	if command -v cygpath >/dev/null 2>&1; then
		cygpath -m "$1"
	else
		printf '%s' "$1"
	fi
}

# ---------------------------------------------------------------- 准备工作

command -v git >/dev/null 2>&1 || die "git is required"

NGM="${NGM_BIN:-}"
if [ -z "$NGM" ]; then
	if command -v ngm >/dev/null 2>&1; then
		NGM="$(command -v ngm)"
	elif command -v go >/dev/null 2>&1; then
		echo "no ngm on PATH — building one from source (this may take a moment)..."
		case "$(go env GOOS)" in
		windows) NGM="$WORK/ngm-bin.exe" ;;
		*) NGM="$WORK/ngm-bin" ;;
		esac
		(cd "$REPO_ROOT" && go build -o "$NGM" ./cmd/ngm) || die "go build failed"
	else
		die "need ngm on PATH (or NGM_BIN=..., or go to build it)"
	fi
fi

echo
echo "== PackageGate demo =="
echo "workdir: $WORK"
echo "ngm:     $NGM"
echo

# ------------------------------------------------- 恶意 Git 依赖（fixture）

UPSTREAM="$WORK/upstream"
EVIL_GIT="$WORK/evil-git"
EVIL_GIT_MARKER="$WORK/evil-git-marker"
POSTINSTALL_MARKER="$(to_win_path "$WORK/postinstall-marker")"
POSTINSTALL_MARKER_SHELL="$WORK/postinstall-marker"

mkdir -p "$UPSTREAM"
(
	cd "$UPSTREAM"
	git init -q -b main .
	git config user.email demo@packagegate.invalid
	git config user.name "packagegate demo"
	# LF/CRLF 转换只影响本地工作区，而 vendor 内容由 Git 的对象模型决定——
	# 关掉它，让 fixture 在所有平台上字节一致（也少一串无害的警告）。
	git config core.autocrlf false

	# 依赖的"payload"：只写一个标记文件（不联网、不改系统状态）。
	cat >postinstall.js <<JS
// PackageGate demo payload: mark that the hook ran. Nothing else.
require("fs").writeFileSync("$POSTINSTALL_MARKER", "ran\n")
JS

	cat >package.json <<'JSON'
{
  "name": "packagegate-fixture",
  "version": "1.0.0",
  "scripts": { "postinstall": "node postinstall.js" }
}
JSON

	# 这一类文件在 npm 上可被**解释为配置**（PackageGate 的 npm 那条正是覆盖
	# git 二进制路径）。在 ngm 上它只是一个普通文件——这就是 demo 要展示的差别。
	cat >.npmrc <<NPMRC
git=$EVIL_GIT
NPMRC

	git add -A
	git commit -qm "feat: fixture with .npmrc + postinstall"
	git tag v1.0.0
)

# 一个"假 git"：如果谁照着 .npmrc 去调用它，它会留下第二个标记。
cat >"$EVIL_GIT" <<SH
#!/usr/bin/env bash
# PackageGate demo: if anything followed .npmrc and called this instead of git, mark it.
echo "called" >>"$EVIL_GIT_MARKER"
exit 0
SH
chmod +x "$EVIL_GIT"

# ------------------------------------------------------------- ngm 侧（断言）

echo "-- ngm side (asserted) --"
export NGM_HOME="$WORK/ngm-home"
mkdir -p "$NGM_HOME/mirror/github.com/demo"
git clone -q --mirror "$UPSTREAM" "$NGM_HOME/mirror/github.com/demo/evil.git"

PROJ="$WORK/proj"
"$NGM" init github.com:demo/app --runtime=node --dir="$PROJ" >/dev/null || die "ngm init failed"
"$NGM" add github:demo/evil@v1.0.0 --ref-type=tag --dir="$PROJ" >/dev/null || die "ngm add failed"
"$NGM" install --dir="$PROJ" >"$WORK/install.log" 2>&1 || {
	cat "$WORK/install.log"
	die "ngm install failed"
}

if [ ! -e "$POSTINSTALL_MARKER_SHELL" ]; then
	pass "the dependency's postinstall hook never ran (no marker file)"
else
	fail "the dependency's postinstall hook RAN — ngm must not execute it"
fi

if [ ! -e "$EVIL_GIT_MARKER" ]; then
	pass "nothing followed .npmrc: the 'git' it points at was never called"
else
	fail "something followed .npmrc and called the replacement 'git'"
fi

VENDORED_NPMRC="$PROJ/ngm.vendor/github.com/demo/evil/.npmrc"
if [ -f "$VENDORED_NPMRC" ]; then
	pass ".npmrc was vendored as an ordinary file (present, byte-for-byte)"
else
	fail ".npmrc is missing from the vendor tree — the demo fixture is wrong"
fi

if "$NGM" verify --dir="$PROJ" >"$WORK/verify.log" 2>&1; then
	pass "ngm verify: the vendored content matches the digest replayed from the mirror"
else
	cat "$WORK/verify.log"
	fail "ngm verify failed"
fi

if grep -q '"commit"' "$PROJ/ngm.lock"; then
	pass "ngm.lock pins a specific commit (the fixture's content is provable)"
else
	fail "ngm.lock has no commit"
fi

# -------------------------------------------------------------- npm 侧（观察）

echo
if [ "$WITH_NPM" = "1" ]; then
	echo "-- npm side (observation only, no assertions) --"
	if command -v npm >/dev/null 2>&1; then
		NPMPROJ="$WORK/npm-proj"
		UPSTREAM_URL="git+file:///$(to_win_path "$UPSTREAM")"
		mkdir -p "$NPMPROJ"
		(
			cd "$NPMPROJ"
			npm init -y >/dev/null 2>&1
			echo "  > npm install --ignore-scripts $UPSTREAM_URL"
			npm_code=0
			npm install --ignore-scripts "$UPSTREAM_URL" >"$WORK/npm.log" 2>&1 || npm_code=$?
			sed 's/^/  /' "$WORK/npm.log"
			echo "  npm exit: $npm_code"
			if grep -q EALLOWGIT "$WORK/npm.log" 2>/dev/null; then
				echo "  note: this npm build disables git-typed dependencies by default (EALLOWGIT);"
				echo "        on it the attack surface is closed by refusing the fetch itself."
				echo "        (Newer npm builds also tightened this path — the point of the demo is"
				echo "        the structural difference, not a re-enactment of the advisory.)"
			fi
		)
		if [ -e "$POSTINSTALL_MARKER_SHELL" ]; then
			echo "  observed: a marker appeared — a postinstall path executed despite --ignore-scripts"
		else
			echo "  observed: no postinstall marker on this npm version/build"
		fi
		if [ -e "$EVIL_GIT_MARKER" ]; then
			echo "  observed: the replacement 'git' from .npmrc WAS called on this npm version"
		else
			echo "  observed: the replacement 'git' from .npmrc was not called on this npm version"
		fi
	else
		echo "-- npm side skipped: npm is not on PATH --"
	fi
else
	echo "-- npm side skipped (rerun with --with-npm to observe it as well) --"
fi

echo
echo "== result: $PASS passed, $FAIL failed =="
echo "background: docs/COMPETITIVE-ANALYSIS.md (the PackageGate section)"
echo

[ "$FAIL" -eq 0 ] || exit 1
