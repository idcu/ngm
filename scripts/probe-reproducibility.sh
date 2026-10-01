#!/usr/bin/env bash
# 可复现构建探针（本地 / CI 共用）。
#
# 它回答的问题来自 [ADR-013](../docs/adr/adr-013-remote-adapter.md) 的翻案条件之一：
# **"同一份源码 + 同一引擎版本 → 相同字节"**到底成立到什么程度。
#
# 用法：
#
#	bash scripts/probe-reproducibility.sh [label] [workdir]
#
# 环境变量：
#
#	PROBE_OUT   机器可读结果写到这里（供跨平台比对 job 消费）；默认 <workdir>/probe-result.json
#
# 它做三件事：
#
#  1. 在两个**内容相同、路径不同**的目录里各构建一次
#  2. 跑一个**对照**：改动一个源文件的一个字节后再构建，产物哈希**必须**变化
#  3. 把结果写成一行 `::notice::` **和**一份 JSON（跨机器比对靠后者）
#
# 覆盖的构建形态（v0.5 扩到 6 种）：
#
#	esbuild 打包 + 压缩 / esbuild sourcemap / tsc 声明 / postcss
#	+ **loader 链**（`.png` → dataurl、`.txt` → text）与 **CSS 压缩链**（v0.5 补）
#	+ **esbuild 插件（JS API）+ metafile**（v0.5 补）
#
# 最后一项是刻意补的：前几轮只证明了"CLI + 简单入口"可复现，而**真实项目走的是插件链**
# （Vite / Astro 都经 esbuild 的 JS API），而 metafile 里记着每个输入输出的路径——
# 那是"绝对路径泄漏进产物"最可能出现的地方。把它写进产物目录并参与哈希，
# 就把"插件链是否引入机器相关的字节"变成了一次逐字节比对。
#
# 为什么必须有对照：如果检查本身坏了（永远返回同一个值、或压根没跑到被测路径），
# "两次字节相同"照样成立，而它什么都没有证明。这与本项目另一条教训同源——
# "探测命令是否存在"曾被当成能力探测，于是 `deno run -e` 那种根本不存在的
# 形态被判为"支持"。
#
# 引擎版本**钉住并打印**：跨版本比较字节没有意义。
# 哈希用 node 而不是 sha256sum：后者在 macOS 与 Git Bash 里不存在。
#
# 结果以一行 `::notice::` 发出——Actions 日志匿名读不到，annotation 可以。

set -u

LABEL="${1:-local}"
WORK="${2:-${TMPDIR:-/tmp}/ngm-repro-probe}"

# 路径统一成绝对路径：下面的插件步骤以项目目录为参数，相对路径会随 `cd` 改变含义。
#
# Windows 的 `X:/...` 也算绝对——Git Bash 里 `/*` 匹配不到它，若按相对路径处理，
# 会拼出一个形如 `<cwd>/d:/repos/...` 的怪路径（本探针第一次扩展时就踩到过）。
abs_path() {
  case "$1" in
    /* | [A-Za-z]:[/\\]*) printf '%s' "$1" ;;
    *) printf '%s/%s' "$PWD" "$1" ;;
  esac
}
WORK=$(abs_path "$WORK")
OUT=$(abs_path "${PROBE_OUT:-$WORK/probe-result.json}")

rm -rf "$WORK"
mkdir -p "$WORK"

# 版本钉住，并在结果行里打印出来
ESB_VER="0.28.2"
TSC_VER="5.6.3"
POSTCSS_VER="11.0.0"
ESB="npx --yes esbuild@$ESB_VER"
TSC="npx --yes -p typescript@$TSC_VER tsc"
POSTCSS="npx --yes -p postcss-cli@$POSTCSS_VER postcss"

A1="--alias:github:demo/one=./ngm.vendor/github.com/demo/one"
A2="--alias:github:demo/two=./ngm.vendor/github.com/demo/two"

# 聚合哈希：按路径排序后逐个喂进去，路径统一用 `/`，使结果与平台无关。
# 既接受目录也接受单个文件——逐形态哈希里有些形态的产物就是一个文件。
hash_tree() {
  node -e '
    const fs = require("fs");
    const path = require("path");
    const c = require("crypto");
    const root = process.argv[1];
    const st = fs.statSync(root);
    const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) =>
      e.isDirectory() ? walk(path.join(d, e.name)) : [path.join(d, e.name)]);
    const files = (st.isDirectory() ? walk(root) : [root]).sort();
    const rel = (f) => (st.isDirectory() ? path.relative(root, f) : path.basename(f));
    const h = c.createHash("sha256");
    for (const f of files) {
      h.update(rel(f).split(path.sep).join("/"));
      h.update("\0");
      h.update(fs.readFileSync(f));
      h.update("\0");
    }
    process.stdout.write(h.digest("hex").slice(0, 16));
  ' "$1" 2>/dev/null
}

# run_one 调用一个引擎；输出收进日志，失败时把编译器自己的话原样带回来。
run_one() {
  local tag="$1"
  shift
  if ! "$@" >"$WORK/$tag.log" 2>&1; then
    head -c 300 "$WORK/$tag.log" | tr "\n" " "
    return 1
  fi
  return 0
}

# setup_tools 为插件那一步准备 `require("esbuild")` 能解析到的位置。
#
# 为什么不用 NODE_PATH：ESM 不认它，而 Windows 上它还要一个 Windows 形态的路径。
# 把**装好包的目录**和**脚本本身**放在一起，node 的解析规则就自然生效。
setup_tools() {
  mkdir -p "$WORK/tools"
  (
    cd "$WORK/tools" || exit 1
    npm install --no-save --silent --no-audit --no-fund "esbuild@$ESB_VER"
  ) >"$WORK/npm-install.log" 2>&1 || {
    head -c 300 "$WORK/npm-install.log" | tr "\n" " "
    return 1
  }

  cat > "$WORK/tools/plugin-build.cjs" <<'EOF'
// 一个"像真实项目"的 esbuild 插件链：把 `github:` 裸导入解析到 vendor。
// （ngm build 用 `--alias:` 做同一件事；Vite / Astro 走的是插件这条路。）
//
// 纪律：**不写入任何与机器或时间有关的东西**（Date.now()、绝对路径、主机名）。
// 那既是复现的前提，也正是这条探针要证的东西——如果插件非得写这些才能工作，
// "可复现"就不成立，而它应该在这里失败，而不是在用户的机器上。
const { build } = require("esbuild");
const fs = require("fs");
const path = require("path");

const projectDir = process.argv[2];
if (!projectDir) {
  console.error("usage: node plugin-build.cjs <project-dir>");
  process.exit(2);
}

const vendorIndex = (name) =>
  path.resolve(projectDir, "ngm.vendor/github.com/demo", name, "index.ts");

const alias = {
  "github:demo/one": vendorIndex("one"),
  "github:demo/two": vendorIndex("two"),
};

build({
  // 路径基准**显式给出**，不靠进程 CWD：ngm 自己也踩过两次这个坑
  // （清单校验、typedecl 输出目录都曾按 CWD 找错地方）。
  absWorkingDir: projectDir,
  entryPoints: ["src/index.ts"],
  outfile: "dist/plugin/app.js",
  bundle: true,
  minify: true,
  metafile: true,
  format: "esm",
  target: "es2020",
  logLevel: "silent",
  plugins: [
    {
      name: "ngm-vendor-alias",
      setup(b) {
        b.onResolve({ filter: /^github:/ }, (args) => {
          const p = alias[args.path];
          if (!p) return { errors: [{ text: "unmapped specifier: " + args.path }] };
          return { path: p };
        });
      },
    },
  ],
})
  .then((r) => {
    // metafile 里的路径是**相对 absWorkingDir** 的，因此它是"是否泄漏路径"最该被
    // 逐字节比对的地方。但**原始 metafile 的字节并不稳定**——这一条是实测出来的：
    //
    //   同一台机器、同一份源码、连续 20 轮：
    //     app.js（真正的产物）      → **1** 个不同取值
    //     JSON.stringify(metafile)  → **20** 个不同取值
    //   连"递归按键排序后"仍然 20 个：变的是**数组元素的顺序**
    //   （例如 inputs[*].imports），而不是键顺序。
    //
    // 也就是说：把原始 metafile 写进产物目录再哈希，测的是 esbuild 内部（多线程）
    // 的完成顺序，不是路径泄漏。第一次跨平台比对就是这么红的（macOS 上
    // treeA ≠ treeB，而 x86_64 两个平台一致到同一台机器的数字）。
    //
    // 因此写的是**规范化投影**：只保留"有哪些输入输出、各自多长、谁 import 谁"，
    // 数组按路径排序。它稳定 ⇔ 路径稳定——这才是这个形态想回答的问题。
    const canon = {
      inputs: Object.keys(r.metafile.inputs)
        .sort()
        .map((k) => ({
          path: k,
          bytes: r.metafile.inputs[k].bytes,
          imports: ((r.metafile.inputs[k].imports) || []).map((i) => i.path).sort(),
        })),
      outputs: Object.keys(r.metafile.outputs)
        .sort()
        .map((k) => ({
          path: k,
          bytes: r.metafile.outputs[k].bytes,
          inputs: Object.keys(r.metafile.outputs[k].inputs || {}).sort(),
        })),
    };
    fs.writeFileSync(
      path.join(projectDir, "dist/plugin/meta.paths.json"),
      JSON.stringify(canon, null, 2) + "\n"
    );
  })
  .catch((e) => {
    console.error(String(e && e.message ? e.message : e));
    process.exit(1);
  });
EOF
}

build_all() {
  local dir="$1" tag="$2" err=""
  local forms="$WORK/work/$tag.forms"
  : > "$forms"
  (
    cd "$dir" || exit 1

    # 每个形态写进**自己的子目录**，并立刻记下该形态产物的哈希。
    #
    # 为什么：(v0.5) 第一次跨平台比对红的时候，整棵树的哈希只告诉我们"某处不同"，
    # 定位靠的是事后推理。逐形态哈希让下一次失败自己说出是哪个形态。
    run_form() {
      local name="$1" artifact="$2"
      shift 2
      err=$(run_one "$tag-$name" "$@") || { echo "$err"; exit 1; }
      printf '%s=%s;' "$name" "$(hash_tree "$artifact")" >> "$forms"
    }

    run_form esbuild-min dist/min $ESB --bundle src/index.ts "$A1" "$A2" \
      --minify --outfile=dist/min/app.js
    run_form esbuild-map dist/map $ESB --bundle src/index.ts "$A1" "$A2" \
      --sourcemap --outfile=dist/map/sm.js
    # 注意：tsc 这一步用**无别名**的入口。ngm 的依赖命名（`github:demo/one`）
    # 只有 esbuild 经 `--alias:` 能解析，裸 tsc 会报 TS2307——那是夹具的形态，
    # 不是被测引擎的缺陷。
    run_form tsc-decl dist/types $TSC --emitDeclarationOnly --declaration \
      --outDir dist/types src/plain.ts
    run_form postcss dist/postcss $POSTCSS --output dist/postcss/app.css < src/app.css
    # loader 链 + 压缩：真实项目里 png / txt 这类资源都要经 loader 变成可打包的形态
    run_form esbuild-loader dist/assets $ESB --bundle src/with-assets.ts \
      --loader:.png=dataurl --loader:.txt=text \
      --minify --outfile=dist/assets/app.js
    # CSS 压缩链：postcss 那一步没有内建压缩，压缩由别的工具承担（这里用 esbuild）
    run_form esbuild-cssmin dist/css $ESB --bundle src/app.css \
      --minify --outfile=dist/css/app.min.css
    # 插件 + metafile（JS API）：Vite / Astro 走的就是这条路
    run_form esbuild-plugin dist/plugin node "$WORK/tools/plugin-build.cjs" "$dir"
    # 再把这个形态**拆成两个文件**分别记一笔：整形态不同时，下一步该查的是
    # "产物不同"还是"metafile 的路径内容不同"——这两件事的处置完全不同，
    # 而它们长得一样（都是"插件形态的哈希变了"）。
    printf 'plugin.appjs=%s;' "$(hash_tree dist/plugin/app.js)" >> "$forms"
    printf 'plugin.metapaths=%s;' "$(hash_tree dist/plugin/meta.paths.json)" >> "$forms"
  )
}

# 生成项目：入口 + 两级嵌套目录里的模块 + 两个依赖（镜像 vendor 布局）+ 两个资源
make_project() {
  local p="$1"
  mkdir -p "$p/src/util/deep" "$p/src/assets" \
           "$p/ngm.vendor/github.com/demo/one" \
           "$p/ngm.vendor/github.com/demo/two"

  printf 'export const one = 1;\n' > "$p/ngm.vendor/github.com/demo/one/index.ts"
  printf 'export const two = 2;\n' > "$p/ngm.vendor/github.com/demo/two/index.ts"

  for i in $(seq 1 20); do
    printf 'export const m%s = %s;\n' "$i" "$i" > "$p/src/util/m$i.ts"
  done
  for i in $(seq 1 10); do
    printf 'export const d%s = %s;\n' "$i" "$i" > "$p/src/util/deep/d$i.ts"
  done

  # 同样不带扩展名：tsc 不接受 `./util/m1.ts`（TS5097），而 esbuild 两种都收。
  printf 'export * from "./util/m1";\nexport * from "./util/deep/d1";\n' \
      > "$p/src/lib.ts"

  {
    printf 'import { one } from "github:demo/one";\n'
    printf 'import { two } from "github:demo/two";\n'
    printf 'import { m1 } from "./lib";\n'
    printf 'console.log(one + two + m1);\n'
  } > "$p/src/index.ts"

  # 给 tsc 的入口：只用相对路径、且**不带扩展名**——两种引擎都这样解析。
  # （esbuild 接受 `./lib.ts`，tsc 默认不接受：TS5097。）
  {
    printf 'import { m1 } from "./lib";\n'
    printf 'console.log(m1);\n'
  } > "$p/src/plain.ts"

  # 资源用**固定字节**：任何随机 / 时间相关的输入都会让"可复现"无从谈起。
  # 8 字节的 PNG 魔数足够让 loader 有事可做（dataurl 会把它 base64 进去）。
  printf '\211PNG\r\n\032\n' > "$p/src/assets/logo.png"
  printf 'note: fixed bytes\n' > "$p/src/assets/note.txt"

  {
    printf 'import logo from "./assets/logo.png";\n'
    printf 'import note from "./assets/note.txt";\n'
    printf 'export const banner = logo.length + note.length;\n'
  } > "$p/src/with-assets.ts"

  printf '.card { color: red; }\n.card .title { font-weight: 700; }\n' \
      > "$p/src/app.css"
}

fails=""
tools_err=$(setup_tools)
if [ -n "$tools_err" ]; then
  fails="$fails tools:[$tools_err]"
fi

for d in a bbbb-longer-directory-name; do
  make_project "$WORK/work/$d"
done

# 逐个构建：工作目录 = 项目目录，参数相对路径（与 ngm 的调用方式一致）
for d in a bbbb-longer-directory-name; do
  out=$(build_all "$WORK/work/$d" "$d")
  if [ -n "$out" ]; then
    fails="$fails $d:[$out]"
  fi
done

tree_a=$(hash_tree "$WORK/work/a/dist")
tree_b=$(hash_tree "$WORK/work/bbbb-longer-directory-name/dist")

# 逐形态哈希（`name=hash;`）：失败时自己说出是哪个形态，而不是只报"整棵树不同"。
forms_a=$(cat "$WORK/work/a.forms" 2>/dev/null)
forms_b=$(cat "$WORK/work/bbbb-longer-directory-name.forms" 2>/dev/null)

# 对照一：两个路径的产物应当**相同**（路径无关）
if [ -n "$tree_a" ] && [ "$tree_a" = "$tree_b" ]; then
  same="yes"
else
  same="no"
fi

# 对照二：改动一个字节后，产物哈希**必须**变化
# 少了这一步，"两次相同"可能只是说明检查本身坏了。
cp -r "$WORK/work/a" "$WORK/work/ctrl" 2>/dev/null
printf 'export const changed = 1;\n' >> "$WORK/work/ctrl/src/lib.ts"
build_all "$WORK/work/ctrl" "ctrl" >/dev/null 2>&1
tree_c=$(hash_tree "$WORK/work/ctrl/dist")

if [ -n "$tree_a" ] && [ "$tree_a" != "$tree_c" ]; then
  ctrl="sensitive"
else
  ctrl="INSENSITIVE"
fi

if [ "$same" = "yes" ] && [ "$ctrl" = "sensitive" ]; then
  verdict="REPRODUCIBLE"
else
  verdict="NOT-REPRODUCIBLE"
fi

esb_got=$($ESB --version 2>/dev/null)
tsc_got=$($TSC --version 2>/dev/null)

# 机器可读的结果：跨平台比对 job 读它，人也读它（比一行 annotation 好读）。
mkdir -p "$(dirname "$OUT")"
cat > "$OUT" <<EOF
{
  "label": "$LABEL",
  "os": "${RUNNER_OS:-$(uname -s)}",
  "arch": "$(uname -m)",
  "node": "$(node --version 2>/dev/null)",
  "esbuild": "$esb_got",
  "tsc": "$tsc_got",
  "treeA": "$tree_a",
  "treeB": "$tree_b",
  "ctrl": "$tree_c",
  "formsA": "$forms_a",
  "formsB": "$forms_b",
  "same_across_paths": "$same",
  "ctrl_sensitive": "$ctrl",
  "verdict": "$verdict",
  "fails": "${fails# }"
}
EOF

echo "::notice::PROBE $LABEL esbuild=$esb_got tsc=$tsc_got treeA=$tree_a treeB=$tree_b ctrl=$tree_c same_across_paths=$same ctrl_sensitive=$ctrl verdict=$verdict fails=$fails"
echo "PROBE wrote $OUT"
