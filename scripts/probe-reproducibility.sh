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
#	PROBE_OUT        机器可读结果写到这里（供跨平台比对 job 消费）；默认 <workdir>/probe-result.json
#	PROBE_STABILITY  设为轮数 N 时进入**稳定性模式**：在同一目录里重复构建 N 轮，
#	                 数每个形态有几个不同取值（新形态进闸门前的必做测量，见下文）。
#
# 它做三件事：
#
#  1. 在两个**内容相同、路径不同**的目录里各构建一次
#  2. 跑一个**对照**：改动一个源文件的一个字节后再构建，产物哈希**必须**变化
#  3. 把结果写成一行 `::notice::` **和**一份 JSON（跨机器比对靠后者）
#
# 覆盖的构建形态（v0.6 扩到 10 种 + 2 个观察项）：
#
#	esbuild 打包 + 压缩 / esbuild sourcemap / tsc 声明 / postcss
#	+ **loader 链**（`.png` → dataurl、`.txt` → text）与 **CSS 压缩链**（v0.5 补）
#	+ **esbuild 插件（JS API）+ metafile**（v0.5 补）
#	+ **代码分割**（多入口 + 动态导入 → 共享 chunk）、**资产指纹**（`[name]-[hash]`）、
#	  **三钩子插件链**（onResolve + onLoad + onEnd，且打开 splitting）（v0.6 补）
#
# v0.6 那三个形态是冲着上一版留下的"合成夹具"帽子去的：前 7 个形态共同假设了
# "**一个入口一个产物**"，而真实项目（Vite / Astro）必然打开代码分割与资产指纹，
# 插件链也不止一个钩子。**不稳定的新形态一律记为观察项**（`obs.`，打印但不判定）——
# 这条纪律来自 metafile 那一轮：原始 metafile 20 轮 20 个取值，它就不该进闸门。
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
    //   ⚠️ v0.6 复测：**这段数字没有再复现**（esbuild 0.28.2、空闲 Windows、38 入口、
    //   20 轮 → 原始 metafile 也是 1 个取值）。移出产物树的**决定**仍然正确，
    //   但理由应读作"**路径内容**随机器/目录变化"（macOS 的 /var 符号链接），
    //   而不是"字节抖动"。更正写在 ADR-013 第五批。
    //
    // 也就是说：把原始 metafile 写进产物目录再哈希，测的是 esbuild 内部（多线程）
    // 的完成顺序，不是路径泄漏。第一次跨平台比对就是这么红的（macOS 上
    // treeA ≠ treeB，而 x86_64 两个平台一致到同一台机器的数字）。
    //
    // 因此写的是**规范化投影**：只保留"有哪些输入输出、各自多长、谁 import 谁"，
    // 数组按路径排序。它稳定 ⇔ 路径稳定——这才是这个形态想回答的问题。
    //
    // 写到 `dist/` **外面**（项目根）：它是**诊断文件**，不是产物。
    // 实测（CI 第一次带形态定位的跑）：唯一跨机器不一致的就是它——
    // `plugin.appjs` 三平台字节一致，而 `plugin.metapaths` 在 macOS 上连同一台机器
    // 的两个目录都不同。根因是**路径字符串**：macOS 的 `/var` 是指向 `/private/var`
    // 的符号链接，而本插件把**绝对路径**交给 esbuild（`path.resolve`，
    // 真实插件如 Vite 也是这么做的），相对化因此在一侧成功、另一侧失败。
    // 把它留在产物树里会让"产物是否可复现"这个判断被一个诊断文件的路径写法带偏。
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
      path.join(projectDir, "meta.paths.json"),
      JSON.stringify(canon, null, 2) + "\n"
    );
  })
  .catch((e) => {
    console.error(String(e && e.message ? e.message : e));
    process.exit(1);
  });
EOF

  # 三钩子插件链（v0.6）：onResolve + onLoad + onEnd，并且**打开 splitting**。
  #
  # 与上面那个插件的区别不是"钩子更多"，而是这条链更接近真实项目：
  #   onResolve  把 `github:` 裸导入解析到 vendor（Vite 的 alias 插件干的事）
  #   onLoad     把 `.txt` 变成 JS 模块（Vite 的 `?raw` / 资源导入干的事）
  #   onEnd      在构建结束时按 metafile 写一份**规范化**清单
  # 加上 splitting + 动态导入，产物于是是"多文件 + 共享 chunk + 文件名带内容哈希"，
  # 而不是此前 7 个形态共同假设的"一个入口一个文件"。
  #
  # 纪律不变：**不写入任何与机器或时间有关的东西**。onEnd 写的是规范化投影
  # （路径、字节数、谁 import 谁，数组按路径排序），且写到 dist **外面**——它是诊断文件。
  cat > "$WORK/tools/plugin-chain.cjs" <<'EOF'
const { build } = require("esbuild");
const fs = require("fs");
const path = require("path");

const projectDir = process.argv[2];
if (!projectDir) {
  console.error("usage: node plugin-chain.cjs <project-dir>");
  process.exit(2);
}

const canon = (m) => ({
  inputs: Object.keys(m.inputs).sort().map((k) => ({
    path: k,
    bytes: m.inputs[k].bytes,
    imports: (m.inputs[k].imports || []).map((i) => i.path).sort(),
  })),
  outputs: Object.keys(m.outputs).sort().map((k) => ({
    path: k,
    bytes: m.outputs[k].bytes,
    inputs: Object.keys(m.outputs[k].inputs || {}).sort(),
  })),
});

build({
  // 路径基准显式给出，不靠进程 CWD（ngm 自己踩过两次这个坑）。
  absWorkingDir: projectDir,
  entryPoints: ["src/chain/entry.ts"],
  outdir: "dist/pluginchain",
  bundle: true,
  splitting: true,
  format: "esm",
  minify: true,
  metafile: true,
  target: "es2020",
  logLevel: "silent",
  plugins: [
    {
      name: "vendor-alias",
      setup(b) {
        b.onResolve({ filter: /^github:/ }, (args) => {
          const name = args.path.slice("github:".length);
          return {
            path: path.resolve(projectDir, "ngm.vendor/github.com/demo", name, "index.ts"),
          };
        });
      },
    },
    {
      name: "text-as-module",
      setup(b) {
        b.onLoad({ filter: /\.txt$/ }, async (args) => {
          const text = await fs.promises.readFile(args.path, "utf8");
          return { contents: "export default " + JSON.stringify(text), loader: "js" };
        });
      },
    },
    {
      name: "manifest",
      setup(b) {
        b.onEnd((result) => {
          if (!result.metafile) return;
          fs.writeFileSync(
            path.join(projectDir, "meta.chain.paths.json"),
            JSON.stringify(canon(result.metafile), null, 2) + "\n"
          );
        });
      },
    },
  ],
})
  .then((r) => {
    // **原始 metafile 原样落盘**：它是这份探针里唯一"已知不稳定"的东西，
    // 因此被当作**稳定性测量的对照**——若它显示出 1 个取值，说明那台仪器测不出不稳，
    // 而"其他形态都稳定"就什么也没证明。
    //
    // 它同时是那条注释（"原始 metafile 20 轮 20 个取值"）的**可复现证据**：
    // 在此之前，那句话只存在于文字里。写到项目**根**（不在 dist 里），
    // 且只以 `obs.` 前缀参与报告——诊断文件不参与门禁。
    fs.writeFileSync(
      path.join(projectDir, "raw.meta.json"),
      JSON.stringify(r.metafile) + "\n"
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
    # 插件 + metafile（JS API）：Vite / Astro 走的就是这条路。
    # **门禁只看产物**（app.js）——metafile 是诊断文件，不是产物。
    run_form esbuild-plugin dist/plugin/app.js node "$WORK/tools/plugin-build.cjs" "$dir"
    # 代码分割（v0.6）：两个入口 + 一个动态导入 → 必然产出**共享 chunk**。
    # chunk 的文件名里带**内容哈希**，因此这一条同时覆盖"分割"与"名字里的指纹是否由内容决定"。
    run_form esbuild-split dist/split $ESB --bundle src/split/a.ts src/split/b.ts \
      --splitting --format=esm --minify --outdir=dist/split
    # 资产指纹（v0.6）：loader 产出的文件按 `[name]-[hash]` 命名——Vite 的默认行为。
    # 这里测的是"名字里的哈希只由内容决定"，与目录名无关。
    run_form esbuild-assetnames dist/assetnames $ESB --bundle src/with-assets.ts \
      --loader:.png=file --loader:.txt=text --asset-names=assets/[name]-[hash] \
      --minify --outdir=dist/assetnames
    # 三钩子插件链 + splitting（v0.6）：Vite 那条路上最常见的组合。
    run_form esbuild-pluginchain dist/pluginchain node "$WORK/tools/plugin-chain.cjs" "$dir"
    printf 'obs.pluginchain.metapaths=%s;' "$(hash_tree meta.chain.paths.json)" >> "$forms"
    # 原始 metafile：**已知不稳定**，是稳定性测量的对照（见 PROBE_STABILITY）。
    printf 'obs.pluginchain.rawmeta=%s;' "$(hash_tree raw.meta.json)" >> "$forms"
    # metafile 的路径内容另记一笔，前缀 `obs.` 表示**观察项、不参与门禁**：
    # 它仍是"路径是否泄漏"的证据（第一次跨机器比对就是靠它定位到根因的），
    # 但"诊断文件的路径字符串随平台变化"不等于"产物不可复现"。
    printf 'obs.plugin.metapaths=%s;' "$(hash_tree meta.paths.json)" >> "$forms"
  )
}

# 生成项目：入口 + 两级嵌套目录里的模块 + 两个依赖（镜像 vendor 布局）+ 两个资源
make_project() {
  local p="$1"
  mkdir -p "$p/src/util/deep" "$p/src/assets" "$p/src/split" \
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

  # 代码分割的夹具（v0.6）：两个入口共享一个模块，其中一个还**动态导入**第三个。
  # 这是真实项目的常态（路由级懒加载），而它必然产出共享 chunk——
  # 也就是说**产物不再是一对一的"一个入口一个文件"**，那正是此前 7 个形态都没覆盖的形态。
  printf 'export const shared = 1;\n' > "$p/src/split/shared.ts"
  # 插件链形态的入口：它 import 一个 `.txt`（由 onLoad 钩子接住），并动态导入懒加载模块
  # （于是 splitting 必然产出共享 chunk）。单独一个入口文件，是因为 `.txt` 的导入只有
  # **那条插件链**能处理——CLI 形态的 split 夹具因此保持干净。
  mkdir -p "$p/src/chain"
  printf 'export const lazy = 2;\n'   > "$p/src/split/lazy.ts"
  {
    printf 'import { shared } from "./shared";\n'
    printf 'export const a = shared;\n'
    printf 'export const load = () => import("./lazy");\n'
  } > "$p/src/split/a.ts"
  {
    printf 'import { shared } from "./shared";\n'
    printf 'export const b = shared + 1;\n'
  } > "$p/src/split/b.ts"
  {
    printf 'import { shared } from "../split/shared";\n'
    printf 'import note from "../assets/note.txt";\n'
    printf 'export const out = shared + note.length;\n'
    printf 'export const load = () => import("../split/lazy");\n'
  } > "$p/src/chain/entry.ts"

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

# ---------------------------------------------------------------- 稳定性模式（v0.6）
#
#	PROBE_STABILITY=20 bash scripts/probe-reproducibility.sh local
#
# 为什么需要它：**"路径无关"不等于"逐轮稳定"**。两个目录各构建一次得到的相同字节，
# 也可能只是"这台机器上恰好每次都一样"。v0.5 的 metafile 事件就是这么发生的——
# 本机两次运行同值、看起来通过，CI 一跑就红（原始 metafile 20 轮 20 个取值）。
# 因此**新形态进闸门之前**，先在同一目录里重复构建 N 轮、数"不同取值"的个数。
#
# 它自带**对照**：`stab-control` 是一个**每轮都改一个字节**的源文件的产物
# （`src/stability.ts` 里的那个数字），因此它必须在 N 轮里给出 **N 个**取值。
# 若它给出 1 个，说明这台仪器分辨不出字节差异——那么"其他形态都稳定"什么也没证明，
# 脚本会因此以非零退出（这是空跑保护，不是报告项）。
#
# 为什么对照不是"原始 metafile"（v0.5 曾这样记过）：v0.6 复测时它**不复现**——
# esbuild 0.28.2、空闲 Windows、**38 个入口**、20 轮，原始 metafile 仍是 1 个取值
# （连产物也是 1 个）。也就是说"原始 metafile 的字节不稳定"那条**不成立或只在特定负载下成立**；
# 把它当对照会让这台仪器在这台机器上永远报"瞎"（一个假警报）。
# 真正站得住的理由仍是**路径内容**：macOS 的 `/var` 符号链接 + 插件传绝对路径，
# 那一半在 CI 上可复现（见 ADR-013 第四批）。`obs.pluginchain.rawmeta` 因此保留为
# **观察项**（它显示路径内容是否会随目录/平台变化），但不再充当对照。
#
# 稳定性模式**替换**正常的双目录构建：两组测量各自独立，混在一起只会让输出更难读。
if [ "${PROBE_STABILITY:-0}" -gt 0 ]; then
  rounds="$PROBE_STABILITY"
  project="$WORK/work/a"
  for i in $(seq 1 "$rounds"); do
    # 每轮前清掉产物与诊断文件：esbuild **不会**清理 outdir，而残留的旧 chunk
    # （文件名里带内容哈希）会让"这一轮的产物"变成"历次产物的并集"——
    # 那样测出来的是"残留随时间累积"，不是确定性。
    rm -rf "$project/dist" "$project/meta.paths.json" \
           "$project/meta.chain.paths.json" "$project/raw.meta.json"
    if ! build_all "$project" "s$i" >/dev/null; then
      echo "stability round $i failed to build" >&2
      exit 1
    fi

    # 对照形态：源文件每轮都不同，产物因此**必须**每轮都不同。
    printf 'export const round = %s;\n' "$i" > "$project/src/stability.ts"
    if ! ( cd "$project" && $ESB --bundle src/stability.ts --minify \
             --outfile=dist/stab-control/app.js ) >"$WORK/stab-control.log" 2>&1; then
      head -c 300 "$WORK/stab-control.log" | tr "\n" " "
      echo "stability control failed to build" >&2
      exit 1
    fi
    printf 'stab-control=%s;' "$(hash_tree "$project/dist/stab-control/app.js")" \
      >> "$WORK/work/s$i.forms"
  done

  node -e '
    const fs = require("fs"), path = require("path");
    const dir = process.argv[1], n = Number(process.argv[2]);
    const per = new Map();
    for (let i = 1; i <= n; i++) {
      const f = path.join(dir, `s${i}.forms`);
      if (!fs.existsSync(f)) { console.error(`missing round ${i}: ${f}`); process.exit(2); }
      for (const part of fs.readFileSync(f, "utf8").split(";")) {
        const j = part.indexOf("=");
        if (j <= 0) continue;
        const k = part.slice(0, j).trim(), v = part.slice(j + 1).trim();
        if (!per.has(k)) per.set(k, new Set());
        per.get(k).add(v);
      }
    }
    const CONTROL = "stab-control";
    const lines = [], unstableGated = [];
    for (const k of [...per.keys()].sort()) {
      const d = per.get(k).size, obs = k.startsWith("obs.");
      lines.push(`  ${k.padEnd(30)} distinct=${String(d).padStart(3)}  ${d === 1 ? "STABLE" : "UNSTABLE"}${obs ? "  [obs]" : ""}`);
      if (!obs && d > 1 && k !== CONTROL) unstableGated.push(k);
    }
    console.log(`stability over ${n} rounds, same directory:`);
    console.log(lines.join("\n"));
    if (!per.has(CONTROL)) {
      console.error(`control ${CONTROL} missing — nothing proves this instrument can see a change`);
      process.exit(3);
    }
    // 对照的判据是**精确**的：源文件每轮都不同，就该每轮都不同。
    if (per.get(CONTROL).size !== n) {
      console.error(`BLIND INSTRUMENT: the control (${CONTROL}) changed every round, ` +
        `yet only ${per.get(CONTROL).size} of ${n} rounds produced a distinct hash. ` +
        `An instrument that cannot see a change makes every "STABLE" above meaningless.`);
      process.exit(1);
    }
    if (unstableGated.length > 0) {
      console.error(`UNSTABLE GATED FORM(S): ${unstableGated.join(", ")} — ` +
        `demote them to an "obs." item (printed, never gated), as metafile was in v0.5`);
      process.exit(1);
    }
    console.log(`all gated forms stable over ${n} rounds; control unstable ` +
      `(the instrument can see instability)`);
  ' "$WORK/work" "$rounds"
  exit $?
fi

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
