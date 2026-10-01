// 跨机器比对：把各平台的探针结果放在一起判一次（CI 的 `compare` job 调它）。
//
// 用法：
//
//	node scripts/compare-probe-results.mjs <results-dir>
//
// `<results-dir>` 下递归找 `probe-result.json`（CI 的 artifact 会各自落在一个子目录里）。
//
// ## 为什么要有这个脚本，而不是"让读结果的人对照三行 annotation"
//
// 手工对照在**结果相同**时看不出问题，在**结果不同**时也很难说清是哪一层不同。
// 更要紧的是：三个平台的 `treeA` 一样，只有在**引擎版本也一样**时才说明"跨机器可复现"——
// 版本不同的两份字节相同纯属巧合，而"读一眼就通过"恰恰会漏掉这一条。
//
// 判定分四层，**任何一层不成立都以非零退出**，并在消息里指名哪个平台：
//
//	1. 每个平台自己的结论成立（verdict=REPRODUCIBLE 且对照组敏感）
//	2. 没有任何构建步骤失败（fails 为空）
//	3. 各平台的引擎版本一致（否则第 4 条没有意义）
//	4. 各平台的 treeA 完全一致，且各自 treeA == treeB
//
// 第 1、2 条是"这台机器上的检查本身是好的"，第 3、4 条才是"跨机器"。

import fs from "node:fs";
import path from "node:path";

const root = process.argv[2];
if (!root) {
  console.error("usage: node compare-probe-results.mjs <results-dir>");
  process.exit(2);
}
if (!fs.existsSync(root)) {
  console.error(`results dir not found: ${root}`);
  process.exit(2);
}

/** 递归收集所有 probe-result.json */
function collect(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) collect(p, out);
    else if (e.name === "probe-result.json") out.push(p);
  }
  return out;
}

const files = collect(root);
if (files.length === 0) {
  console.error(`no probe-result.json under ${root} — nothing was measured`);
  process.exit(1);
}

const bad = [];
const rows = [];
for (const f of files.sort()) {
  let r;
  try {
    r = JSON.parse(fs.readFileSync(f, "utf8"));
  } catch (err) {
    bad.push(`${f}: not valid JSON (${err.message})`);
    continue;
  }
  const where = `${r.label || "?"} (${r.os || "?"}/${r.arch || "?"})`;

  if (r.verdict !== "REPRODUCIBLE") {
    bad.push(`${where}: verdict=${r.verdict} — 该平台自身的检查就没通过`);
  }
  if (r.ctrl_sensitive !== "sensitive") {
    bad.push(
      `${where}: ctrl_sensitive=${r.ctrl_sensitive} — 对照组不敏感，` +
        `说明"两次相同"可能是检查坏了，而不是可复现`
    );
  }
  if (r.same_across_paths !== "yes") {
    bad.push(`${where}: same_across_paths=${r.same_across_paths} — 路径无关性不成立`);
  }
  if ((r.fails || "").trim() !== "") {
    bad.push(`${where}: 有构建步骤失败 → ${r.fails}`);
  }
  rows.push({
    where,
    esbuild: r.esbuild,
    tsc: (r.tsc || "").replace(/^Version\s+/i, "").trim(),
    treeA: r.treeA,
    treeB: r.treeB,
    ctrl: r.ctrl,
  });
}

// 先把所有输出攒起来，最后**一次**写出：
// 混着写 stdout / stderr 时两者会被分别缓冲，失败时表格与结论会交错错位
// （本脚本第一版就是这样：错误行夹在表头与表体之间）。
const table = ["per-platform results:"];
for (const r of rows) {
  table.push(
    `  ${r.where}: treeA=${r.treeA} treeB=${r.treeB} ctrl=${r.ctrl} ` +
      `esbuild=${r.esbuild} tsc=${r.tsc}`
  );
}

if (rows.length < 2) {
  bad.push(
    `only ${rows.length} platform(s) reported — "跨机器"至少需要两份结果才有意义`
  );
}

// 第 3 层：引擎版本必须一致。跨版本比字节没有意义（探针头部也写了这条）。
for (const key of ["esbuild", "tsc"]) {
  const seen = new Map();
  for (const r of rows) {
    const v = r[key];
    if (!seen.has(v)) seen.set(v, []);
    seen.get(v).push(r.where);
  }
  if (seen.size > 1) {
    const detail = [...seen.entries()]
      .map(([v, whos]) => `${v || "(空)"} ← ${whos.join(", ")}`)
      .join(" | ");
    bad.push(
      `platforms disagree on ${key} version: ${detail} — ` +
        `字节比对只在引擎版本相同时成立`
    );
  }
}

// 第 4 层：产物哈希必须一致。
const trees = new Map();
for (const r of rows) {
  if (!trees.has(r.treeA)) trees.set(r.treeA, []);
  trees.get(r.treeA).push(r.where);
}
if (trees.size > 1) {
  const detail = [...trees.entries()]
    .map(([h, whos]) => `${h || "(空)"} ← ${whos.join(", ")}`)
    .join(" | ");
  bad.push(`treeA differs across machines: ${detail}`);
}

if (bad.length > 0) {
  const lines = [...table, "", "NOT REPRODUCIBLE ACROSS MACHINES:"];
  for (const b of bad) lines.push(`  - ${b}`);
  process.stderr.write(lines.join("\n") + "\n");
  process.exit(1);
}

const t = rows[0].treeA;
process.stdout.write(
  [
    ...table,
    "",
    `REPRODUCIBLE ACROSS MACHINES: ${rows.length} platform(s) agree on treeA=${t} ` +
      `(esbuild=${rows[0].esbuild}, tsc=${rows[0].tsc})`,
  ].join("\n") + "\n"
);
