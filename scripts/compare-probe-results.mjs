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

/**
 * 把 `name=hash;` 解析成对象（探针的逐形态哈希就是这种紧凑格式）。
 */
function parseForms(s) {
  const out = {};
  for (const part of String(s || "").split(";")) {
    const i = part.indexOf("=");
    if (i > 0) out[part.slice(0, i).trim()] = part.slice(i + 1).trim();
  }
  return out;
}

/**
 * 观测项（`obs.` 前缀）**打印但不参与门禁**。
 *
 * 存在的理由：探针里有些哈希测的是**诊断文件**（如插件形态的 metafile），
 * 而"诊断文件里的路径字符串随平台变化"不等于"产物不可复现"。
 * 第一次跨机器比对正是靠这类观察项定位到根因的，所以它们必须继续被打印；
 * 但它们不该让判定变红——否则门禁会退化成"哪天诊断文件写法变了就红"。
 */
const isObs = (k) => k.startsWith("obs.");

/** 返回 a 与 b 中**取值不同**的形态名（含只出现在一边的），按名字排序。 */
function differingForms(a, b, includeObs = false) {
  const keys = new Set([...Object.keys(a), ...Object.keys(b)]);
  return [...keys]
    .filter((k) => (includeObs || !isObs(k)) && a[k] !== b[k])
    .sort();
}

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
  // 逐形态哈希：让失败**自己说出是哪个形态**。
  // 第一次跨平台红的时候（macOS 上 treeA ≠ treeB），我们只有整棵树的哈希，
  // 定位靠的是事后推理——那一步本该由结果自己完成。
  const formsA = parseForms(r.formsA);
  const formsB = parseForms(r.formsB);
  const sameMachine = differingForms(formsA, formsB);
  if (sameMachine.length > 0) {
    bad.push(
      `${where}: 同一台机器、两个目录的产物不同 → 形态: ${sameMachine.join(", ")}`
    );
  }

  rows.push({
    where,
    esbuild: r.esbuild,
    tsc: (r.tsc || "").replace(/^Version\s+/i, "").trim(),
    treeA: r.treeA,
    treeB: r.treeB,
    ctrl: r.ctrl,
    formsA,
    formsB,
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
//
// node 也在这一层：插件形态走的是 esbuild 的 **JS API**，脚本由 node 执行。
// 它在结果里一直印着（v0.5 第一次跑就显示 macOS 是 v22.23.2、Linux 是 v22.23.3），
// 但当时**没有任何一层看它**——印出来不等于被检查。
for (const key of ["esbuild", "tsc", "node"]) {
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

  // 形态级定位：相对第一个平台，逐个平台列出**哪些形态不同**。
  // 有它才知道该去查 loader 链、还是 tsc、还是插件。
  const base = rows[0];
  for (const r of rows.slice(1)) {
    const diff = differingForms(base.formsA, r.formsA);
    table.push(
      `  ${base.where} vs ${r.where}: ` +
        (diff.length ? `形态不同 → ${diff.join(", ")}` : "逐形态哈希**完全一致**（差异不在单个形态里）")
    );
  }
}

// 观察项的跨平台差异：打印，但不判定。它是"诊断文件里有没有机器相关字符串"的证据。
{
  const base = rows[0];
  for (const r of rows.slice(1)) {
    const obs = differingForms(base.formsA, r.formsA, true).filter(isObs);
    if (obs.length > 0) {
      table.push(
        `  [观察/不判定] ${base.where} vs ${r.where}: ${obs.join(", ")} —— ` +
          `诊断文件（非产物）里的路径内容不同；产物见上面的逐形态哈希`
      );
    }
  }
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
