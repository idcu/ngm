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
# 它做两件事：
#
#  1. 在两个**内容相同、路径不同**的目录里各构建一次（60 个源文件，两级嵌套目录）
#  2. 跑一个**对照**：改动一个源文件的一个字节后再构建，产物哈希**必须**变化
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

rm -rf "$WORK"
mkdir -p "$WORK"

# 版本钉住，并在结果行里打印出来
ESB="npx --yes esbuild@0.28.2"
TSC="npx --yes -p typescript@5.6.3 tsc"
POSTCSS="npx --yes -p postcss-cli@11.0.0 postcss"

A1="--alias:github:demo/one=./ngm.vendor/github.com/demo/one"
A2="--alias:github:demo/two=./ngm.vendor/github.com/demo/two"

# 目录的聚合哈希：按路径排序后逐个喂进去，路径统一用 `/`，使结果与平台无关。
hash_tree() {
  node -e '
    const fs = require("fs");
    const path = require("path");
    const c = require("crypto");
    const root = process.argv[1];
    const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).flatMap((e) =>
      e.isDirectory() ? walk(path.join(d, e.name)) : [path.join(d, e.name)]);
    const h = c.createHash("sha256");
    for (const f of walk(root).sort()) {
      h.update(path.relative(root, f).split(path.sep).join("/"));
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

build_all() {
  local dir="$1" tag="$2" err=""
  (
    cd "$dir" || exit 1
    err=$(run_one "$tag-esbuild-min" $ESB --bundle src/index.ts "$A1" "$A2" \
      --minify --outfile=dist/app.js) || { echo "$err"; exit 1; }
    err=$(run_one "$tag-esbuild-map" $ESB --bundle src/index.ts "$A1" "$A2" \
      --sourcemap --outfile=dist/sm.js) || { echo "$err"; exit 1; }
    # 注意：tsc 这一步用**无别名**的入口。ngm 的依赖命名（`github:demo/one`）
    # 只有 esbuild 经 `--alias:` 能解析，裸 tsc 会报 TS2307——那是夹具的形态，
    # 不是被测引擎的缺陷。
    err=$(run_one "$tag-tsc-decl" $TSC --emitDeclarationOnly --declaration \
      --outDir dist/types src/plain.ts) || { echo "$err"; exit 1; }
    err=$(run_one "$tag-postcss" $POSTCSS --output dist/app.css < src/app.css) \
      || { echo "$err"; exit 1; }
  )
}

# 生成项目：入口 + 两级嵌套目录里的模块 + 两个依赖（镜像 vendor 布局）
make_project() {
  local p="$1"
  mkdir -p "$p/src/util/deep" \
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

  printf '.card { color: red; }\n.card .title { font-weight: 700; }\n' \
      > "$p/src/app.css"
}

for d in a bbbb-longer-directory-name; do
  make_project "$WORK/work/$d"
done

# 逐个构建：工作目录 = 项目目录，参数相对路径（与 ngm 的调用方式一致）
fails=""
for d in a bbbb-longer-directory-name; do
  out=$(build_all "$WORK/work/$d" "$d")
  if [ -n "$out" ]; then
    fails="$fails $d:[$out]"
  fi
done

tree_a=$(hash_tree "$WORK/work/a/dist")
tree_b=$(hash_tree "$WORK/work/bbbb-longer-directory-name/dist")

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

echo "::notice::PROBE $LABEL esbuild=$($ESB --version 2>/dev/null) tsc=$($TSC --version 2>/dev/null) treeA=$tree_a treeB=$tree_b ctrl=$tree_c same_across_paths=$same ctrl_sensitive=$ctrl verdict=$verdict fails=$fails"
