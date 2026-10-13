#!/usr/bin/env bash
# scripts/check-docs-links.sh —— 检查 Markdown 里的相对链接指向的文件是否存在。
#
# 规则：
#   1. 扫描给到的根：目录递归取 `**/*.md`，文件直接取（默认 `docs README.md`）
#   2. 匹配形如 [label](path) 的内联链接：path 若是 .md/.txt 相对路径，必须存在。
#      path 里的括号按**平衡括号**解析（v0.92）：`](./b(c).md)` 的结尾是最后一个配平的
#      `)`。旧的"截到第一个 `)`"会把它切成 `c`——既不报也看不见（一处已知漏检）。
#      括号不配平时退回旧语义（取最后一个 `(` 之后），不把畸形写法顺手变哑。
#   3. 跳过：绝对 URL、纯锚点（#xxx）、跨仓链接（http/https）、**行内代码里的示例**
#   4. CI 友好：打印每个坏链接（文件:行号）并在最后 exit 1
#
# 用法：scripts/check-docs-links.sh [root ...]
#   默认 `docs README.md`——**根 README 也要查**：它是最多人读的一份文档，
#   而它此前不在任何检查范围内（v0.10 B 组补上）。
#
# 注意：本检查只看"目标文件是否存在"，**不校验锚点**（`#xxx` 部分被剥掉）。
# 锚点由 `cmd/ngm/docs_anchors_test.go` 单独检查（中文标题的 slug 规则放在 Go 里，
# 因为 `grep -P` 的 Unicode 行为依赖 locale）。
set -euo pipefail

if [ "$#" -gt 0 ]; then
  roots=( "$@" )
  strict=1
else
  roots=( docs README.md )
  strict=0
fi

shopt -s globstar nullglob

bad=0
files=()
for r in "${roots[@]}"; do
  if [ -d "$r" ]; then
    files+=( "$r"/**/*.md )
  elif [ -f "$r" ]; then
    files+=( "$r" )
  elif [ "$strict" -eq 1 ]; then
    # **显式点名的根不存在 = 调用方写错了**，绝不是"没有东西要查"。
    # 静默跳过正是本项目反复吃亏的那种"看起来通过"（v0.8 复盘 §5.8）。
    echo "BROKEN ROOT (does not exist): $r"
    bad=$((bad + 1))
  else
    # 默认根（CI 里 docs/ 一定在）：缺失时说明并跳过，保持旧行为。
    echo "no $r; skipping"
  fi
done

# link_scan：**一个 awk 进程**做完"剥反引号 + 找链接 + 提取 target + 规范化路径"，
# 输出 `<待检路径>\t<文件>\t<行号>\t<原文匹配>`，交给 bash 只做 `[ -f ]`（纯内建，不起子进程）。
#
# 为什么（v0.91）：v0.90 把每条链接一次 `cd … && pwd` 换成纯 bash 的 normalize_path，
# 实测 133.72s → 33.07s；但 `$(normalize_path …)` **仍然每条链接 fork 一次**——
# docs/ 有 248 个文件、上千条链接 ⇒ 剩下的 33 秒基本就是这上千次 fork。
# 把规范化搬进 awk 之后，整条流水线只剩 1 个进程。
#
# 语义逐条保住（等价性用同一份夹具与旧实现对照过；负例网在 cmd/ngm/v090…）：
#   · 只剥**成对**反引号里的内容（等价于 `sed -E 's/`[^`]*`//g'`）；
#   · target 取"最后一个 '(' 之后、第一个 ')' 之前"——**连它的 bug 一起兼容**
#     （`b(c).md` 会被截成 `c`；这是旧实现的既有行为，改它要单独论证）；
#   · 跳过空 / `#` / http(s) / mailto / tel / file；去掉 `#锚点`；只查 `.md` / `.txt`；
#   · 不做符号链接解析（旧实现用 `cd`+`pwd` 会解；本检查只问"文件在不在"）。
link_scan() {
  awk '
    function normalize(p,   out, n, i, parts, seg) {
      n = split(p, parts, "/")
      out = ""
      for (i = 1; i <= n; i++) {
        seg = parts[i]
        if (seg == "" || seg == ".") continue
        if (seg == "..") {
          # 退一级；已经到根就保持根（**不能**只靠 sub：out 里没有 `/` 时它不匹配，
          # 那个 `..` 会变成空操作——v0.91 实测就是这样把 `../../docs/x` 解析错的）
          if (out ~ /\//) sub(/\/[^\/]*$/, "", out)
          else out = ""
          continue
        }
        out = (out == "" ? seg : out "/" seg)
      }
      if (substr(p, 1, 1) == "/") return "/" out
      return out
    }
    FILENAME ~ /(^|\/)(node_modules|vendor)\// { next }
    {
      line = $0
      gsub(/`[^`]*`/, "", line)
      pos = 1
      while (pos <= length(line) && match(substr(line, pos), /\[[^]]+\]\(/)) {
        start = pos + RSTART - 1
        open = start + RLENGTH - 1
        # destination 用**平衡括号**解析（v0.92）：`](./b(c).md)` 的结尾是最后一个
        # 配平的 `)`，而不是第一个 `)`。旧实现（左边到第一个 `)` 为止）会把它切碎成
        # `c` ⇒ 既看不见、也报不出来——一处**已知漏检**，v0.91 写进判据注释留待论证。
        depth = 0
        endpos = 0
        for (i = open + 1; i <= length(line); i++) {
          c = substr(line, i, 1)
          if (c == "(") depth++
          else if (c == ")") {
            if (depth == 0) { endpos = i; break }
            depth--
          }
        }
        if (endpos == 0) {
          # 括号不配平 ⇒ 按 CommonMark 这不是链接。**但仍按旧语义看一眼**：
          # 那类畸形写法以前会被报出来，把检查变强不该顺手把它变哑。
          # （best-effort：取这一行里最后一个 `(` 之后、第一个 `)` 之前。）
          mo = substr(line, start)
          idx = 0
          for (i = 1; i <= length(mo); i++) if (substr(mo, i, 1) == "(") idx = i
          t = substr(mo, idx + 1)
          sub(/\).*$/, "", t)
          m = mo
          pos = length(line) + 1
        } else {
          t = substr(line, open + 1, endpos - open - 1)
          m = substr(line, start, endpos - start + 1)
          pos = endpos + 1
        }
        if (t == "" || substr(t, 1, 1) == "#") continue
        if (t ~ /^(http|https|mailto|tel|file):/) continue
        sub(/#.*$/, "", t)
        if (t !~ /\.(md|txt)$/) continue
        base = FILENAME
        if (base ~ /\//) sub(/\/[^\/]*$/, "", base)
        else base = ""
        # 拼接时 base 为空**不能**产生前导 `/`：那会让相对链接被当成绝对路径
        # （v0.91 等价性对照抓到的第二处——根目录的 README.md 正好命中）。
        joined = (base == "" ? t : base "/" t)
        print normalize(joined) "\t" FILENAME "\t" FNR "\t" m
      }
    }
  ' "$@"
}

if [ "${#files[@]}" -eq 0 ]; then
  echo "no files to scan"
  exit 0
fi

while IFS=$'\t' read -r resolved f lineno match; do
  [ -n "$resolved" ] || continue
  if [ ! -f "$resolved" ]; then
    echo "BROKEN LINK in $f: $lineno:$match"
    bad=$((bad + 1))
  fi
done < <(link_scan "${files[@]}")

if [ "$bad" -gt 0 ]; then
  echo "found $bad broken links"
  exit 1
fi
echo "all docs links OK"