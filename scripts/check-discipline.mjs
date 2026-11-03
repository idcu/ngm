/**
 * check-discipline.mjs —— 方法论自检看板（零依赖）
 *
 * 对应 `engineering-discipline.md` §8 的十问。它**不替代**任何判据，只回答一件事：
 * **这个仓库现在处于哪一档，缺的是哪一条**。
 *
 * 为什么需要它：纪律不均衡是常态——每个仓库都有几条做得很好、几条从来没做。
 * 单看"CI 绿了"看不出缺口；单看"文档写了"更看不出。十问逐条给 ✅/⚠️/❌ 与读数，
 * 缺口就变成一张可执行的清单。
 *
 * 用法：node scripts/check-discipline.mjs [--strict]
 *   （默认）只报告，退 0；
 *   --strict  低于 L3 退 1（挂 CI 用，防止"新仓库悄悄从 L0 起步"）。
 */
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const STRICT = process.argv.includes('--strict');
const read = (p) => { try { return fs.readFileSync(path.join(ROOT, p), 'utf8'); } catch { return ''; } };
const exists = (p) => fs.existsSync(path.join(ROOT, p));
const list = (dir) => { try { return fs.readdirSync(path.join(ROOT, dir)); } catch { return []; } };
const walk = (dir, out = []) => {
  for (const e of list(dir).length ? fs.readdirSync(path.join(ROOT, dir), { withFileTypes: true }) : []) {
    const rel = path.posix.join(dir, e.name);
    if (e.isDirectory()) {
      if (['node_modules', '.git', 'ngm.vendor', 'dist', '.tmp'].includes(e.name)) continue;
      walk(rel, out);
    } else out.push(rel);
  }
  return out;
};

const pkg = (() => { try { return JSON.parse(read('package.json')); } catch { return null; } })();
const scripts = (pkg && pkg.scripts) || {};
const workflows = list('.github/workflows').filter((f) => /\.ya?ml$/.test(f)).map((f) => read('.github/workflows/' + f)).join('\n');

/* 不适用声明：有些问对某些项目**结构上就不成立**（包仓库没有构建产物、纯静态站点没有依赖锁）。
   与其让它长期挂红、训练人忽略红色，不如把"不适用 + 理由"写下来，由看板标 ⚠️ 并排出计分。
   理由必须写清"为什么结构上不成立"，不写或写得太短（<10 字）仍按缺口算。 */
const exemptions = (() => {
  try {
    const raw = JSON.parse(read('docs/discipline-exemptions.json'));
    const out = {};
    for (const [k, v] of Object.entries(raw.exemptions || {})) {
      if (typeof v === 'string' && v.trim().length >= 10) out[k] = v.trim();
    }
    return out;
  } catch { return {}; }
})();

const checks = [];
const add = (id, ask, ok, note) => {
  if (exemptions[id]) {
    checks.push({ id, ask, state: 'na', note: '不适用：' + exemptions[id] });
    return;
  }
  checks.push({ id, ask, state: ok === true ? 'ok' : ok === 'na' ? 'na' : 'gap', note });
};

/* 1. 有一条会变红的判据 */
const checkScripts = walk('scripts').filter((f) => /(^|\/)check-[^/]+\.(mjs|js|sh)$/.test(f));
const goTests = walk('cmd').filter((f) => f.endsWith('_test.go')).length;
const gateCount = checkScripts.length + goTests;
add(1, '有一条会变红的判据', gateCount > 0,
  checkScripts.length + ' 个 check-*' + (goTests ? ` + ${goTests} 个 Go 判据文件` : ''));

/* 2. 判据在 CI 上跑 */
const ciRunsGates = /(node scripts\/check-|node scripts\/verify|npm run (ci|verify|test)|go test|go vet|ngm-validate)/.test(workflows);
add(2, '判据在 CI 上跑', workflows ? ciRunsGates : 'na',
  workflows ? (ciRunsGates ? 'workflow 里调用了判据/总闸' : 'workflow 存在但没有调用判据') : '没有 .github/workflows');

/* 3. 本地与 CI 是同一条命令 */
// 判法：CI 里跑的命令，必须能在 package.json 的命令值或本地文档里找到同一句。
// 只比"有没有 verify 脚本"不够——那只能证明本地有闸门，证明不了 CI 跑的是同一条。
const localText = (read('README.md') + read('docs/DEVELOPMENT.md') + read('docs/development/README.md'));
const pkgCmds = Object.values(scripts).flatMap((s) => String(s).split('&&').map((x) => x.trim()));
const ciCmds = [...workflows.matchAll(/run:\s*([^\n|]+)/g)].map((m) => m[1].trim()).filter((c) => c.length > 3);
const matched = ciCmds.filter((c) => {
  const head = c.split(/\s+/).slice(0, 2).join(' ');
  return pkgCmds.some((p) => p.includes(head)) || localText.includes(head);
});
add(3, '本地与 CI 同一条命令', ciCmds.length === 0 ? 'na' : matched.length > 0,
  ciCmds.length ? `${ciCmds.length} 条 CI 命令中 ${matched.length} 条能在本地命令/文档里找到` : 'CI 里没有可识别的命令');

/* 4. 契约文件 */
const contracts = list('contracts').filter((f) => f.endsWith('.json'));
const contractRefs = checkScripts.concat(walk('scripts')).filter((f) => /contracts\//.test(read(f))).length;
add(4, '最贵的约定写成了契约文件', contracts.length > 0 && contractRefs > 0,
  contracts.length + ' 份契约，' + contractRefs + ' 个脚本读它');

/* 5. 生成物策略 */
const gi = read('.gitignore');
const buildEntry = Object.keys(scripts).some((k) => /build/.test(k))
  || walk('scripts').some((f) => /(^|\/)build[-.]/.test(f));
add(5, '生成物有明确归宿（重建或判据）', gi.length > 0 && buildEntry,
  buildEntry ? '.gitignore 与构建入口都在' : '找不到构建入口（或 .gitignore 为空）');

/* 6. 依赖按字节锁定 */
const depLock = exists('ngm.lock') || exists('package-lock.json') || exists('go.sum');
// 校验脚本按"内容里提到 lock/vendor"识别，而不是按文件名里有没有 ngm——
// 后者会让 ngm 自己的仓库判"没有校验脚本"（它的脚本叫 check-vendored-standard.sh）。
const depVerify = checkScripts.some((f) => /(ngm\.lock|ngm\.vendor|vendor)/.test(read(f)));
add(6, '依赖按字节锁定并可复验', depLock && depVerify,
  (depLock ? '有锁定文件' : '没有锁定文件') + (depVerify ? ' + vendor/lock 校验脚本' : '，但没有校验脚本'));

/* 7. 文档分层 */
const docs = walk('docs').filter((f) => f.endsWith('.md'));
add(7, '文档有入口/权威分层', exists('README.md') && docs.length >= 3,
  'README + ' + docs.length + ' 篇 docs/');

/* 8. 破坏性变更 / 删除有记录 */
const hasCompat = exists('contracts/compat-plan.json');
const hasDeletions = exists('docs/deletions.md') || exists('docs/development/deletions.md');
add(8, '破坏性变更与删除有记录', hasCompat || hasDeletions,
  [hasCompat ? 'compat-plan' : '', hasDeletions ? 'deletions' : ''].filter(Boolean).join(' + ') || '都没有');

/* 9. 删除记录本身被判据守 */
// Go 项目把负例写在 `cmd/**/*_test.go` 里，所以两种载体都算。
const goDelGuard = walk('cmd').filter((f) => f.endsWith('_test.go')).some((f) => /[Dd]eletion/.test(read(f)));
const delGuarded = checkScripts.some((f) => /deletions/.test(f)) || goDelGuard;
add(9, '删除记录本身被判据守（能抓假删除）', delGuarded,
  delGuarded ? (goDelGuard ? 'Go 判据在册' : 'check-deletions 在册') : '记录有了但没人查');

/* 10. 判据有牙齿（可构造的负例） */
const selftest = walk('scripts')
  .filter((f) => /\.(mjs|js)$/.test(f))
  .filter((f) => read(f).includes('--selftest'));
add(10, '判据自己带牙齿（可构造负例）', selftest.length > 0 || goTests > 0,
  selftest.length ? selftest.length + ' 个脚本支持 --selftest' : (goTests ? '负例在 Go 测试里' : '没有可运行的负例'));

/* 11. 文档规模：单文件不超过阈值（超过就会重新长回去，拆分只做一次不算数） */
const LIMIT_KB = 60;
const LIMIT_LINES = 600;
// 快照不参与规模判据：复盘、评估、竞品对照这类文件**按纪律不重写**，
// 它们的体积是历史事实，不是"文档没治理好"。把它们算进来，只会逼人删历史。
const SNAPSHOT = [
  /(^|\/)v0\.\d+-(plan|retrospective)\.md$/,
  /(^|\/)EVALUATION-/,
  /(^|\/)COMPETITORS-\d{4}-/,
  /(^|\/)assessment-/
];
const mdFiles = [...walk('docs').filter((f) => f.endsWith('.md')), 'README.md']
  .filter((f) => exists(f) && !SNAPSHOT.some((re) => re.test(f)))
  .map((f) => {
    const text = read(f);
    return { f, kb: Buffer.byteLength(text) / 1024, lines: text.split('\n').length };
  })
  .sort((a, b) => b.kb - a.kb);
const oversized = mdFiles.filter((m) => m.kb > LIMIT_KB || m.lines > LIMIT_LINES);
add(11, `文档单文件不无限增长（≤${LIMIT_KB}KB 且 ≤${LIMIT_LINES} 行）`, oversized.length === 0,
  oversized.length
    ? '超限 ' + oversized.length + ' 个，最大 ' + oversized[0].f + ' ' + oversized[0].kb.toFixed(1) + 'KB/' + oversized[0].lines + 'L'
    : '最大 ' + (mdFiles[0] ? mdFiles[0].f + ' ' + mdFiles[0].kb.toFixed(1) + 'KB/' + mdFiles[0].lines + 'L' : '（无 md）'));

/* 12. 每条判据都在总闸里（写完却从不跑的判据，价值是 0，代价是有） */
// 判法：脚本文件名必须出现在「命令链」或「另一个脚本的内容」里（后者覆盖 check.mjs 这类
// 通过 spawn 调子判据的聚合器）。只看命令链会误报一整批被聚合器调用的子判据。
// 判法：文件名必须出现在**别的文件**里。第一版把所有脚本内容拼成一段再查，
// 结果每条判据都被自己的头部注释命中（`/* check-contracts.mjs —— …`）——恒绿的判据。
// 排除"自己"之后，check.mjs 这类聚合器对子判据的 spawn 调用才算数。
const scriptBodies = walk('scripts').filter((f) => /\.(mjs|js|sh)$/.test(f));
const orphanChecks = checkScripts.filter((f) => {
  const base = path.basename(f);
  const others = [
    ...Object.values(scripts).map(String),
    workflows,
    ...scriptBodies.filter((g) => g !== f).map((g) => read(g))
  ].join('\n');
  return !others.includes(base);
});

// 需要外部环境（真实数据库 / 网络 / 特定硬件）的判据进不了总闸，这没问题——
// 但它们必须**登记为手动项**，否则与"忘了挂"分不开。登记过的标 ⚠️ 并计入说明。
const manual = (() => {
  try {
    const raw = JSON.parse(read('docs/manual-checks.json'));
    return raw.checks || {};
  } catch { return {}; }
})();
const manualOrphans = orphanChecks.filter((f) => manual[path.basename(f)]);
const forgot = orphanChecks.filter((f) => !manual[path.basename(f)]);
if (orphanChecks.length === 0) {
  add(12, '每条判据都在总闸里（没有"写了但从不跑"的）', true, checkScripts.length + ' 个 check-* 都在总闸里');
} else if (forgot.length === 0 && manualOrphans.length <= Math.max(2, Math.ceil(checkScripts.length / 3))) {
  add(12, '每条判据都在总闸里（没有"写了但从不跑"的）', 'na',
    manualOrphans.length + ' 个已登记为手动项（' + Object.keys(manual).join('、') + '），其余都在总闸里');
} else if (forgot.length === 0) {
  // 手动项本身也要有上限：全都登记成手动，第 12 问就成了摆设。
  add(12, '每条判据都在总闸里（没有"写了但从不跑"的）', false,
    '手动项过多（' + manualOrphans.length + '/' + checkScripts.length + '）——超过三分之一时，' +
    '「需要外部环境」更像是在给不做 CI 找理由');
} else {
  add(12, '每条判据都在总闸里（没有"写了但从不跑"的）', false,
    '孤立 ' + forgot.length + ' 个：' + forgot.slice(0, 3).map((f) => path.basename(f)).join('、'));
}

/* 13. 文档里的命令都指向真实存在的东西 */
// 只扫代码块：散文里提到 `scripts/xxx` 是叙述，代码块里的才是"照着敲就会跑"。
// 查两件事：`scripts/*.{mjs,js,sh}` 的路径存在；`npm run <script>` 在 package.json 里有。
const cmdDocs = ['README.md', ...walk('docs').filter((f) => f.endsWith('.md'))].filter((f) => exists(f));
const missingCmds = [];
let cmdCount = 0;
for (const f of cmdDocs) {
  const text = read(f);
  for (const block of text.matchAll(/```[a-zA-Z]*\n([\s\S]*?)```/g)) {
    for (const line of block[1].split('\n')) {
      for (const m of line.matchAll(/(?:^|\s)(scripts\/[\w./-]+\.(?:mjs|js|sh))/g)) {
        cmdCount++;
        if (!exists(m[1])) missingCmds.push(`${f}: ${m[1]}`);
      }
      for (const m of line.matchAll(/npm\s+run\s+([\w:-]+)/g)) {
        // 没有 package.json 的项目（Go 仓库、纯脚本仓库），文档里的 `npm run` 是
        // **别的项目**的示例（方法论、迁移指南），不该按本仓库解析。
        if (!pkg) continue;
        cmdCount++;
        if (!(m[1] in scripts)) missingCmds.push(`${f}: npm run ${m[1]}`);
      }
    }
  }
}
// package.json 里的 script 也常常直接指向脚本文件（`"build": "node scripts/build.mjs"`）。
// 光查"名字在不在 scripts 里"不够：改名后 `npm run build` 仍能匹配上，跑起来才报 ENOENT。
for (const [name, cmd] of Object.entries(scripts)) {
  for (const m of String(cmd).matchAll(/(scripts\/[\w./-]+\.(?:mjs|js|sh))/g)) {
    cmdCount++;
    if (!exists(m[1])) missingCmds.push(`package.json scripts.${name}: ${m[1]}`);
  }
}

add(13, '文档里的命令都指向真实存在的脚本', missingCmds.length === 0,
  missingCmds.length
    ? '断 ' + missingCmds.length + ' 条（如 ' + missingCmds[0] + '）'
    : cmdCount + ' 条命令全部可解析');

/* ---------------- 成熟度 ---------------- */
const okCount = checks.filter((c) => c.state === 'ok').length;
const applicable = checks.filter((c) => c.state !== 'na').length;
const ratio = applicable ? okCount / applicable : 0;
// 按"具备比例"算档，而不是按绝对条数：声明了不适用的项目不该被它无解的那几问拖住。
const level = ratio === 1 && okCount >= 6 ? 'L4'
  : ratio >= 0.8 ? 'L3'
    : ratio >= 0.6 ? 'L2'
      : okCount >= 2 ? 'L1' : 'L0';

console.log(`discipline: ${okCount}/${applicable} 项具备 → ${level}`);
for (const c of checks) {
  const mark = c.state === 'ok' ? '✅' : c.state === 'na' ? '⚠️ ' : '❌';
  console.log(`  ${mark} ${String(c.id).padStart(2)}. ${c.ask} —— ${c.note}`);
}
const gaps = checks.filter((c) => c.state === 'gap').map((c) => c.id);
if (gaps.length) console.log(`  缺口：第 ${gaps.join('、')} 问`);

if (STRICT && !/^L[34]$/.test(level)) {
  console.error(`\n--strict：成熟度 ${level} 低于 L3（缺口在第 ${gaps.join('、')} 问）`);
  process.exitCode = 1;
}
