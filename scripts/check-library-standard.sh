#!/usr/bin/env bash
# scripts/check-library-standard.sh —— **JS Git 库标准**的自检（唯一实现）。
#
# 标准写在 docs/guides/js-git-library-standard.md；本文件是它的判据。
# 两条纪律与本项目其它检查器一致：
#
#   1. **规则只允许有一种写法**：Windows 入口是 `.ps1`（转调本文件），不另写一份规则；
#      两份实现会漂移，而漂移的代价是"你以为检查过了"。
#   2. **失败必须点名并退出非 0**：必做项不满足 ⇒ exit 1；没有任何"降级成不做事"的路径。
#
# 用法：
#   scripts/check-library-standard.sh <dir>            # 必做项不满足 ⇒ exit 1
#   scripts/check-library-standard.sh <dir> --strict   # 推荐项也计入退出码
#   scripts/check-library-standard.sh <dir> --json     # 机器读（同一套事实）
#
# 依赖：`node`（读 package.json / ngm.json；被查的对象是 JS 库，node 一定在）。
# 找不到 node 时**明确失败**（exit 2）——不静默跳过。
#
# 退出码：
#   0  全部通过（--strict 下推荐项也通过）
#   1  有检查项未通过
#   2  无法检查（目录不存在 / 没有 node / package.json 读不出来）

set -uo pipefail

DIR=""
STRICT=0
JSON_OUT=0

usage() {
	sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
	case "$1" in
	--strict) STRICT=1 ;;
	--json) JSON_OUT=1 ;;
	-h | --help)
		usage
		exit 0
		;;
	-*)
		echo "unknown flag: $1" >&2
		usage >&2
		exit 2
		;;
	*)
		if [ -n "$DIR" ]; then
			echo "only one directory can be checked: $DIR and $1" >&2
			exit 2
		fi
		DIR="$1"
		;;
	esac
	shift
done

if [ -z "$DIR" ]; then
	echo "usage: $0 <dir> [--strict] [--json]" >&2
	exit 2
fi
if [ ! -d "$DIR" ]; then
	echo "not a directory: $DIR (nothing was checked)" >&2
	exit 2
fi
if ! command -v node >/dev/null 2>&1; then
	echo "node not found: this check reads package.json with node." >&2
	echo "install node, or run it yourself. refusing to report success without checking." >&2
	exit 2
fi

# win_path 把 bash 的路径翻译成 Windows 版 node 认得的路径。
#
# 为什么需要：Git bash 的 `mktemp` 给出的是 `/tmp/xxx` 这类 MSYS 路径，而本机的
# `node` 是 **Windows 二进制**——它把 `/tmp/xxx` 解析成 `D:\tmp\xxx` 并报 ENOENT。
# 两个工具链的路径不通是这台机器上反复出现的一类坑（Go 测试里 WSL 的 bash 同源）。
win_path() { # win_path <path>
	if command -v cygpath >/dev/null 2>&1; then
		cygpath -w "$1"
	else
		printf '%s' "$1"
	fi
}

# ---- 结果收集 ------------------------------------------------------------
# 每行：id|级别(must|should)|状态(pass|fail)|说明
#
# 分隔符刻意用 `|` 而不是制表符：本文件里出现过 `$'\t'` 被编辑器/工具转义成
# `$'\\t'` 的事故（判据静默地按错的分隔符切分）。用一个**不需要转义**的字符，
# 这类事故就没有发生的余地——与"规则只允许有一种写法"同一条理由。
CHECKS=""
# 角色（v0.77）：`package` = 一个包（默认）；`mono-root` = monorepo 的根（它**不是包**）。
#
# 为什么需要它：monorepo 的根没有入口、没有 exports、也不发布——把"包"的判据
# 套在它头上会把一份完全合格的仓库判成红的（v0.77 实测的误伤）。根的包专属判据
# 一律记为**不适用**：对那些问题，正确答案是"这里没有这个问题"。
ROLE="${NGM_LIB_STD_ROLE:-package}"
# ID 前缀：monorepo 里子包的结果必须能归属回去（`packages/a/pkg-name`）。
ID_PREFIX=""
record() { # record <level> <id> <status> <why>
	CHECKS="${CHECKS}${ID_PREFIX}${2}|${1}|${3}|${4}
"
}
pass() { record "$1" "$2" pass "$3"; }
fail() { record "$1" "$2" fail "$3"; }
# 不适用（例如"检查 tag"遇到没有 .git 的 vendored 副本）：**必须显式打印**，
# 但它不进任何计数——静默跳过才是这里真正的敌人。
skip() { record "$1" "$2" skip "$3"; }

# ---- monorepo 检测（v0.77 起，v0.78 扩到三种信号） ------------------------
#
# 三个信号合并去重：① npm —— `package.json` 的 `workspaces`；
# ② pnpm —— `pnpm-workspace.yaml` 的 `packages:`（只读简单形态：YAML 列表或内联数组）；
# ③ lerna —— `lerna.json` 的 `packages`。
#
# 检测必须在读 package.json **之前**：pnpm / lerna 的 monorepo 根**可以没有**根清单，
# 而那种形态的根同样"不是包"（v0.78 实测补上的第二处误伤）。
# 子进程带 `NGM_LIB_STD_CHILD=1` ⇒ 不做嵌套 monorepo 的递归（尚未验证的形态）。
MONO_PATTERNS=""
IS_CHILD=0
[ "${NGM_LIB_STD_CHILD:-0}" = "1" ] && IS_CHILD=1
# NESTED=1 表示"这次体检不完整"（检测到嵌套 monorepo，它的子包没有被体检）。
NESTED=0
if [ "$ROLE" = "package" ]; then
mono_raw=$(node -e '
		const fs = require("fs"), path = require("path");
		const dir = process.argv[1];
		const pats = [];
		// unparsed 统计"看得出来是子包声明、但本工具读不出的写法"（YAML 映射形式、
		// 内联对象等）。**读不出必须说出来**：静默丢一条 = 少体检一个子包。
		let unparsed = 0;
		const looksLikePattern = (s) => !/^[A-Za-z_][A-Za-z0-9_-]*\s*:/.test(s) && !/^\{.*:.*\}/.test(s);
		// ① npm
		try {
			const o = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
			if (Array.isArray(o.workspaces)) pats.push(...o.workspaces.map(String));
		} catch {}
		// ② pnpm（只读简单形态；`!` 开头的排除项不是子包）
		try {
			const y = fs.readFileSync(path.join(dir, "pnpm-workspace.yaml"), "utf8");
			let inList = false;
			for (const raw of y.split("\n")) {
				const line = raw.replace(/#.*$/, "").replace(/\s+$/, "");
				if (/^packages\s*:/.test(line)) {
					const inline = line.match(/\[(.*)\]/);
					if (inline) {
						for (const piece of inline[1].split(",")) {
							const v = piece.trim().replace(/^["\x27]|["\x27]$/g, "");
							if (!v) continue;
							if (looksLikePattern(v)) pats.push(v); else unparsed++;
						}
					}
					inList = !inline;
					continue;
				}
				if (!inList) continue;
				if (line !== "" && !/^\s/.test(line)) { inList = false; continue; }
				const m = line.match(/^\s*-\s*(.+)$/);
				if (m) {
					const v = m[1].trim().replace(/^["\x27]|["\x27]$/g, "");
					if (looksLikePattern(v)) pats.push(v); else unparsed++;
					continue;
				}
				// 列表块里既不是 `- ` 也不是空行 ⇒ 是某种映射写法（读不出来）
				if (line !== "") unparsed++;
			}
		} catch {}
		// ③ lerna
		try {
			const o = JSON.parse(fs.readFileSync(path.join(dir, "lerna.json"), "utf8"));
			if (Array.isArray(o.packages)) pats.push(...o.packages.map(String));
		} catch {}
		const uniq = [...new Set(pats.map((p) => String(p).trim()).filter((p) => p && !p.startsWith("!")))];
		// 分隔符用**换行**而不是逗号：glob 里可以合法地出现逗号（`{a,b}`），
		// 用它当分隔符会把一条模式切碎成两条垃圾（v0.78 实测）。
		process.stdout.write(uniq.join("\n") + "\n#unparsed=" + unparsed + "\n");
	' "$DIR" 2>/dev/null)
	# 最后一行是统计（`#unparsed=N`），其余每行一条模式（分隔符用换行，见上）。
	MONO_PATTERNS=$(printf '%s\n' "$mono_raw" | grep -v '^#unparsed=')
	MONO_UNPARSED=$(printf '%s\n' "$mono_raw" | sed -n 's/^#unparsed=\([0-9]*\)$/\1/p')
	[ -n "$MONO_PATTERNS" ] && ROLE="mono-root"

	# v0.80：**子包自己也是 monorepo**（嵌套形态）时，此前它会掉进"它是个包"的
	# 判据里——于是红的是 `entry-resolvable`（"找不到入口"），而真正的事实是
	# "它是个 monorepo 根"。**用错理由红，等于没红**：看报告的人会去给出入口，
	# 而那个仓库缺的不是入口。这里把它明说，并把它的子包数写出来（不递归 ⇒ 没体检）。
	if [ "$IS_CHILD" = 1 ] && [ "$ROLE" = "mono-root" ]; then
		declared=$(printf '%s' "$MONO_PATTERNS" | paste -sd' ' -)
		nested_children=$(node -e '
			const fs = require("fs"), path = require("path");
			const root = process.argv[1];
			const pats = String(process.argv[2]).split("\n").filter(Boolean);
			let n = 0;
			for (const p of pats) {
				const clean = p.replace(/\/+$/, "");
				if (!clean.endsWith("/*")) { n++; continue; }
				try {
					n += fs.readdirSync(path.join(root, clean.slice(0, -2)), { withFileTypes: true })
						.filter((e) => e.isDirectory()).length;
				} catch {}
			}
			process.stdout.write(String(n));
		' "$DIR" "$MONO_PATTERNS" 2>/dev/null)
		skip must nested-monorepo "它是 monorepo 根（声明：$declared；约 $nested_children 个子包**没有被体检**）—— 嵌套 monorepo 是**未验证的形态**，本工具不递归：与其假装查过，不如把没查过的说出来"
		# 这一次体检**不完整**：结论句不许说"符合标准"（见汇总段）。
		NESTED=1
	fi

	# 读不出来**不许猜**，也不许悄悄少体检一个子包：
	#  ① 有 N 条 packages 写法读不出来 ⇒ 显式失败（不猜那些写法的含义）；
	#  ② 文件在、里面也写着 packages，却一条都没解析出来 ⇒ 同上。
	if [ "${MONO_UNPARSED:-0}" -gt 0 ] 2>/dev/null; then
		fail must workspace-manifest-unreadable "有 $MONO_UNPARSED 条 packages 写法读不出来（标准只读简单形态：YAML 列表 / 内联字符串数组 / JSON 数组）—— 不予猜测，读不出的子包不会**偷偷**少体检"
	fi
	for mf in pnpm-workspace.yaml lerna.json; do
		if [ -f "$DIR/$mf" ] && [ -z "$MONO_PATTERNS" ] && grep -q 'packages' "$DIR/$mf" 2>/dev/null; then
			fail must workspace-manifest-unreadable "$mf 里写着 packages，但读不出子包列表（标准只读简单形态：YAML 列表 / 内联字符串数组 / JSON 数组）—— 不予猜测"
		fi
	done
fi

# ---- package.json -------------------------------------------------------
PKG="$DIR/package.json"
pkg_fields=""
if [ ! -f "$PKG" ]; then
	if [ "$ROLE" = "mono-root" ]; then
		# pnpm / lerna 的 monorepo 根可以没有根清单（npm 的 `workspaces` 则必须有——
		# 那正是信号本身）。没有根清单 ⇒ 根不是包 ⇒ 这条判据不适用。
		skip must pkg-json-exists "monorepo 根不是包（pnpm / lerna 形态可以没有根清单）——清单由各子包自己带"
	else
		fail must pkg-json-exists "根目录没有 package.json —— 入口推断只剩 index 约定，mappings 会退化成一个目录"
	fi
else
	pass must pkg-json-exists "package.json 存在"
	# node 一次性读出关心的字段（KEY=VALUE 行；值里不含换行）
	if ! pkg_fields=$(node -e '
		const fs = require("fs");
		const o = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
		const scripts = o.scripts || {};
		const hooks = ["preinstall","install","postinstall","prepare"].filter(k => k in scripts);
		const deps = Object.keys(o.dependencies || {});
		const gitDeps = deps.filter(n => /^(git|github:|gitlab:|bitbucket:|git\+|https?:)/.test(String((o.dependencies||{})[n])));
		const exp = o.exports;
		// exports 的解析**逐字对齐** internal/mappings 的 parseExports（v0.78）：
		// 字符串形式；`"."` 是字符串；`"."` 是条件映射（types → import/module/default/require 先到先得）。
		// 自检器比产品严是误判（实测：只写 exports 的包被判"找不到入口"，而我们**推荐** exports）。
		const parseExports = (raw) => {
			if (typeof raw === "string") return { main: raw, types: "" };
			if (raw === null || typeof raw !== "object") return { main: "", types: "" };
			const target = "." in raw ? raw["."] : raw;
			if (typeof target === "string") return { main: target, types: "" };
			if (target === null || typeof target !== "object") return { main: "", types: "" };
			const types = typeof target.types === "string" ? target.types : "";
			let main = "";
			for (const key of ["import", "module", "default", "require"]) {
				if (typeof target[key] === "string" && target[key] !== "") { main = target[key]; break; }
			}
			return { main, types };
		};
		const expEntry = parseExports(exp);
		const out = {
			name: o.name || "", version: o.version || "", type: o.type || "",
			license: o.license || "", main: o.main || "", types: o.types || "", exportsMain: expEntry.main, exportsTypes: expEntry.types,
			exportsShape: exp === undefined ? "none" : (typeof exp === "string" ? "string" : (exp["."] && typeof exp["."] === "object" ? "conditions" : "object")),
			hasFiles: !!o.files, hasEngines: !!o.engines,
			private: o.private === true ? "true" : "",
			workspaces: Array.isArray(o.workspaces) ? o.workspaces.join(",") : "",
			hooks: hooks.join(","), gitDeps: gitDeps.join(",")
		};
		for (const [k, v] of Object.entries(out)) console.log(k + "=" + v);
	' "$PKG" 2>/dev/null); then
		fail must pkg-json-valid "package.json 不是合法 JSON —— 入口推断与版本检查都无法进行"
	else
		pass must pkg-json-valid "package.json 是合法 JSON"
		while IFS='=' read -r k v; do
			[ -n "$k" ] && eval "P_$k=\$v"
		done <<EOF
$pkg_fields
EOF
	fi
fi

# ---- 子包清单展开（`dir/*` 与显式路径两种写法） ---------------------------
if [ "$ROLE" = "mono-root" ] && [ -n "$MONO_PATTERNS" ] && [ "$IS_CHILD" = 0 ]; then
	MONO_CHILDREN=$(node -e '
		const fs = require("fs"), path = require("path");
		const root = process.argv[1];
		const pats = String(process.argv[2]).split("\n").filter(Boolean);
		// 花括号展开（npm 与 pnpm 都支持 `{a,b}`）：一条 `{packages,apps}/*`
		// 等于两条模式。**不展开就是误判**——它会以"目录不存在"的形式红掉。
		const expandBraces = (p) => {
			const m = p.match(/\{([^{}]*)\}/);
			if (!m) return [p];
			const out = [];
			for (const alt of m[1].split(",")) {
				out.push(...expandBraces(p.slice(0, m.index) + alt + p.slice(m.index + m[0].length)));
			}
			return out;
		};
		const out = [];
		for (const raw of pats) {
			for (const p of expandBraces(raw)) {
				const clean = p.replace(/\/+$/, "");
				if (clean.endsWith("/*")) {
					const base = path.join(root, clean.slice(0, -2));
					let names = [];
					try {
						names = fs.readdirSync(base, { withFileTypes: true })
							.filter((e) => e.isDirectory()).map((e) => e.name).sort();
					} catch { continue; }
					const dir = clean.slice(0, -1);
					for (const n of names) out.push(dir + n);
				} else {
					out.push(clean);
				}
			}
		}
		process.stdout.write([...new Set(out)].join("\n"));
	' "$DIR" "$MONO_PATTERNS" 2>/dev/null)
else
	MONO_CHILDREN=""
fi

field() { # field <name>
	local name="P_$1"
	printf '%s' "${!name:-}"
}

if [ "$ROLE" = "mono-root" ] && [ ! -f "$PKG" ]; then
	# 没有根清单的 monorepo 根（pnpm / lerna 形态）：包名与版本都归各子包。
	skip must pkg-name "monorepo 根不是包（没有根清单）—— 包名由各子包自己带"
elif [ -n "${P_name:-}" ]; then
	pass must pkg-name "name = ${P_name}"
else
	fail must pkg-name "package.json 缺 name"
fi

if [ "$ROLE" = "mono-root" ] && [ ! -f "$PKG" ]; then
	skip must pkg-version "monorepo 根不是包（没有根清单）—— 版本由各子包自己带"
elif [ "$ROLE" = "mono-root" ] && [ "${P_private:-}" = "true" ] && [ -z "${P_version:-}" ]; then
	# 私有 monorepo 根不发布，没有 version 是正常的（v0.77：又一处"根的包专属判据"）。
	skip must pkg-version "私有 monorepo 根（private: true）不发布，version 可缺——版本由各子包自己带"
elif printf '%s' "${P_version:-}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$'; then
	pass must pkg-version "version = ${P_version}"
else
	fail must pkg-version "version 不是 semver（x.y.z），got 「${P_version:-<空>}」"
fi

# ---- 入口可解析（与 internal/mappings 的 InferEntry 三层同口径） --------
# 第 1 层：依赖自己的 ngm.json 的 main/types
ngm_main=""
ngm_types=""
if [ -f "$DIR/ngm.json" ]; then
	ngm_pair=$(node -e '
		const fs = require("fs");
		const o = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
		console.log((o.main || "") + " " + (o.types || ""));
	' "$DIR/ngm.json" 2>/dev/null || echo " ")
	ngm_main=$(echo "$ngm_pair" | cut -d' ' -f1)
	ngm_types=$(echo "$ngm_pair" | cut -d' ' -f2)
fi

# 候选顺序**逐条对齐** internal/mappings.InferEntry：
#   ① ngm.json 的 main/types → ② package.json 的 exports（优先）/ main / types
#   → ③ index 约定（含 .cjs——v0.78 对齐时补上的一个缺口）；
# 层内 exports 先于 main 字段（Go 侧就是这么写的：`if main == "" { main = pj.Main }`）。
entry_candidates=""
for c in "$ngm_main" "$ngm_types" "${P_exportsMain:-}" "${P_exportsTypes:-}" \
	"${P_main:-}" "${P_types:-}" "./index.ts" "./index.js" "./index.mjs" "./index.cjs"; do
	[ -n "$c" ] && entry_candidates="$entry_candidates $c"
done

entry_found=""
entry_layer=""
for c in $entry_candidates; do
	p="${c#./}"
	if [ -f "$DIR/$p" ]; then
		entry_found="$c"
		case "$c" in
		"$ngm_main" | "$ngm_types") [ -n "$c" ] && entry_layer="ngm.json" ;;
		*) [ "$c" = "./index.ts" ] || [ "$c" = "./index.js" ] || [ "$c" = "./index.mjs" ] && entry_layer="index 约定" || entry_layer="package.json" ;;
		esac
		break
	fi
done

if [ "$ROLE" = "mono-root" ]; then
	if [ "$IS_CHILD" = 1 ]; then
		skip must entry-resolvable "monorepo 根不是包——入口由各子包自己声明（**嵌套形态：它的子包没有被体检**，见 nested-monorepo）"
	else
		skip must entry-resolvable "monorepo 根不是包——入口由各子包自己声明（子包逐个体检，见下）"
	fi
elif [ -n "$entry_found" ]; then
	pass must entry-resolvable "入口 $entry_found（来自 $entry_layer，与 InferEntry 三层同口径）"
else
	fail must entry-resolvable "三层都没找到入口（ngm.json 的 main/types、package.json 的 exports/main、./index.ts|js）；mappings 会只剩一个目录"
fi

# 入口指向构建产物 ⇒ 产物必须被提交（ngm 不替依赖构建）
if [ "$ROLE" = "mono-root" ]; then
	skip must dist-committed "monorepo 根不是包——产物是否已提交由各子包自己承担"
elif [ -n "$entry_found" ]; then
	case "$entry_found" in
	dist/* | ./dist/* | build/* | ./build/* | lib/* | ./lib/*)
		# .gitignore 也可能写在**仓库根**（monorepo 的常态），因此两处都看。
		ignored_in="$DIR"
		if [ -f "$DIR/.gitignore" ] && grep -Eq '(^|/)(dist|build|lib)/?([[:space:]]|$)' "$DIR/.gitignore"; then
			ignored_in="$DIR"
		elif [ -n "${NGM_LIB_STD_MONO_ROOT:-}" ] && [ -f "$NGM_LIB_STD_MONO_ROOT/.gitignore" ] &&
			grep -Eq '(^|/)(dist|build|lib)/?([[:space:]]|$)' "$NGM_LIB_STD_MONO_ROOT/.gitignore"; then
			ignored_in="$NGM_LIB_STD_MONO_ROOT（仓库根）"
		else
			ignored_in=""
		fi
		if [ -n "$ignored_in" ]; then
			fail must dist-committed "入口指向构建产物（$entry_found），但它被 .gitignore 排除了（$ignored_in/.gitignore）—— ngm 不替依赖构建（钩子默认 deny），产物必须提交"
		else
			pass must dist-committed "入口指向构建产物（$entry_found）且未被 .gitignore 排除"
		fi
		;;
	esac
fi

# ---- 禁止项 -------------------------------------------------------------
if [ -n "${P_hooks:-}" ]; then
	fail must no-install-hooks "声明了安装期钩子：${P_hooks} —— ngm 默认不执行（postInstallPolicy=deny），靠它才能用的库 vendored 之后是坏的"
else
	pass must no-install-hooks "没有 preinstall/install/postinstall/prepare"
fi

if [ -e "$DIR/.npmrc" ]; then
	fail must no-npmrc "仓库里有 .npmrc —— ngm 全仓库零处读取它；而它正是 PackageGate 那条「覆盖 git 二进制路径」的载体"
else
	pass must no-npmrc "没有 .npmrc"
fi

if [ -e "$DIR/.gitmodules" ]; then
	fail must no-submodule "有 .gitmodules —— gitlink 无法被忠实重放（ngm 拒绝）"
elif [ -d "$DIR/.git" ] && git -C "$DIR" ls-files -s 2>/dev/null | awk '$1=="160000"' | grep -q .; then
	fail must no-submodule "索引里有 gitlink（160000）条目 —— 子模块不受支持"
else
	pass must no-submodule "没有子模块/gitlink"
fi

lfs_hits=""
if command -v grep >/dev/null 2>&1; then
	lfs_hits=$(grep -rl --exclude-dir=.git -m1 'version https://git-lfs.github.com/spec/v1' "$DIR" 2>/dev/null | head -3)
fi
if [ -n "$lfs_hits" ]; then
	fail must no-lfs-pointer "发现 LFS 指针文件（$(echo "$lfs_hits" | head -1 | sed "s|$DIR/||")）—— 指针不是内容，ngm 拒绝对其求哈希"
else
	pass must no-lfs-pointer "没有 LFS 指针"
fi

# symlink 不逃逸包目录
escaped=""
if command -v find >/dev/null 2>&1; then
	root_abs=$(cd "$DIR" && pwd)
	while IFS= read -r link; do
		target=$(readlink "$link")
		case "$target" in
		/*) abs="$target" ;;
		*) abs="$(cd "$(dirname "$link")" && pwd)/$target" ;;
		esac
		canonical=$(cd "$(dirname "$abs")" 2>/dev/null && pwd)
		case "$canonical/" in
		"$root_abs"/*) ;;
		*) escaped="${escaped:+$escaped, }${link#$root_abs/}" ;;
		esac
	done < <(find "$DIR" -path "$DIR/.git" -prune -o -type l -print 2>/dev/null)
fi
if [ -n "$escaped" ]; then
	fail must symlink-in-package "symlink 目标逃逸包目录：$escaped —— 读内容时会被拒绝，verify 报 critical"
else
	pass must symlink-in-package "symlink 目标都在包内（或没有 symlink）"
fi

# 文本文件统一 LF
# 用 node 扫，而不是 grep $'\r'：CR 的转义写法在不同工具链下会被改写，
# 而"判据按错的模式去搜"是一种静默失效（详见 record 处的注释）。
crlf=$(node -e '
const fs = require("fs"), path = require("path");
const root = process.argv[1];
const exts = [".js", ".ts", ".mjs", ".cjs", ".json", ".md", ".yml", ".yaml"];
const bad = [];
(function walk(d) {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    if (e.name === ".git" || e.name === "node_modules") continue;
    const p = path.join(d, e.name);
    if (e.isDirectory()) walk(p);
    else if (exts.includes(path.extname(e.name))) {
      const b = fs.readFileSync(p);
      if (b.includes(13)) bad.push(p.slice(root.length + 1)); // 13 = CR
    }
  }
})(root);
console.log(bad.slice(0, 3).join(", "));
' "$DIR" 2>/dev/null)
if [ -n "$crlf" ]; then
	fail must lf-only "这些文件含 CRLF（$(echo "$crlf" | head -1 | sed "s|$DIR/||")）—— digest 对原始字节求哈希，行尾混用会让同一份代码算出不同 digest"
else
	pass must lf-only "文本文件为 LF"
fi

# 库自带的 ngm.json（若有）必须能过 ngm 自己的校验：name/version/runtime 与 schemaVersion。
# 为什么要有这条：同一个文件既是"上游入口声明"又可能被当项目清单用——
# 缺 runtime 的清单会让 `ngm config validate` 在那个仓库里直接报错（v0.75 补的判据，
# 起因是标准页自己的示例漏了 runtime，被实现时抓出来）。
if [ -f "$DIR/ngm.json" ]; then
	ngm_problem=$(node -e '
		const fs = require("fs");
		let o;
		try { o = JSON.parse(fs.readFileSync(process.argv[1], "utf8")); } catch { console.log("PARSE_ERROR"); process.exit(0); }
		const errs = [];
		if (o.schemaVersion !== undefined && o.schemaVersion !== 1) errs.push("schemaVersion 只支持 1");
		if (typeof o.name !== "string" || o.name.trim() === "") errs.push("缺 name");
		if (typeof o.version !== "string" || o.version.trim() === "") errs.push("缺 version");
		if (o.runtime !== "node" && o.runtime !== "deno") errs.push("runtime 必须是 node 或 deno（当前：" + JSON.stringify(o.runtime) + "）");
		console.log(errs.join("；"));
	' "$DIR/ngm.json" 2>/dev/null)
	case "$ngm_problem" in
	"") pass must ngm-json-valid "ngm.json 能过 ngm 自己的校验（name/version/runtime 齐全）" ;;
	PARSE_ERROR) fail must ngm-json-valid "ngm.json 不是合法 JSON" ;;
	*) fail must ngm-json-valid "ngm.json 过不了 ngm 自己的校验：$ngm_problem" ;;
	esac
fi

if [ "$ROLE" = "mono-root" ] && [ ! -f "$PKG" ] && ! ls "$DIR" | grep -qiE '^(LICENSE|LICENCE|COPYING)'; then
	skip must license "monorepo 根不是包（没有根清单、也没有根 LICENSE）—— 许可证由各子包自己带"
elif [ -n "${P_license:-}" ]; then
	pass must license "license = ${P_license}"
elif ls "$DIR" | grep -qiE '^(LICENSE|LICENCE|COPYING)'; then
	pass must license "根目录有 LICENSE 文件"
else
	fail must license "既没有 license 字段也没有 LICENSE 文件 —— 没有许可证的 vendored 代码不可分发"
fi

# ---- 推荐项 -------------------------------------------------------------
if [ "$ROLE" = "mono-root" ]; then
	skip should esm-first "monorepo 根不是包——模块形态由各子包自己声明"
elif [ "${P_type:-}" = "module" ]; then
	pass should esm-first "type=module"
elif [ "${P_type:-}" = "commonjs" ]; then
	# v0.77：**显式**选择 CJS 就是本条要求的那件事（原措辞写的就是"CJS 需显式说明"）。
	# 判据不许把"已经写明的选择"判成没做到——那会让它无法被满足（实测缺陷）。
	pass should esm-first "type=commonjs（显式选择；ESM 是默认推荐，但写明的选择被尊重）"
else
	fail should esm-first "未声明 type（module 或 commonjs）—— 模块形态不该靠猜"
fi

if [ "$ROLE" = "mono-root" ]; then
	skip should exports-conditions "monorepo 根不是包——条件导出由各子包自己声明"
elif [ "${P_exportsShape:-none}" = "conditions" ]; then
	pass should exports-conditions "exports 带条件导出（types/import/require）"
elif [ "${P_exportsShape:-none}" = "string" ] || [ "${P_exportsShape:-none}" = "object" ]; then
	pass should exports-conditions "有 exports（非条件形态；推荐 types/import/require）"
else
	fail should exports-conditions "没有 exports —— 入口靠 main/types 猜"
fi

if [ "$ROLE" = "mono-root" ]; then
	skip should types-provided "monorepo 根不是包——类型入口由各子包自己声明"
elif [ -n "${P_types:-}" ] || [ -n "${P_exportsTypes:-}" ] || [ -n "$ngm_types" ] ||
	ls "$DIR"/*.d.ts >/dev/null 2>&1; then
	pass should types-provided "提供了类型入口"
else
	fail should types-provided "没有 types 字段、也没有根级 .d.ts —— TS 消费者只能靠约定"
fi

if [ "$ROLE" = "mono-root" ]; then
	if [ -f "$PKG" ]; then
		skip should publish-scope "monorepo 根不是包（private）——发布范围由各子包自己声明"
	else
		skip should publish-scope "monorepo 根不是包（没有根清单）——发布范围由各子包自己声明"
	fi
elif [ "${P_hasFiles:-false}" = "true" ] || [ -e "$DIR/.npmignore" ]; then
	pass should publish-scope "声明了发布范围（files 或 .npmignore）"
else
	fail should publish-scope "没声明发布范围 —— 测试与 CI 配置会一起进消费者的 vendor 目录"
fi

if [ -f "$DIR/README.md" ]; then
	pass should readme "有 README.md"
else
	fail should readme "没有 README.md"
fi

if [ "$ROLE" = "mono-root" ]; then
	# 根不发布 ⇒ engines（给消费者看的运行期约束）在根上没有意义（v0.78 实测：
	# lerna 形态的根有清单、没写 engines，被当成"没做到"）。
	skip should engines "monorepo 根不是包/不发布 —— 运行期约束由各子包自己声明"
elif [ "${P_hasEngines:-false}" = "true" ]; then
	pass should engines "声明了 engines"
else
	fail should engines "没声明 engines —— 消费者的运行期约束不可见"
fi

# LF 声明是**仓库级**的：monorepo 的子包不必自带 .gitattributes（v0.77 实测的误伤）。
gitattr_dir="$DIR"
[ -n "${NGM_LIB_STD_MONO_ROOT:-}" ] && gitattr_dir="$NGM_LIB_STD_MONO_ROOT"
if [ "${NGM_LIB_STD_MONO_ROOT:-}" = "" ] && [ ! -e "$DIR/.git" ]; then
	# v0.79：**vendored 的子路径**看不到仓库级声明（`.gitattributes` 在仓库根，
	# 而 ngm 只把那个子目录复制进来）。此前这条会**必然红**——每一个被
	# `subPath` 消费的依赖都会红一次，为一件它无法控制的事。
	# 与 `semver-tag` 同一条先例：看不到的事实**明说"不适用"**，而不是判它没做到；
	# 内容侧的 LF 仍由 `lf-only` 守（那条看的是文件本身，与仓库无关）。
	skip should gitattributes-lf "不是 git 仓库（vendored 副本 / 导出的子包）—— 看不到仓库级声明；内容侧的 LF 由 lf-only 守"
elif [ -f "$gitattr_dir/.gitattributes" ] && grep -Eq 'eol=lf|text=auto' "$gitattr_dir/.gitattributes"; then
	if [ "$gitattr_dir" = "$DIR" ]; then
		pass should gitattributes-lf ".gitattributes 声明了 LF"
	else
		pass should gitattributes-lf ".gitattributes 声明了 LF（仓库根 $(basename "$gitattr_dir")/，子包不必自带）"
	fi
else
	fail should gitattributes-lf "没有 .gitattributes 的 LF 声明 —— LF 应该由仓库强制，而不是靠人记"
fi

# tag 是 git 概念：**不是 git 仓库时不适用**（vendored 副本、导出的 tarball 都没有 .git，
# 而"那里没有 tag"不是缺陷——v0.76 用真实 vendored 副本跑出来的误报）。
if ! git -C "$DIR" rev-parse --git-dir >/dev/null 2>&1; then
	skip should semver-tag "不是 git 仓库（vendored 副本 / 导出物）——tag 检查不适用"
elif git -C "$DIR" tag --list 2>/dev/null | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+$'; then
	pass should semver-tag "有 semver tag"
else
	fail should semver-tag "没有 semver tag（tag 型依赖要求 tag 存在）"
fi

if [ -n "${P_gitDeps:-}" ]; then
	if [ -f "$DIR/ngm.json" ] && grep -q '"dependencies"' "$DIR/ngm.json"; then
		pass should git-deps-in-ngm-json "package.json 里的 Git 依赖也在 ngm.json 中声明"
	else
		fail should git-deps-in-ngm-json "package.json 里有 Git 依赖（${P_gitDeps}），但 ngm.json 没声明 —— ngm 只从 ngm.json 递归传递依赖，写在 package.json 里的只会被提示"
	fi
else
	pass should git-deps-in-ngm-json "package.json 里没有 Git 依赖（无需 ngm.json 声明）"
fi

# ---- 子包（monorepo）：用**同一条判据**再跑一遍，父进程只做归属（v0.77） ----
#
# 子包就是普通的包，所以跑的是同一支脚本（这是"规则只允许有一种写法"的直接兑现）；
# `NGM_LIB_STD_CHILD=1` 阻止嵌套 monorepo 的递归——那是一个尚未验证的形态，
# 静默地"反正也查了"比明确不做更坏。结果先按父进程的输出模式存好，
# 等父进程自己打印完再一并输出（顺序：根 → 子包）。
CHILD_JSON_FILES=""
CHILD_TEXT=""
child_fail=0
child_count=0
if [ "$ROLE" = "mono-root" ] && [ -n "$MONO_CHILDREN" ]; then
	child_args=""
	[ "$STRICT" = 1 ] && child_args="--strict"
	for rel in $MONO_CHILDREN; do
		if [ ! -f "$DIR/$rel/package.json" ]; then
			fail must workspace-package-json "workspaces 里的 $rel 没有 package.json —— 子包必须自成一个包"
			child_fail=1
			continue
		fi
		child_count=$((child_count + 1))
		if [ "$JSON_OUT" = 1 ]; then
			cf=$(mktemp)
			if ! NGM_LIB_STD_ROLE=package NGM_LIB_STD_CHILD=1 NGM_LIB_STD_MONO_ROOT="$DIR" bash "$0" "$DIR/$rel" $child_args --json >"$cf" 2>/dev/null; then
				child_fail=1
			fi
			CHILD_JSON_FILES="$CHILD_JSON_FILES $rel::$cf"
		else
			out=$(NGM_LIB_STD_ROLE=package NGM_LIB_STD_CHILD=1 NGM_LIB_STD_MONO_ROOT="$DIR" bash "$0" "$DIR/$rel" $child_args 2>&1) || child_fail=1
			CHILD_TEXT="${CHILD_TEXT}=== $rel ===
$out

"
		fi
	done
fi

# ---- 汇总 ---------------------------------------------------------------
must_fail=0
should_fail=0
skipped=0
while IFS='|' read -r id level status why; do
	[ -z "$id" ] && continue
	case "$status" in
	fail)
		if [ "$level" = must ]; then
			must_fail=$((must_fail + 1))
		else
			should_fail=$((should_fail + 1))
		fi
		;;
	skip) skipped=$((skipped + 1)) ;;
	esac
done <<EOF
$CHECKS
EOF

exit_code=0
[ "$must_fail" -gt 0 ] && exit_code=1
if [ "$STRICT" = 1 ] && { [ "$should_fail" -gt 0 ] || [ "$must_fail" -gt 0 ]; }; then
	exit_code=1
fi

if [ "$JSON_OUT" = 1 ]; then
	# 自己的判据先写成 JSON；有子包时由 node 合并（计数相加、id 按相对路径归属）。
	own_json=$(mktemp)
	{
		printf '{"dir":"%s","ok":%s,"complete":%s,"mustFail":%d,"shouldFail":%d,"skipped":%d,"strict":%s,"checks":[' \
			"$DIR" "$([ $exit_code -eq 0 ] && echo true || echo false)" "$([ "$NESTED" = 1 ] && echo false || echo true)" "$must_fail" "$should_fail" "$skipped" "$([ "$STRICT" = 1 ] && echo true || echo false)"
		first=1
		while IFS='|' read -r id level status why; do
			[ -z "$id" ] && continue
			[ "$first" = 1 ] || printf ','
			first=0
			printf '{"id":"%s","level":"%s","status":"%s","why":"%s"}' "$id" "$level" "$status" "$why"
		done <<EOF
$CHECKS
EOF
		printf ']}\n'
	} >"$own_json"

	if [ -n "$CHILD_JSON_FILES" ]; then
		# 交给 node 的路径必须是它认得的形态（见 win_path 的注释）。
		child_specs=""
		for spec in $CHILD_JSON_FILES; do
			rel="${spec%%::*}"
			child_specs="$child_specs $rel::$(win_path "${spec#*::}")"
		done
		node -e '
			const fs = require("fs");
			const own = JSON.parse(fs.readFileSync(process.argv[1], "utf8"));
			let broken = 0;
			for (const spec of process.argv.slice(2)) {
				const i = spec.indexOf("::");
				const rel = spec.slice(0, i), file = spec.slice(i + 2);
				let child = null;
				try { child = JSON.parse(fs.readFileSync(file, "utf8")); } catch { child = null; }
				if (child === null) {
					// 读不出子包的结果**不许静默丢**：它变成一条显式的失败。
					broken++;
					own.checks.push({
						id: rel + "/child-result-unreadable",
						level: "must", status: "fail",
						why: "子包的体检结果读不出来（临时文件路径或 JSON 有问题）—— 静默丢弃等于把没查过的当成查过了",
					});
					own.mustFail++;
					own.ok = false;
					continue;
				}
				for (const c of child.checks) {
					own.checks.push({ id: rel + "/" + c.id, level: c.level, status: c.status, why: c.why });
				}
				own.mustFail += child.mustFail;
				own.shouldFail += child.shouldFail;
				own.skipped += child.skipped;
				if (!child.ok) own.ok = false;
				// 子包"体检不完整"（嵌套 monorepo）⇒ 合并后的结论同样不完整。
				if (child.complete === false) own.complete = false;
			}
			process.stdout.write(JSON.stringify(own) + "\n");
		' "$(win_path "$own_json")" $child_specs
	else
		cat "$own_json"
	fi

	rm -f "$own_json"
	for spec in $CHILD_JSON_FILES; do rm -f "${spec#*::}"; done
	[ "$child_fail" -eq 0 ] || exit_code=1
	exit $exit_code
fi

while IFS='|' read -r id level status why; do
	[ -z "$id" ] && continue
	case "$status" in
	pass) printf '  ✔ %-22s %s\n' "$id" "$why" ;;
	fail) printf '  ✖ %-22s %s\n' "$id" "$why" ;;
	skip) printf '  — %-22s %s\n' "$id" "$why" ;;
	esac
done <<EOF
$CHECKS
EOF

echo
skip_note=""
if [ "$skipped" -gt 0 ]; then
	skip_note="（不适用 $skipped 条，见上）"
fi
if [ "$must_fail" -gt 0 ]; then
	echo "必做项未通过 $must_fail 条 —— 这个库不符合标准（docs/guides/js-git-library-standard.md）$skip_note"
elif [ "$NESTED" = 1 ]; then
	# "不完整"必须与"通过"分开说：子包没查过的东西，不能算在"符合"里。
	echo "已查的判据全部通过，**但这不是一次完整体检**：检测到嵌套 monorepo，它的子包没有被体检（见 nested-monorepo）$skip_note"
elif [ "$should_fail" -gt 0 ] && [ "$STRICT" = 1 ]; then
	echo "推荐项未通过 $should_fail 条（--strict）$skip_note"
elif [ "$should_fail" -gt 0 ]; then
	echo "必做项全部通过；推荐项未通过 $should_fail 条（不加 --strict 时不算失败）$skip_note"
else
	echo "全部通过 —— 这个库符合标准$skip_note"
fi

# 子包的结果放在根之后打印（顺序：根 → 子包），并折算进退出码。
[ "$child_fail" -eq 0 ] || exit_code=1
if [ -n "$CHILD_TEXT" ]; then
	printf '\n---- 子包（workspaces）：%d 个体检结果 ----\n\n' "$child_count"
	printf '%s' "$CHILD_TEXT"
	[ "$child_fail" -eq 0 ] || echo "子包里有未通过项 —— 合计不合格"
fi
exit $exit_code
