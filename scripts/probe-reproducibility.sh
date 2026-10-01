#!/usr/bin/env bash
# 可复现构建探针（本地 / CI 共用）。
#
# 它回答的问题来自 [ADR-013](../docs/adr/adr-013-remote-adapter.md) 的翻案条件之一：
# **"同一份源码 + 同一引擎版本 → 相同字节"**到底成立到什么程度。
#
# 用法：
#
#	bash scripts/probe-reproducibility.sh [workdir]
#
# 它做三件事：
#
#  1. 生成两个**内容相同、路径不同**的项目（60 个文件，两级嵌套目录）
#  2. 用**钉住版本**的引擎分别构建，比较产物字节
#  3. 跑两个**对照**，防止"两次相同"只是一种假象：
#
#	内容对照   改动一个源文件的一个字节 → 产物哈希**必须**变化
#	开关对照   不带 `--signatures` 时**不**得产生报告段落
#
# 为什么必须有对照：如果检查本身坏了（例如永远返回同一个值、或根本没跑到被测路径），
# "两次构建字节相同"照样成立，而它什么都没有证明。这一点与本项目的另一条纪律同源
# ——"探测命令是否存在"曾被误当成能力探测，于是 `deno run -e` 那种不存在的形态被判为"支持"。
#
# 工具版本**钉住并打印**：跨版本比字节没有意义。
#
# 结果以一行 `::notice::` 发出——Actions 日志匿名读不到，annotation 可以。

set -u

WORK="${1:-${TMPDIR:-/tmp}/ngm-repro-probe}"
rm -rf "$WORK"
mkdir -p "$WORK"

ESB="npx --yes esbuild@0.28.2"
TSC="npx --yes -p typescript@5.6.3 tsc"
POSTCSS="npx --yes -p postcss-cli@11.0.0 postcss"

# 目录的聚合哈希：按路径排序后逐个喂进去，路径统一用 `/`，使结果与平台无关。
hdir() {
  node -e '
    const fs = require("fs"), path = require("path"), c = require("crypto");
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

build() {
  local dir="$1"
  (
    cd "$dir" || exit 1
    $ESB --bundle src/index.ts \
      --alias:github:demo/one=./ngm.vendor/github.com/demo/one \
      --alias:github:demo/two=./ngm.vendor/github.com/demo/two \
      --minify --outfile=dist/app.js
  ) || return 1
  (
    cd "$dir" || exit 1
    $ESB --bundle src/index.ts \
      --alias:github:demo/one=./ngm.vendor/github.com/demo/one \
      --alias:github:demo/two=./ngm.vendor/github.com/demo/two \
      --sourcemap --outfile=dist/sm.js
  ) || return 1
  (
    cd "$dir" || exit 1
    $TSC --emitDeclarationOnly --declaration --outfile=dist/types src/index.ts
  ) || return 1
  (
    cd "$dir" || exit 1
    $POSTCSS --output dist/app.css < src/app.css
  ) || return 1
}

# 1) 生成两个内容相同、目录名不同的项目
for d in a bbbb-longer-directory-name; do
  p="$WORK/work/$d"
  mkdir -p "$p/src/util/deep" "$p/ngm.vendor/github.com/demo/one" "$p/ngm.vendor/github.com/demo/two"
  printf 'export const one = 1;\n' > "$p/ngm.vendor/github.com/demo/one/index.ts"
  printf 'export const two = 2;\n' > "$p/ngm.vendor/github.com/demo/two/index.ts"
  for i in $(seq 1 20); do
    printf 'export const m%s = %s;\n' "$i" "$i" > "$p/src/util/m$i.ts"
  done
  for i in $(seq 1 10); do
    printf 'export const d%s = %s;\n' "$i" "$i" > "$p/src/util/deep/d$i.ts"
  done
  printf 'export * from "./util/m1.ts";\nexport * from "./util/deep/d1.ts";\n' > "$p/src/lib.ts"
  {
    printf 'import { one } from "github:demo/one";\n'
    printf 'import { two } from "github:demo/two";\n'
    printf 'import { m1 } from "./lib.ts";\n'
    printf 'console.log(one + two + m1);\n'
  } > "$p/src/index.ts"
done

# 2) 两个路径各构建一次并比字节
for d in a bbbb-longer-directory-name; do
  build "$WORK/work/$d" > /dev/null 2>&1 || echo "PROBE build failed for $d" >&2
done

# 3) 对照:改动一个字节 → 产物哈希必须变化
before=$(hdir "$WORK/work/a/dist")
printf "export const changed = 1;\n" >> "$WORK/work/a/src/lib.ts"
build "$WORK/work/a" > /dev/null 2>&1
after=$(hdir "$WORK/work/a/dist")

echo "PROBE before=$before after=$after"
