# ADR-054：adopt 的保守迁移边界与脚本审批"只预演"

- **状态**：已定（2026-11-24）
- **范围**：`ngm adopt` 的多锁文件读取；`ngm doctor` 的脚本审批报告
- **依据**：[竞品全维度差距评估 2026-11-24 §6 P0 后两项](../analysis/ngm-competitive-gap-full-2026-11-24.md)
- **与 ADR-040 / ADR-009 的关系**：继承 ADR-040 的"adopt 不做安装、不判安全、
  不假装覆盖完整"，并把其中"只读 Git 来源"从 npm 扩到四种 lock；脚本侧沿用
  ADR-009 的 `postInstallPolicy` 默认 deny，**不新增任何执行面**。

## 结论

1. `adopt` 的边界是**保守迁移**：只提取"声明 + 锚点"能可靠映射到
   `node_modules/<name>` 与 `git+<url>#<commit|ref>` 的条目。
   无法证明来源的条目逐条进入 `proof.skipped[]`（`lockfile` / `key` / `reason`），
   **绝不编造 Git 来源**，也绝不静默吞掉。
2. `ngm doctor` 新增一节脚本审批**预演**：列出会被默认策略拒绝的声明，
   并给出可粘贴的 `supplyChain.postInstallPolicy` 放行/拒绝片段。
   报告**只预演不执行**：不 spawn、不写文件、不改变退出码语义（doctor 恒 0）。

## 决策

### 1. 多锁读取的形状

按优先级读取**第一份存在且可识别**的 lock：
`package-lock.json` / `npm-shrinkwrap.json`（npm v2/v3 的 `packages`）、
`pnpm-lock.yaml`、`yarn.lock`、`bun.lock`（**文本**，不是老的二进制 `bun.lockb`）、
`deno.lock`（**JSON v4/v5**）。沿用 ADR-003 的"不引入依赖"：pnpm/yarn/bun
只做按行窄解析，deno 只用标准库 JSON；不引入 YAML 解析器，也不起安装器子进程。

- 只有显式声明的 Git scheme（`git+` / `git:` / `git://`）**并且**带 commit/ref
  锚点的条目才进入 `provenance`。40 位 hex 是 `commit`，其它 fragment 是 `ref`
  （不可追溯，不能写成 commit）——与 ADR-040 的口径一字不改。
- 名字必须来自 lock 的声明（pnpm `name` / yarn descriptor key / bun key /
  deno specifier 的 URL 末段），并映射到 `node_modules/<name>`；取不出、多锚点
  争同一名字、spec 与 key 不一致、跨行数组、未知 lock 版本，一律 skipped。
- registry / integrity / remote 条目**不是 ngm 的轴**（ngm 证明的是
  `content <-> commit`），保持 `provenance=unknown` 并逐条 skipped 说明原因；
  npm 的 registry 条目仍按 ADR-040 记 `KindRegistry` + 原样 integrity。
- `skipped[]` 按 `lockfile/key/reason` 固定排序，`notes` 给出总数；
  `coverage` 的三个读数与 `skipped[]` 是互补的两面，不互相替代。
- **不读**：`bun.lockb`（二进制，发现时 notes 明说）、deno.lock v3 及更早、
  npm lock v1（没有 `packages` 段）、pnpm/yarn 的 registry integrity、
  多行/复杂 YAML 形状。这些边界在 V213 判据里固定。

### 2. 脚本审批报告的形状与边界

扫描两类**明确声明**，不解释、不推断：

- `ngm.vendor/` 中的 `postinstall.js`（ngm 唯一可沙箱执行的钩子）与
  `package.json` 的 `scripts.postinstall` / `scripts.install`（shell 钩子，
  ngm 从不执行，因为派生 shell 不受 Deno 权限约束，ADR-012）；
- `package-lock.json` v2/v3 的 `hasInstallScript: true` 标记。

pnpm/yarn/bun/deno lock 不携带脚本声明，`ngm.lock` 也没有脚本字段，
因此不被扫描；这不是"支持缺失"，而是没有可提取的声明。
报告字段：`status ∈ {none, pending, declared}`、有效 `policy`、按
`(source, dependency, kind, detail)` 排序并去重的 `entries`、`allow` / `deny`
两个可粘贴片段。除 `allow` 外（`deny` / `prompt`）均按"会被拒绝"呈现；
`prompt` 在非交互工具里等于不执行（ADR-009 决策 5 的修订）。

`allow` 片段是**全局策略** `"supplyChain": {"postInstallPolicy": "allow"}`，
不是逐包白名单：当前配置模型没有 per-dependency 审批字段，**不发明配置键**。
即便放行，也只在 Deno 沙箱里执行 `postinstall.js`，没有写权限（ADR-009）。
doctor 不执行任何脚本、不改 work tree、不把 pending 升级为 fail。

## 接线

- `ngm doctor` 已在本 ADR 所属文件集内接线（`cmd/ngm/doctor.go`）：
  `checks[]` 增加 `scripts`（warn/ok），`--json` 增加顶层 `scripts` 段，
  人读输出增加 `script approval preview` 一节。**无需改 root.go**。
- 可独立调用的函数签名：
  `scanScriptApprovals(projectDir, vendorRoot, policy string) scriptApprovalReport`；
  人读渲染：`writeScriptApprovals(w io.Writer, r *scriptApprovalReport)`。
  `policy` 由 `doctorPostInstallPolicy(*config.ProjectFile) string` 取声明值，空即 `deny`。
- 未在本次改动的文档：`README.md`、`docs/guides/cli.md`、`CHANGELOG.md`
  里 adopt 的"仅 npm lock"与 doctor 的 CHECKS 列表需要由接线方同步。

## 判据

- `TestV213AdoptReadsPnpmYarnBunAndDenoLocks` / `TestV213UnsupportedDenoVersionIsSkipped`
  / `TestV213BinaryBunLockIsNotRead`：四格式最小样例、畸形条目 skipped、确定性、版本与二进制边界。
- `TestV214DoctorListsDeclaredInstallScripts` / `TestV214DoctorSaysNoApprovalNeeded`
  / `TestV214DoctorScriptReportIsDeterministic`：假 vendor 条目被列出、
  无脚本明确"无需审批"、排序确定、脚本未被执行。

## 相关文档

- [ADR-040 · adopt 证明层](./adr-040-adopt-proof-layer.md)
- [ADR-009 · 供应链策略](./adr-009-supply-chain-policy.md)
- [ADR-003 · 纯 vendor 目录 / 不引入安装器依赖](./adr-003-vendor.md)
- [ADR-012 · 沙箱](./adr-012-sandbox.md)
