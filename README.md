# ngm

> Git-first Dependency Provenance Layer

ngm is a Node.js / Deno package manager with **provable** Git dependency tracking. Every dependency is locked to a specific commit and content-addressed by a content digest — `archiveDigest` for a whole repository, `moduleDigest` for a sub-path module — and verifiable on demand via `ngm verify`.

Status: **v0.1 … v0.101 delivered in source**, and all one hundred and one tags `v0.1.0` … `v0.101.0` exist.
The current development line (unreleased as a tag) adds **one-repository module collections**,
lock v2 (`moduleDigest + subtreeOid`), per-module content store/vendor, partial-clone module cache,
collection indexes, and the `internal/core` extraction:
[module collections](./docs/architecture/module-collections.md) ·
[ADR-032: module-scoped proof](./docs/adr/adr-032-scoped-content-proof.md) ·
[ADR-033: language-neutral core](./docs/adr/adr-033-language-neutral-core.md) ·
[core and adapters](./docs/architecture/core-and-adapters.md).

`v0.5` was the convergence and delivery pass; `v0.6` made the remaining conclusions *decidable*
(a git spawn budget as a CI gate, real-world reproducibility forms, measured content-store
growth); `v0.7` made the content store's footprint **visible** (`ngm store usage`) and its
residues reclaimable (`ngm store prune`); `v0.8` replaced the layer-2 layout with a
**blob pool + tree manifests**, cutting the measured cost of 12 commits from
**20.0× the real source delta to 0.6×** (`1.88 MiB → 59.9 KiB`); `v0.9` split that pool into
*shared / exclusive / orphaned* so the numbers say what they mean; `v0.10` pushed the checks to
the outermost ring (the root README's own links and anchors, and "the store is incomplete");
`v0.11` closed the four open decisions (ADR-020…023, three of them "do not do this") and did the
layer's **first real reclaim** — `ngm store prune --orphans`.
Per-version plans and retrospectives live in [`docs/development/`](./docs/development/README.md);
for a single-page assessment of **what works today, what is blocked and why** — plus how it compares
to pnpm / npm / Yarn / Bun / Deno — see
[`docs/internals/project-state.md`](./docs/internals/project-state.md).

**Release state, read from the APIs on 2026-10-10** (not from this file's memory —
the previous reading had drifted because it counted *releases* and called them *tags*;
this time both columns were measured, and the tag column then **explained** the release column):

| source | releases | tags | what the two columns say |
|--------|----------|------|--------------------------|
| GitHub | **60 of 101** | 65 | tags run `v0.1.0` … `v0.65.0` with no gaps; releases run through **`v0.64.0`**, the one above it (`v0.65.0`) ran the workflow and failed ✗ |
| Gitee | **3 of 101** | not counted | the three are `v0.2.0`, `v0.3.0`, `v0.4.0` — `v0.1.0` has no release there |

Read together, the GitHub columns hold exactly: 65 tags − 4 that never ran the workflow
(`v0.5.0` … `v0.8.0`, forwarded by the mirror) − 1 that ran and failed (`v0.65.0`) = **60 releases**.
The 36 tags **GitHub** does not have are `v0.66.0` … `v0.101.0`: they were never pushed there.
Two of them (`v0.100.0`, `v0.101.0`) are on Gitee, which is where this repository's branch and
tags are pushed first — so they can be cloned today, while GitHub sees them on its next sync.
The two hosts therefore disagree by two, on purpose and temporarily.

**Why the GitHub gap stays a gap**: the machine that maintains this repository cannot reach
`github.com:443` at all — `git ls-remote https://github.com/idcu/ngm` returns
`Connection timed out` twice in a row, while `api.github.com` answers and Gitee answers
(measured 2026-10-30). So the branch and tags go to Gitee first, where they are clonable
immediately, and the GitHub mirror is updated from a network that can reach it. That is a
property of the environment, not a decision — and it is the reason the two columns in this
table differ, rather than the release process being abandoned.

New versions are released by the tag push itself: that is how the releases through `v0.64.0` were
produced — with one miss: `v0.28.0` first pushed with a red CI, and its tag was re-pointed at the fix.

**Back-filling those ninety-eight historical artifacts is deferred by decision (2026-10-04)** — not
forgotten: every version in the gap has its capabilities in source (`v0.5`…`v0.57` compile with the
"from source" path in [`docs/guides/installation.md`](./docs/guides/installation.md)), and the newest
version is downloadable today. What it costs is a credential — `POST /actions/workflows/…/dispatches`
returns 401 without a token, and the Gitee uploader needs a `GITEE_TOKEN` — so resuming it is one
command per version. The decision, its rationale and the signals that would make it worth resuming
are recorded in
[`docs/development/README.md`](./docs/development/README.md#发布清单每个版本).

Until then, **`v0.4.0` is the newest version downloadable from both sources, and `v0.57.0` is the
newest downloadable from GitHub.** Both readings are mechanical, not remembered: the checklist that
keeps "delivered" and "released" from drifting apart is in `docs/development/README.md`, and the
Gitee side of it has a reading of its own (`scripts/check-gitee-release-status.sh`, with a Windows
entry point that delegates to that one implementation).

> v0.8 changes one user-visible behaviour: the `symlink` link mode degrades to per-entry hardlinks
> under the v2 layout (same disk savings, reported honestly); see
> [`docs/adr/adr-019-content-addressed-blobs.md`](./docs/adr/adr-019-content-addressed-blobs.md).

All four v0.1 exit criteria are met with executable evidence: `ngm install` works end to end,
`ngm.lock` is byte-identical across platforms, `ngm verify` tells a re-tagged tag apart from an
advanced branch, and a digest replay mismatch blocks the build. Since then: **v0.2** added OSV/audit,
the policy engine, `why`/`tree`/`outdated`, the tsc/deno/postcss adapters and the CI install modes;
**v0.3** added the wasm adapter, the Deno sandbox, enforced permissions and the integration
scaffolds; **v0.4** added `ngm typedecl`, `verify --signatures` / `--require-signed`, and the
mechanical "declared but not wired" config check.

See [`docs/development/`](./docs/development/) for the per-version plans and retrospectives —
including the performance targets that were **not** met at first (`verify` at 100 dependencies).

---

## Why ngm — and when you should not use it

**The strongest argument for ngm did not come from us.** On 2026-01-26 Koi Security disclosed
**PackageGate**: six zero-days across npm / pnpm / vlt / Bun. The npm one was exactly this —
**a malicious `.npmrc` inside a Git dependency can override the git binary path, achieving code
execution even with `--ignore-scripts`**. pnpm fixed it in 2 weeks, vlt in 8 days, Bun in 3.
**npm closed the report as "works as expected"** — *"users are responsible for reviewing the
packages they install"*.

npm's own threat model — *a Git dependency is an under-estimated attack surface* — is ngm's product
thesis. This exact path cannot exist here, and each row is checkable against the code:

| The attack needs | ngm provides |
|---|---|
| the dependency's `.npmrc` to be **interpreted as configuration** | ngm parses no npm configuration, anywhere. A `.npmrc` inside a dependency is just a file: it goes into the `archiveDigest`, it is vendored, it is never interpreted |
| a script inside the dependency to run on install | postinstall hooks are **denied by default**; when explicitly allowed they run only inside a Deno sandbox (no network, no run, writes never granted) |
| the dependency to change *which git* runs | `git` is launched from exactly one place, gated by the `run:git` permission; no config key can replace the binary |

Run [`scripts/packagegate-demo.sh`](./scripts/packagegate-demo.sh) to watch it yourself — the demo
asserts; it does not ask you to trust a table.

**When you should use ngm**: your dependencies are Git repositories — private forks, upstreams you
must trace to a commit, audit deliverables that need reproducible content — and you want the lock
file to be a *proof* rather than a *cache key*.
**When you should not**: you only consume packages from a registry. pnpm / npm / Yarn / Bun serve
that case better, and ngm deliberately does not manage registry packages. The full honest version
(both directions) is in [`docs/README.md`](./docs/README.md#诚实版劣势与边界).

---

## What works today

### M0 — 工程骨架与配置层

- `ngm --version` / `ngm --help`
- `ngm init <name> --runtime=node|deno` — generate a `ngm.json` template
- `ngm config validate` — schema-check the project config
- `ngm config show` — print the merged effective configuration (builtin → global → project)

### M1 — Git 层

- **URL canonicalization** — 4 protocols (`https` / `git@host:path` / `git://` / shorthand) plus `ssh://`, explicit host, and bare canonical forms all normalize to `host/org/repo`; `Normalize` is idempotent
- **Slug form** — `github:org/repo` for `ngm.json` / lock / CLI output, distinct from the canonical URL used for git operations
- **refType resolution** — `tag` / `branch` / `commit` → commit, with **annotated tag peeling** (`^{}`) so the lock records a commit, never a tag object. A full 40-char commit may omit `refType`; tag/branch still require it
- **mirror + module cache** — `~/.ngm/mirror/...` is the full object mirror; `~/.ngm/module-cache/...` is an optional `--filter=blob:none` partial clone for lock-pinned modules (ADR-034)
- **credential passthrough** — ngm never reads or stores tokens; the environment is passed to git as-is and all output is redacted (`https://user:token@host` → `https://***@host`)
- `ngm add <url>[@<ref>] [--ref-type <type>] [--infer-ref-type]` — writes `ngm.json`; inference is opt-in and only chooses when the remote has exactly one of tag/branch
- `ngm update [<dep>|--all] [--offline]` — re-resolves refs to commits

### M2 — content digests (ADR-008 / ADR-032)

- **Root manifest** — `ngm-archive-digest/v1`: header + one `path NUL mode NUL sha256 NUL` record per non-directory tree entry, sorted by **UTF-8 byte order**. `archiveDigest = "sha256:" + sha256(manifest)`
- **Module manifest** — `ngm-module-digest/v1`: the same record format with paths **relative to the module root**; location is bound separately by `subtreeOid = git rev-parse <commit>:<subPath>`
- **No newline conversion** — blobs are hashed as-is; a frozen vector contains a CRLF file to prove it
- **No Git host archive API** — the manifest is built from the local mirror/cache, so digest generation is fully offline
- **LFS pointers and gitlinks are rejected** (`exit 3`), never silently hashed; a sibling module's LFS/gitlink no longer poisons the module's own proof
- **content store** — root entries use `archiveDigest`, module entries use `moduleDigest`; the tree is unpacked and byte-verified, symlinks recreated, exec bits preserved
- `ngm update --digest` — print the content digest; `--store` also populates the content store

Frozen vectors live in [`testdata/vectors/`](./testdata/vectors/) and are asserted on all three CI platforms.

### M3 — 依赖图与确定性 lock

- **Transitive dependencies** — resolved breadth-first from each module's own `ngm.json` (`<subPath>/ngm.json` first; repository root only as a warned fallback). `package.json` is never a dependency source. 4-way concurrency within a layer, deterministic results
- **Module addressing** — CLI targets accept `repo#path=<subPath>` (or legacy `repo#<subPath>`); ambiguity is an error listing candidates, never a silent “all modules”
- **Conflict policy** — same `ref` merges; **root wins** (with the overridden transitive requirement reported); transitive-only conflicts fail with `exit 3` and a full provenance chain
- **Deterministic `ngm.lock`** — fixed field order, 2-space indent, LF, trailing newline; every field is byte-reproducible except `resolvedAt` (which lives inside each entry, never at the top level)
- **`ngm install`** — no lock → resolve and write it; lock present → **respect it** (refs are not re-resolved), and report staleness when `ngm.json` changed
- **`ngm update`** — re-resolve and rebuild the lock; `ngm remove` — edit the declaration only

The `install` vs `update` split is the heart of provable reproducibility:

```bash
$ ngm install          # upstream re-tags v1 ...
github:demo/b@v1 (tag) → 671c6133...   # ... install keeps the locked commit

$ ngm update --all     # update is the only way to move it
github:demo/b@v1 (tag) → d24e024b...
```

### M4 — vendor 4 层与 mappings

- **link tree (layer 3)** — real directory structure with per-file hardlinks to the content store; symlink entries are rebuilt as symlinks (never hardlinked). `ngm.vendor/` is a derived artifact, so each run cleans and rebuilds the target path
- **module-aware landing** — a sub-path module is materialized from its **module-scoped** content store entry (paths relative to the module root), not by filtering a whole-repo tree
- **`linkMode`** — `auto` (hardlink, falling back to a full-tree copy) / `hardlink` (no fallback) / `copy` / `symlink` (the dependency directory is linked as a whole). The **actually used** mode is reported, so a silent fallback cannot hide
- **monorepo sub-paths** — only the sub-directory is materialized (`ngm.vendor/…/packages/core`), matching `dependency-resolution.md`
- **cache layers** — mirror (full), module cache (partial clone), content store (root/module manifests), and `~/.ngm/cache/{metadata,osv,tmp}` (disposable); `ngm store usage` reports all three storage layers
- **cache (layer 4)** — `~/.ngm/cache/{metadata,osv,tmp}`, disposable by contract; `ngm cache clean` never touches mirror or content store
- **mappings** — `ngm.mappings.json` with three-tier entry inference (`ngm.json` → `package.json` exports → `./index.ts|js`); a missing entry is a warning, not a failure (the spec says the mapping is still generated)
- **`ngm mappings validate`** — checks `from` against the lock, `to` on disk, and `main`/`types` existence
- **`VerifyVendorTree`** — full file-by-file sha256 comparison of vendor against content (no sampling), reused by M5's `verify`

### M5 — verify

`ngm verify` answers one question: **is what is on disk still what the lock claims?**

- **check 1 — ref → commit.** Re-resolves the declared `refType` and compares it with the pinned commit.
- **check 2 — binding (module entries).** `git rev-parse <commit>:<subPath>` must equal the lock's `subtreeOid`; a wrong location is `critical`.
- **check 3 — digest replay.** Root entries replay `archiveDigest`; module entries replay `moduleDigest` from their own subtree. Both are fully local and work offline.
- **check 4 — landing integrity.** Content-store metadata plus vendor tree structure (paths, symlink targets, sizes). `--deep` replays the digest from the installed content tree and hashes every vendor file against it.

Drift is classified so CI can act on it without parsing prose:

| What happened | `driftKind` | Default exit |
|---------------|-------------|--------------|
| branch advanced (fast-forward) | `expected` | 0 (`--strict` → 1) |
| tag moved, branch rewritten, ref deleted | `unexpected` | 1 (`--allow-drift` → 0) |
| digest replay mismatch, tampered content/vendor | `critical` | 2 (never downgraded) |
| network failure, cold mirror under `--offline` | — | 4 |

```bash
$ ngm verify
✓ github:demo/a@v2.0.0 (tag) → fedcba9 — match
⚠ github:demo/b@main (branch) → def456a (now 789xyz0) — expected update
  → driftKind: expected; expected update, not blocking; run `ngm update github:demo/b`
✗ github:demo/c@v0.9.0 (tag) → 123abc4 — integrity failure
    landing: content store digest mismatch: ...
  → driftKind: critical; do NOT build from this tree; ...

verified 3 dependency(ies): 1 ok, 1 expected, 1 critical (exit 2)
```

`ngm verify --json` emits the same verdicts as a stable machine-readable report; `ngm verify <dep>...` scopes the check to specific dependencies.

### M6 — engine adapter（构建 / 类型 / CSS）

**ngm core does not implement engines** (ADR-005). Every capability goes through an adapter that spawns an external tool; ngm decides *what*, the engine decides *how*.

- **engine catalog** — `ngm.engines.json` (project) overrides `<ngm home>/ngm.engines.json` overrides the built-in catalog. One entry = one engine + one `kind` (`transform` / `bundle` / `typeCheck` / `typeDecl` / `css`); the same binary may appear under several kinds.
- **what the built-in catalog adapts** — `esbuild` (bundle + transform), `typescript` (typeCheck + typeDecl, `tsc --emitDeclarationOnly`, `optional`), `postcss` (css, `optional`) and the `self` stub; since v0.3 the `wasm` adapter runs engines inside a wasm runtime. `tsc` / `postcss` are marked `optional` so "not installed" is not a catalog problem. The `remote` adapter is **excluded by decision** (ADR-013) rather than "not implemented yet". Declare your own in `ngm.engines.json` — the subprocess driver is generic.
- **engine selection** — `--engine=<name>` → `ngm.json` `engines.<kind>` → global defaults → built-in default (esbuild for bundle/transform). Shorthand `"esbuild"` and the full form `{"primary": "esbuild", "fallbacks": []}` resolve to the *same* command, byte for byte.
- **fallback** — `fallbacks` are tried in order; every fallback is announced on stderr, so you always know which engine actually ran.
- **`ngm build`** — bundles through the engine, and turns every `ngm.mappings.json` entry into `--alias:<from>=<to>/<main>` so a bare `import x from "github:org/repo"` resolves into `ngm.vendor` — the bytes ngm proved. That is the one thing it adds over calling esbuild yourself.
- **`--dry-run`** — prints the resolved command (engine, argv, availability, detected version) and runs nothing. It is the fastest way to find out why a flag did not arrive.
- **`ngm engines list|info|validate`** — availability and probed version per entry, human table or `--json`; `validate` separates "your catalog is malformed" (exit 3) from "this engine is not installed" (exit 5).

```bash
$ ngm build src/index.ts --outfile=dist/app.js
bundled src/index.ts → dist/app.js

$ ngm build --dry-run
would bundle src/index.ts
✓ primary: esbuild (bundle, subprocess)
    status:  available (0.28.2)
    command: esbuild --bundle --format=esm --target=es2020 src/index.ts
    source:  built-in catalog
```

`audit` / `why` / `tree` / `outdated` were delivered in v0.2 and the integration scaffolds
(`ngm integrations add vite|esbuild|deno|webpack`) in v0.3 — see the roadmap below.

### Try it

```bash
go build -o ngm ./cmd/ngm

# 1. a project
./ngm init github.com:my-org/app --runtime=node --dir ./app
./ngm add github:my-org/utils@v1.2.3 --ref-type=tag --dir ./app
./ngm config validate              # run inside ./app

# 2. resolve + digest + content store (fully offline if the mirror is warm)
./ngm update --all --offline --digest --store --dir ./app
# github:my-org/utils@v1.2.3 (tag) → 1a2b3c4...
#   archiveDigest: sha256:9f86d081...
#   content store: ~/.ngm/content/sha256/9f86d081.../ (stored)
```

---

### M7 — module collections（当前开发线）

一个 Git 仓库可以承载一个公司/个人的一组公共模块；每个模块有自己的 `ngm.json`、依赖、版本和内容证明：

- **模块身份** — `repo + subPath`；消费声明写 `path`，CLI 目标写 `repo#path=<subPath>`
- **独立依赖** — 先读 `<subPath>/ngm.json`；缺失才回退仓库根清单并输出 warning
- **独立版本 / 发布** — 模块清单可写 `version` 与 `publish`；`publish` 显式 true/false **完全覆盖**全局 `--releases`，nil 才跟随默认
- **模块级证明** — lock v2 写 `moduleDigest + subtreeOid + version`，不写整仓 `archiveDigest`
- **模块级存储/落地** — content store 与 vendor 都按模块 scope 工作
- **集合索引** — `ngm modules list` / `validate` / `index`；索引可内联 releases，也可 `--shards` 写 `releases/<module>.json`；本地 `ngm-index` 分支承载根索引与分片树
- **部分克隆缓存** — `install-from-lock` 与 `verify --offline` 优先复用 `~/.ngm/module-cache` 的 `blob:none` partial clone；filter 不支持时明确回退完整 mirror
- **核心抽离** — `atomicfile` / `digest` / `errs` / `git` / `lock` / `resolve` / `vendor` / `verify` 已在 `internal/core/`
- **外部证明模块** — `gitee.com/idcu-go/modproof v0.2.0` 承载模块 proof；ngm 的 `verify` 已切换，内部 facade 已删除

详见 [模块集合架构](./docs/architecture/module-collections.md)、
[语言无关核心与适配器](./docs/architecture/core-and-adapters.md)、
[ADR-031](./docs/adr/adr-031-module-collections.md)…[ADR-037](./docs/adr/adr-037-per-module-publish-override.md)。

---

## Roadmap

The complete per-version plans live in [`docs/development/`](./docs/development/). At a glance:

| Milestone | Capability | State |
|-----------|------------|-------|
| M0 | CLI scaffold, error model, three-tier config, test infra, CI | done |
| M1 | URL canonicalization, refType resolution, mirror, credential passthrough, `add` / `update` | done |
| M2 | `archiveDigest` (ADR-008) — content manifest + frozen digest vectors + content store | done |
| M3 | Dependency graph, conflict detection, deterministic `ngm.lock`, `install` | done |
| M4 | Vendor 4-layer (mirror / content store / link tree / cache), mappings | done |
| M5 | `ngm verify` — three-level checks, drift classification, exit codes | done |
| M6 | esbuild adapter, `ngm build` / `transform` / `typecheck` / `css` / `engines` | done |
| M7 | End-to-end acceptance, cross-platform binaries, release pipeline | done |
| v0.2 | OSV/`audit`, policy engine (minimumReleaseAge / allowlist / postInstallPolicy), `why` / `tree` / `outdated`, tsc·deno·postcss adapters, `install --frozen-lockfile` / `--offline` | delivered · released |
| v0.3 | wasm adapter, Deno sandbox, enforced `permissions`, Vite / esbuild / Deno / Webpack scaffolds, mappings sub-paths | delivered · released |
| v0.4 | `ngm typedecl`, `verify --signatures` / `--require-signed`, mechanical "declared but not wired" config check | delivered · released |
| v0.5 | online-verify cost & variance (ADR-015/016), ADR-013 re-open conditions judged, permission enforcement points audited, **v0.2~v0.4 catch-up releases** | delivered · tag only |
| v0.6 | the spawn budget as a CI gate, cross-machine reproducibility evidence, measured store growth (20.0×) | delivered · tag only |
| v0.7 | `ngm store usage` (read-only) and `ngm store prune` (residues only) | delivered · tag only |
| v0.8 | layer 2 re-laid out as blob pool + tree manifests (ADR-019): 20.0× → 0.6× | delivered · tag only |
| v0.9 | the store's readings say what they mean (shared / exclusive / orphaned) + anchor checks + 100k-blob scale | delivered · released |
| v0.10 | checks pushed to the outermost ring: the root README itself, and "the store is incomplete" | delivered · released |
| v0.11 | ADR-020…023 (four decisions closed), **first real reclaim** (`store prune --orphans`), workflow YAML gate, three `WaitDelay` fixes | delivered · released |
| v0.12 | made the project's own readings honest: three-tier documentation corrections (9), **MIT license**, real `ngm <cmd> --help`, six "silent failure" defects, and the first net over command-line flags | delivered · released |
| v0.13 | turned defect *shapes* into nets (positional arguments must be bounded; `--offline` must spawn no git at all) — and recorded one net that was **deliberately not built**, because it would miss the case it targets | delivered · released |
| v0.14 | reconciled the documentation with measured behaviour: four corrections (one fact wrong in three separate places) + three nets (dry-run writes nothing; which engine kinds get a built-in default; every flag binding must be dereferenced) | delivered · released |
| v0.15 | pinned two external contracts: documented example commands must be real invocations (306 examples / 27 living docs), and usage errors must be distinguishable (exit 3, own usage on **stderr**, empty stdout, nothing leaking past the injected writer) | delivered · released |
| v0.16 | pinned the machine-readable surface: a `--json` contract over 20 states (exactly one JSON document; the flag never changes the exit code; `exitCode` inside a report equals the process code; input errors leave stdout empty) — and wrote the shapes down where users can read them | delivered · released |
| v0.17 | bounded the explanations: `why` enumerates at most 64 paths and `tree` expands at most 4096 entries by default — the count is exponential in graph width (41 nodes produced over a million paths / 626 MB) and the graph's shape comes from upstream manifests, so both say so when they stop, and `--all` lifts the bound (ADR-024) | delivered · released |
| v0.18 | the first run against a **real remote**: an opt-in network check (`NGM_REAL_UPSTREAM=1`, skipped by default) plus a recorded attempt — no product code changed, because the one hypothesis worth acting on ("network waits have no bound") was **disproved by measurement** (failures end at git's own limits: ~20s reset, ~21s connect timeout) | delivered · released |
| v0.19 | the text users see is an interface too: every command's usage now writes its own `EXIT CODES` (8 new + a correction to `install`), `config` learned `--dir` and stopped reporting OK where there is no manifest, and the internal stage labels (`M\d`) leaked into user-visible strings were removed | delivered · released |
| v0.20 | measure first: two pending candidates were both **disproved** — the "missing engine" error already lists its candidates, and all 21 commands already refuse consistently on configuration errors. No product code changed | delivered · released |
| v0.21 | made declarations answerable: every exit code each command's usage promises must either have an offline trigger that is actually run, or be recorded as a known gap — and the gap list trips itself when it goes stale. Found and fixed one product defect: `ngm tree --offline` did not thread `--offline` into graph resolution, exiting 3 instead of the 4 it promises | delivered · released |
| v0.22 | drained the gap list: 26 known gaps down to **3**, measured claims up from 44 to **67 of 70** — a fake engine covers five kinds succeeding and failing, and drift / tamper / cold-mirror / missing-Deno / local-OSV fixtures cover 1, 2, 4 and 5. The three remaining gaps each carry a measurement showing why they cannot be reached today | delivered · released |
| v0.23 | split the *name* of an error from its *number*: exit code 1 is one number with four meanings, so `ngm typecheck` no longer reports a failing engine as `RefDrift:`. Exit codes, `--json` and `errors.Is/As` are untouched; a stale line in the normative spec was corrected too | delivered · released |
| v0.24 | the exit-code contract has four sources of truth; a net now ties them together — implementation, the normative table, the spec's constant block (whose `iota` ordering would shift every code silently) and the user-facing summary. It caught a second stale line on its first run | delivered · released |
| v0.25 | the same question, asked about the other hand-written lists: the `--json` shape table (9 commands, 48 keys) and the three config field tables (19 keys) are now checked against the implementation. No product code changed; the expensive lesson was measuring the judgement before writing it (a naive version flagged 27 of 62 tokens) | delivered · released |
| v0.26 | the other direction: every top-level field of every report must be named in the docs (67 fields), and the forward net grew to 71 keys. Getting there exposed a prerequisite — two payloads were anonymous struct literals, and **an unnamed shape cannot be reconciled with anything**: both were given names | delivered · released |
| v0.27 | the loosest list of all — field names that live in *prose* rather than in a table — is now checked too, anchored to the lines that talk about `--json` (52 lines, 21 names; the anchor takes false positives from 27/62 to 0). The config doc also gained its reverse check | delivered · released |
| v0.28 | a census of "does this error have a next step" (300 construction sites, 129 with none) turned up a real, user-visible defect: the `hint:` line was **lost when errors were wrapped**. Fixed at the one place it can be lost — the renderer now walks the cause chain — which repairs all 300 sites without touching any error data, plus a ratchet so the count cannot grow | delivered · released |
| v0.29 | read the 11 ratcheted sites one by one and found every one of them *can* carry a real next step, so the ratchet became a hard gate (0/47); an end-to-end net now proves the advice actually reaches the user by making `ngm init` fail for real | delivered · released |
| v0.30 | runtime proof instead of source reading: reusing the 64 exit-code cases, every one of the 15 error-text failures carries a next step and none fails silently (the 28 report-style failures are out of scope but reported). The judgement guards its own reachability, and a repository-wide ratchet watches the half the user-layer gate cannot see | delivered · released |
| v0.31 | the error surface, ordered by *failure path* rather than by declaration: v0.30's 21 configuration-error cases were all the same scenario (an unknown flag). Reshaped into 14 real entry points — 12 error-text failures all carry a next step, 2 are report-style, 0 silent, and only 2 error identities exist. One judgement of mine was disproved by measurement: "verify exits 0 when the vendor tree is deleted" was a path that does not exist | delivered · released |
| v0.32 | reports must speak too: every failing item (`✗`) has to carry a line saying what to do next. The new net caught two real gaps on its first run — `audit` said nothing when an advisory records no fixed version, and `tree --osv` marked a vulnerability without naming it or pointing at `ngm audit` (while the *unchecked* branch already had that pointer) | delivered · released |
| v0.33 | advice has to name names: having a next-step line is not enough — that line must name something executable, and the named thing is cross-checked against the running command table and the config schema sources. A wrong pointer is worse than no pointer. The net's boundary is stated plainly: it can tell that advice points at something real, not that the something is the right thing to do | delivered · released |
| v0.34 | advice has to match the *shape* of the failure: drift points at `ngm update`, changed bytes at `ngm install`, vulnerabilities at `ngm audit` — not just any command that exists. Writing that judgement measured a real defect: the "check could not complete" branch of `verify` had its advice computed and then dropped by a render gate, the same shape as v0.28. The teeth show it plainly: point the critical advice at `ngm audit` and v0.33 passes while v0.34 fails | delivered · released |
| v0.35 | advice has to acknowledge the *cause*: one branch of `verify` covers a missing mirror and a policy-denied host, and v0.34 gave both the same sentence — which is wrong for the second. The right sentence was already written by the layer closest to the cause and was dropped twice: once by `err.Error()` keeping only the message, once by an assignment nothing ever read. Fixed by letting the layer that knows say it (`errs.Hint` + `ErrHint`), so a new cause becomes correct on its own | delivered · released |
| v0.36 | sweep instead of selection: the report contract used to cover nine cases I had hand-picked; now every one of the 57 non-zero cases is run and anything printing a failing item must carry a next step. It caught a blind spot on the first pass (`ngm engines validate`: seven issues, exit 5, no advice at all), and the judgement itself once produced a false positive by reading the usage text that *documents* the marker. One candidate was disproved outright: a lexical dead-assignment check finds nothing on this repository | delivered · released |
| v0.37 | enumerate instead of sweep: 21 commands × 3 configuration-error shapes = 63 runs, each required to honour its channel (error text carries a next step, reports carry a next step, usage text must not appear at all, silence is red, and an output fitting no known channel is red too). Under a second, no fixtures — and it caught two bugs of mine rather than the product's | delivered · released |
| v0.38 | more shapes (3 → 8, including a corrupt lock), and the net's one remaining hole closed: a run that exits 0 satisfies no contract at all, so every such cell must now be registered with the measured reason why zero is correct — checked in both directions, so a stale registration fails too | delivered · released |
| v0.39 | the argument dimension: every command run with no positional arguments and with one extra argument nobody understands. 42 runs, all non-zero, and the invariant that matters is that **an extra argument is never silently ignored** — v0.19 fixed one command for this, now it holds for all of them | delivered · released |
| v0.40 | how arguments are *spelled*: ordering, repetition, and `--`. All three rest on sentences the product already wrote about itself, and one of them was lying: the argument rewriter classified everything after `--` as a positional and then **dropped the `--` when re-emitting**, so those tokens landed back in flag position — nine commands affected. One existing unit test had encoded that bug as its expectation | delivered · released |
| v0.41 | `--dir` spellings and the empty value: four spellings must be byte-identical, and an explicitly empty `--dir=` must be refused. It was not — an empty string is indistinguishable from "flag not given", so the command silently fell back to the current directory: `add … --dir=` edited the *current* project's manifest, `init … --dir=` built a project in the current directory. The judgement looks only at side effects (the current directory must stay empty) | delivered · released |
| v0.42 | the empty value, generalised to every value-taking flag: 15 flag/command pairs, of which 14 refuse it outright and 1 treats it exactly like omitting the flag — none of them touches the working directory. The rule is written in the shape of the defect: **an empty value must either be refused or behave exactly like omission, never succeed differently** | delivered · released |
| v0.43 | boolean flags: the table of 41 command/flag pairs is derived from the source rather than recalled, and 38 of them are put through four assertions — `=true` equals the bare flag, `=false` equals omitting it, `=x` must be refused, and a boolean flag must never consume the token after it (the rewriter's own comment promises exactly that) | delivered · released |
| v0.44 | closing a nine-version-old "unproven": one branch of `verify` (a commit missing from the local mirror, while offline) had never been reached by any test, so the advice written inside it — the value of what was a dead assignment until v0.35 — had no evidence of ever reaching a user. This version builds that state with a commit that genuinely exists upstream, and the branch turns out to work, and the advice turns out to actually discriminate: run online once and the verdict changes from "not in the mirror" to "upstream discarded it" | delivered · released |
| v0.45 | driving the *published skips* to zero: of the three pairs v0.43 skipped, only two were genuinely subcommand-scoped; the third was my own derivation being too coarse (two commands live in one file). Fixing that uncovered a bigger hole — the `css` command has no `css.go`, so its two flags had never been reached by any net at all, hidden behind a *false* skip. A skip list reads as transparent, but it doubles as cover: "tested, just skipped" and "never tested" look identical on it | delivered · released |
| v0.46 | the same mistake, still in use by another net: the value-flag net kept a **hand-copied** table of 14 pairs while the source yields 37 — so 23 had never been looked at, including another `css` flag (that command has now leaked in two consecutive versions, same root cause). The table is now derived from source blocks (cross-file, carrying declaration kinds), and the coverage guard draws its set from the three nets' *own* selection functions instead of re-stating a rule of its own — including removing a "name has a space, so it must be covered" shortcut that repeated the very error the previous version had just fixed | delivered · released |
| v0.47 | the two-token spellings — the last piece of the argument dimension: `--flag ""` must equal `--flag=`, and the empty token after a boolean flag is a *positional* that must be treated exactly like `--flag ZZ-JUNK`. Both reds were mine: a judgement that picked the wrong reference ("same as the baseline" proves nothing when the baseline itself fails) and the fifth instance of a fixture leaking a freshly-created path into the output. And one judgement boundary got measured and written into the net itself: the value side is *insensitive* to the empty-token-filter mutation — green does not mean it is watching anything | delivered · released |
| v0.48 | the shape dimension, finished: wrong field type, illegal sub-path, unknown `schemaVersion` — all three already refused correctly (that is itself a reading), but they exposed misdirected advice: the cause for `--path=/etc` says the path must be relative, while the only advice shown talks about the *repository spec* shape (the inner validation returned a bare error, so the caller's fallback was all there was). Fixing that made a new assertion possible — advice must acknowledge *this* failure's shape — and quietly degraded another line (`cause:` repeating the error code), which no net caught: it took reading the output line by line. The renderer now drops a same-code prefix | delivered · released |
| v0.49 | closing the "the comment says more than the code does" family: v0.48 met it once, this version counted it — all 30 validation sites in `internal/config` created hint-less errors while the package comment promised actionable hints. Every site now carries a field-level hint (30 → 0 bare errors, 5 of them `%w` wrappers that pass the inner hint through), and a new `WrapUnlessHinted` makes the fallback appear only when the inner layer has nothing to say (three call sites unified). The static census reads the source and refuses any new bare error. Worth recording: the *previous* version's assertion went red, and rightly so — it had pinned the fallback's wording, and this version exists to retire that wording; and one fixture turned out not to build the state it named, exposed not by a human but by the wording inside the advice | delivered · released |
| v0.50 | the next-step contract has to hold across channels: twenty-two consecutive versions polished the human-readable output, while `--json` is what scripts read. Three real gaps (audit with no fixed version, `tree --osv`, `engines validate`) now carry a `remediation` field whose sentence is *the same sentence* the human side prints — one source per report. One apparent gap turned out to be deliberate (rule ④ of the v0.16 JSON contract: on input errors stdout must be empty), so it became a named registration instead. The most valuable reading of the version is a failure: the first judgement asked "does the JSON contain such a field", and the tooth did not bite — an item-level field was standing in for a report-level one. Rewritten as a content-level check ("the human sentence must appear verbatim in the JSON"), it bites | delivered · released |
| v0.51 | failure paths need a machine-readable half too: a script that ran `--json` and hit an error got "exit code + empty stdout + some prose" — it knew something failed, not what. An error envelope now carries the same code name, the same exit code, and the *same hint sentence* as the human side, emitted from the single error-rendering point. Rule ④ of the v0.16 JSON contract was rewritten with evidence: stdout may be empty *or* hold exactly one report/envelope — the original intent ("a report is never half-emitted") is written next to the new rule. And the version forced a second thing into shape: the v0.43 net went red because the envelope's path is JSON-escaped, making this the fourth recurrence of "a path this run created leaked into a compared output" — so the normalisation list became one shared helper, and four nets migrated to it | delivered · released |
| v0.52 | making the deliberate and the deferred *named and countable* — no product code changed; what changed is the ledger behind the judgements. A bare error in the source looks the same whether its author thought about it or forgot it, and the same holds for items hanging in a candidate list: "I know it is there" and "I missed it" are indistinguishable. So `duration.go`'s ten deliberate ones got a named allowance (the measured value — the first draft guessed 8), and everything hanging got a registry where each entry must carry a `cost` and a `since`, with the total under a ratchet | delivered · released |
| v0.53 | paying two of those debts down, and letting the ratchet move downward: a per-item count assertion on the machine side (the content-level check cannot notice "seven lines for humans, one field for scripts"), and *where does a net's coverage come from* turned from a slogan into a checked register — ten nets, one named exception, with the rule that the file must actually read source or a runtime table, or say why. That register caught two of my own errors on its first run, including a test-function name I had invented | delivered · released |
| v0.54 | failures that happen *before* a command learns about `--json` are machine-readable too: the envelope added in v0.51 lived in the single error-rendering point, which commands only reach after they have parsed their own flags — so an unknown flag or an unknown command still left stdout empty. The fix asks the raw arguments once ("did the user ask for this?") and never prints the human text twice. Two controls matter more than the new cases: without `--json` nothing may be emitted, and for a command that does not accept it stdout stays empty (rule ④ is unchanged) | delivered · released |
| v0.55 | flags written *before* the command are no longer swallowed: `ngm --json verify` used to go in and never come out — the token entered the root-level head and had no continuation, so the command ran normally and the caller got the human report with exit 0 and an empty stderr: a wrong channel, silently. That is the worst of the three shapes a failure can take (error / silent failure / silent success). The root whitelist is now a runtime table both the dispatch and the net read, everything else is refused — which also brings in the v0.54 envelope, so even "your flag is in the wrong place" is machine-readable | delivered · released |
| v0.56 | auditing the *earlier* contract nets too: the register only covered the ones since v0.41, leaving two dozen v0.2x–v0.3x nets never asked where their coverage set comes from. A checklist-style survey (rather than reading 20 files) said **12 → 32 registered nets** — and surfaced two cases where *the net itself cannot see that it has lost a member*: three pointers in a register that claim "this exit code is measured elsewhere", which nothing verified (the second appearance of "a register pointing at nothing" — last time a test function name, this time a subtest name), and a cause table that could only catch dangling keys, never a missing cause, now derived from the fixture table's naming | delivered · released |
| v0.57 | exit code 1's sources become auditable: three places disagreed about how many sources it has (a code comment said three, a hand-written keyword table said four, the source tree yields twenty-seven sites across twenty-one functions), and that table only checked one way — code gaining a source could not turn it red. The universe is now derived from *behaviour* rather than from a name, which matters because the panic handler writes a bare `exit = 1` and never mentions the code: scanning for the name could never see it. Five classes came out, and the substantive find is the last one — a dozen sites in the CLI return 1 because the command finished, reached a conclusion, and then could not *say* it (writing stdout, encoding JSON, reading stdin), while the docs described the code as "policy failure". Docs and the code comment now say what it is | delivered · released |
| v0.58 | the numbers we quote about releases now have a source: a hand-copied line had drifted by one version ("55 tags, 51 on GitHub") and nothing was watching the prose — the release-check script only asks whether tags and releases *exist*. What can be derived offline is derived (the delivered version count comes from the retrospective files, cross-checked against the plan files; the latest version from those filenames — deliberately not from `git tag`, since CI checks out shallow). What cannot be derived is required to be at least *self-consistent*: every total equals the derived one, the same fact is written the same everywhere, and the arithmetic holds (gap = versions − Gitee; success count = GitHub − 4 never-released). Its first run found a genuine stale claim — the roadmap still said "v0.1 ~ v0.11 delivered" | delivered · released |
| v0.59 | internal failures get their own exit code (6): the first time the exit-code contract itself has moved (0..5 → 0..6), so per this project's discipline it starts as an ADR. Sixteen sites that "reached a conclusion and could not deliver it" (writing stdout, encoding, reading stdin, a panic, plus the catch-all for errors that never entered the error model) used to share code 1 with policy failures — and the two want different things: drift needs a human, a failed write may just need a rerun. The four policy sources of code 1 do not move. Code 6 is measurable (hand the dispatcher a writer that always fails), so five commands are measured and six are named gaps with reasons | delivered · released |
| v0.60 | making the two channels agree about a failed write: the human path used to ignore it (`fmt.Fprintf`'s error was discarded), so "half the report went out" and "success" were indistinguishable by exit code — while the JSON path always checked. The evidence was inside the same codebase: `engines` did check its own flush, so the question was never which semantics to pick, but why one thing had two treatments. A single accounting writer at the one entry point turns a failed write after an otherwise-successful run into code 6, leaving business failures to win | delivered · released |
| v0.61 | a broken pipe is intent, not failure: v0.60 correctly made a failed write exit 6 — and thereby made `ngm verify \| head` a failure too. The two are now separated, starting from a measurement: on this machine a write to a pipe whose reader is closed yields errno 232 (ERROR_NO_DATA), while `errors.Is(err, syscall.EPIPE)` is false and the syscall package exports only ERROR_BROKEN_PIPE. The judgement is cross-platform (with a GOOS gate, since 109/232/233 mean something else in Unix errno space) and the exception lives in one place: a closed peer is not accounted as a failure, so the command keeps its own code. The third case — JSON plus a closed peer still exits 6 — is asserted on purpose: a deliberate asymmetry should be visible, not silent | delivered · released |
| v0.62 | paying down the gap list, and then finding out that one of its own conclusions was wrong: the fixtures for `audit` and `integrations` had been in the test suite all along, so both moved into the measured set; the other four were reported as failing at the permission layer "because the hook is dead". v0.63 overturned that — the hook was wired, and what the fixture lacked was one call to isolate the user environment (the commands were reading the real home, hence no authorisation). Registering an action is not the same as the action landing | delivered · released |
| v0.63 | registering an action is not the same as the action landing: this version overturns one of its predecessor's own conclusions (the "dead hook" was wired all along — the consumer sits in `testenv_test.go`'s `init()`), so the four engine commands moved into the measured set once the fixture gained the one line it lacked (isolating the user environment). It also found, and repaired, four living documents whose per-version tables had been scrambled by the very act of registering a row — rows inserted in reverse, a whole range losing its row framing, one range descending, one row never landing at all — and added the missing judgement: the version set is derived from the retrospective filenames, any document reaching the newest version must declare where its table starts, and after that start every version appears exactly once, in ascending order, up to the latest | delivered · released |
| v0.64 | a hand-maintained list becomes a derived one: `schemaFiles` (the schema files a coverage check scans) carried its own confession — "add it here when you add a schema; forgetting makes the check *looser*, silently". Three gates now derive it: the config-file table in `configuration.md`, the name appearing on a **code line** as an assignment or a `filepath.Join` argument (the definer resolves the path; a consumer only mentions it), and the file actually declaring json-tagged fields. Two counterexamples fix the shape: `verify/graph.go` names `ngm.json` in a const but is a *reader* of an upstream manifest (no tags), and `lock/schema.go` has eleven tags but names `ngm.lock` (not a `.json`, not in the doc's table). Derived set equals the hand-written three, and both directions are asserted — a missing entry and a stale entry each turn it red | delivered · released |
| v0.65 | every column of the matrix has to carry its own evidence: V37's eight config-error shapes and V39's two argument dimensions were hoisted out of the function body into named axes, and each column now has to (a) reach the configuration layer at least once and (b) differ from every other column — the signature being the per-cell channel plus the *whole normalised* output, with equivalence allowed only as a named registration. The guard's first run caught two **existing** columns: two fixtures produced byte-identical output for every command, because both stopped at an earlier validation ("version is required") — they were not testing the defect their names claimed. The fix was to repair the fixtures, not to register an equivalence | delivered · released |
| v0.66 | closing the holes that undermined the promises: this version's scope comes from an external line-by-line assessment rather than a candidate list, so for the first time the input is *defects that exist* rather than *capabilities that are missing*. All five P1s are fixed — the graph-resolution `ls-remote` now goes through the `run:git` gate (asserted by exit code **and** a spawn count of zero), symlink targets and manifest paths are checked for escapes before materialisation (reported as critical by `verify`, and the pre-check runs *before* the old tree is cleaned), short commits are expanded via `rev-parse` inside the mirror so what gets locked is what can be read back, the `write:` namespace gets honesty instead of a dead `CheckWrite` (deleted, with the two places that state the verdict aligned), and `internal/verify` gets its first in-package tests (exit-code precedence, aggregation, advice selection, meta comparison, escaping symlinks). P2/P3 in the same files: a path `@` is no longer stripped as userinfo (it used to resolve to a *different repository*); the OSV client gets a 30s timeout and a severity parser that understands the real schema without ever dropping an advisory; `mergeProject` returns a copy; global-mode mappings stop failing their own validation; `store prune` gains an age guard and refuses `--older-than=0`; and the shallow check's boundary is now *stated* in the summary line. `AssertNoSecrets` is finally wired in, `FetchMirrorTags` is gone, install fetches only when it must (asserted with a FETCH_HEAD probe), and install/update grow `--json`. A probe run during the version found one more pre-existing gap — the empty `--dir=` rejection carried no error envelope — now fixed | delivered · released |
| v0.67 | playing the PackageGate card: in January 2026 Koi Security disclosed **PackageGate** — six zero-days across npm / pnpm / vlt / Bun, where npm's entry was exactly *"a malicious `.npmrc` inside a Git dependency can override the git binary path, achieving code execution even with `--ignore-scripts`"* — and npm closed the report as "works as expected". That is the strongest external validation this project's thesis has ever received, and no page here mentioned it. This version writes it down in the competitive analysis (with the three checkable reasons the path cannot exist in ngm: zero mentions of `.npmrc` in the codebase, postinstall denied by default and sandboxed otherwise, a single git construction point behind the `run:git` gate), adds a "why you should use ngm — and when you should not" section to both READMEs, and ships a runnable comparison demo (`scripts/packagegate-demo.sh`, with a Windows entry point that delegates to the same implementation). The demo's fixture only writes marker files — it is a comparison, not an attack — and its ngm-side assertions all pass; the npm side is observation-only, because npm builds differ (this machine's npm refuses git-typed dependencies outright with `EALLOWGIT`). The tooth: breaking the fixture turns the assertion red and exits 1 | delivered · released |
| v0.68 | taking out the four redundant spawns: the install path used to spend **six** git subprocesses per dependency on the digest — computing it once, re-computing it inside `Put`, and then `Put`'s own `ls-tree` + `cat-file` — while a cross-layer diamond (A→C and A→B→C) re-resolved C entirely, including a real fetch. This version introduces `git.ArchiveMaterial` (entries + contents + manifest records + digest, computed once) with `BuildArchive` reduced to a thin wrapper over it, so the optimised path and the old path cannot compute differently; `Put` accepts the material optionally (**0 spawns**) and still verifies the two identity fields (commit and digest), while `installFromLock` deliberately keeps re-computing — what gets saved is duplicated *work*, not duplicated *checking*; and `ResolveGraph` now reconciles against `seen` before resolving. Every claim is asserted with counters, not stopwatches: `Put(±material)` 0/2 spawns, `install(fresh, one dependency)` 10 → 6, and the diamond's leaf resolved once instead of twice | delivered · released |
| v0.69 | opening the one market that pays for provability: `ngm sbom` exports the lock as **CycloneDX 1.5** (default) or **SPDX 2.3** — every dependency with its commit, its `archiveDigest` and a git purl. It reads the lock only (no network, no re-resolution, no writes), and its determinism is deliberate: the exporter sorts components itself, the SPDX document namespace is derived from a content hash rather than a random UUID, and the timestamp is the single non-deterministic field the way `resolvedAt` is in the lock. It refuses to guess: licences and copyright are `NOASSERTION` and `filesAnalyzed` is false — inventing a file list to satisfy a schema is the most common way an SBOM ends up lying. The most informative part of the version is what the existing contract nets did to a new command without being asked: the config matrix failed on "argument table has 21 entries but there are 22 commands", the exit-code ledger demanded evidence for all three declared codes, the code-source registry claimed the new "could not write the report" exit, and the docs net demanded usage text and a command-table row | delivered · released |
| v0.70 | attaching the predicate layer without inventing a signing system: `ngm attest` exports the lock as an **unsigned** in-toto Statement v1 whose predicate is SLSA provenance v1 — subjects are git purls carrying each dependency's `archiveDigest`, external parameters are the byte-level sha256 of `ngm.json` and `ngm.lock`, and materials are the pinned commits. ADR-027 answers what/who/verify up front: signing stays with the user or CI (`cosign attest-blob`), because a signature proves *who said it*, never *whether it is true*. Determinism comes first — there is no timestamp and the invocation id is the lock's own hash, so two exports of one lock are byte-identical and can be diffed before signing. The digest-scope annotation states in the data itself that the hash is over the canonical manifest, not the tarball | delivered · released |
| v0.71 | retiring the hand-copied ADR index: a new net (`TestV71AdrIndexCoversEveryAdrFile`) reconciles `docs/adr/README.md` against the actual `docs/adr/adr-*.md` set in both directions — exactly one row per file, every link resolves, exactly three columns per row, and gapless numbering (the discipline: superseded, never deleted). This is the direct payoff of the defect fixed in v0.70 (a missing row, a duplicate row and a fused row that had survived unnoticed for several releases). The tooth was tested by duplicating a row: the net went red naming the file and the count. Product code: zero lines; ADR-027 also gained a runnable CI snippet for the attest → cosign → verify path | delivered · released |
| v0.72 | deciding the lock's dependency-graph shape before writing any code: ADR-028 settles it field by field — an always-present `requiredBy` per entry (direct requirers, sorted and deduped; `[]` for the project's direct dependencies, because "missing" must mean exactly one thing: an old lock with an unknown graph), direct edges only (full paths are exponential, ADR-024), ghost-edge validation, and lockfileVersion unchanged (additive optional fields are the existing contract). Consumers are ruled too: `ngm why` goes offline with zero git spawns once the graph is present (falling back to re-resolution with the source named), SBOM grows CycloneDX `dependencies` / SPDX `RELATIONSHIPS`, and `verify` / `tree` / `attest` explicitly stay as they are. Implementation is v0.73's user-visible increment, with five acceptance items pre-written in the ADR. Also: the roadmap now reconciles the assessment's three stop-items (all upheld — including why v0.71's net was not a violation) and corrects a wrong citation in the v0.71 retro | delivered · released |
| v0.73 | implementing ADR-028 — the user-visible increment: every lock entry now carries an always-present `requiredBy` (direct requirers, including the `(root)` marker, normalised at serialisation time), `Validate` rejects ghost edges, and `ngm why` reads the lock instead of resolving upstream manifests — asserted at **zero git sub-processes** by the spawn counter, and it says so in the report (a trailing "来源" line in text, a `source` field in `--json`). Old locks still work and fall back to re-resolution, never silently. SBOM grows a CycloneDX `dependencies` section and SPDX `DEPENDS_ON` relationships, both derived from one shared edge table, and both omitted when the lock has no graph. Implementing it overturned one clause of the ADR (root edges must be stored, or `why` loses the root → node path in the root-wins case) and the ADR was amended in place | delivered · released |
| v0.74 | the standard for a JS library that will be consumed as a Git dependency, plus a checker that enforces it: the guide states 10 musts, 9 recommendations and 5 prohibitions, each with a reason and a way to verify, all derived from three facts in the code (the three-layer entry inference in `InferEntry`, transitive dependencies coming only from an upstream `ngm.json`, and install hooks denied by default). `scripts/check-library-standard.sh` (with a PowerShell entry point delegating to the same implementation) runs 22 checks — 13 must and 9 should, with `--strict` and `--json`. Five teeth were tested by breaking a conforming fixture (a `postinstall` hook, a `.npmrc`, an entry pointing at a git-ignored `dist/`, a missing `version`, git deps in `package.json` without `ngm.json`) and each went red naming the clause. Ecosystem claims that could not be verified offline are marked as such. Product code: zero lines | delivered · released |
| v0.75 | the standard's first implementation: a real, usable library — `canonical-json` (deterministic JSON serialization and hashing; zero dependencies, no build step, 13 files, 25 tests, repository initialised and tagged `v1.0.0`, waiting for a push) — plus the two defects that only surfaced by following the standard: its `ngm.json` example omitted `runtime` (one of the three required fields, so copying the example produces an invalid manifest), now fixed with a new must-check `ngm-json-valid` (teeth tested: removing `runtime` turns the check red and exits 1), and the LF clause now states that the check reads the working tree, so a Windows checkout must stay LF. The library's README link to the standard was changed from a cross-repo relative path to a URL. The checker grew from 22 to 23 checks; ngm product code: zero lines | delivered · released |
| v0.76 | end to end, driven by the user's own library: once `canonical-json` was pushed to Gitee, `ngm install / verify / mappings / why / tree / sbom / attest` all ran against the real public repository — the lock carried `requiredBy: ["(root)"]` for the first time on a real dependency, mappings resolved the entry from the library's `ngm.json` (layer one: `main: ./src/index.js`), verify said `✓ match`, `why` reported zero git sub-processes, the SBOM gained a root → dependency edge, and the attestation carried the real purl, digest and `gitCommit`. Two pieces of evidence that only a real run produces: the library's own 25 tests pass inside the **vendored copy** (no install, no build), and the standard checker passes on that same vendored copy. The first install was refused by the permission system exactly as designed (`net:gitee.com` not allowed), and GitHub is unreachable from this machine, so the run went through Gitee. Defect found: the checker treated a vendored copy (no `.git`) as "missing a semver tag" — it now has an explicit "not applicable" verdict that is printed, excluded from the counts, and reported as `skipped` in `--json` | delivered · released |
| v0.77 | the standard meets its second and third package shapes, and both turned out to be casualties: a package that explicitly declares `"type": "commonjs"` was failed for "not declaring type=module" — while the check's own wording says CJS must be declared — so a written-down choice is now accepted; and a monorepo root was judged against five package-only rules (entry, exports, types, ESM, publish scope) although a root is not a package. A `mono-root` role now marks those as not applicable, checks every workspace child with the same script, and prefixes child verdicts with their path. New must: every directory matched by `workspaces` needs its own `package.json`. `.gitattributes` and `.gitignore` are read at the repository root. One silent-drop bug was found while implementing it: the JSON merge skipped unreadable child results (`continue`) because Windows node cannot open MSYS temp paths — paths now go through `cygpath`, and an unreadable child becomes an explicit must-fail. The checker grew from 23 to 24 checks; `ngm add` was exercised end to end against the real library (add → install → verify match → why with zero sub-processes → mappings validate OK) | delivered · released |
| v0.78 | monorepos come in more than one flavour, so the standard now detects three signals — npm `workspaces`, pnpm `pnpm-workspace.yaml` and lerna `lerna.json` — and a pnpm root may legitimately have **no** `package.json` at all, which used to be a must-fail. Child globs understand `dir/*` and brace `{a,b}/*`, and a `packages` entry the tool cannot read (a YAML mapping or inline object) raises `workspace-manifest-unreadable` as a must-fail instead of quietly checking one child less. The sharpest finding of the version: **the checker was stricter than the product** — the standard recommends `exports`, the checker did not read it, so a package following the standard was failed for "no entry point" while `InferEntry` had always read it. The candidate layers are now aligned with `parseExports`, `index.cjs` was added, and 25 checks are verified across six package shapes. Release readings were re-measured from both APIs on 2026-10-10 — GitHub 65 tags / 60 releases (newest `v0.64.0`), Gitee 3 — and tags and releases are now two separate columns, because the previous reading conflated them | delivered · released |
| v0.79 | the second half of the standard becomes a command: `scripts/check-vendored-standard.sh` runs the same checker over every dependency in `ngm.mappings.json`, and attributes each verdict back to the dependency it came from. Three rules carry the value — the fact source is the mappings file rather than a walk of `ngm.vendor/` (a walk would count pruned leftovers as dependencies, so the same tree could produce a different verdict), the wrapper must run *all* the rules (a dependency's check count is reconciled against running the checker directly), and exit 2 means "nothing was checked" rather than "everything passed". A mapping that points at a missing directory is a must-fail instead of a skip. Building the second library — `jsonkit`, a monorepo whose two packages are usable on their own — surfaced one guaranteed false positive: `gitattributes-lf` must fail for every vendored subpath, because the LF declaration lives at the repository root while a subpath copy only receives the subdirectory. It is now "not applicable" for directories without `.git`, which also made the v0.76 check that demanded exactly one such verdict turn red — correctly, since that check exists to stop "not applicable" from quietly growing | delivered · released |
| v0.80 | getting the subpath end-to-end as far as it can go without a human push meant measuring the two blockers rather than working around them: `install --offline` refuses to install without a lock (by design — "an offline install can only materialise what the lock already pins"), and the address validator deliberately rejects single-label hostnames and IP literals, which rules out a local `git daemon`. So there is no local substitute, and the product rule was **not** relaxed to make a test green. Nested monorepos stopped being reported for the wrong reason: previously the inner root failed `entry-resolvable` ("no entry point found"), which points the reader at giving the repository an entry it does not need — it is a monorepo root, and what is missing lives in its own children. Now a `nested-monorepo` record names the declared pattern and how many children were **not** checked, the package-only checks are "not applicable", no grandchild verdicts appear at all, and the checks the root *can* answer still apply — nested is not a free pass. The conclusion carries `complete:false`, kept separate from `ok`, because a partial audit must never read as conformance | delivered · released |
| v0.81 | a line-by-line review of everything added in v0.66-v0.80, which found seven defects and fixed them: the symlink escape check only closed the POSIX case (the target string was never normalised, so a backslash relative target escapes on Windows - and none of v0.66's twelve boundary cases contained a backslash); `archiveDigest` only checked the prefix, so `"sha256:"` passed and SBOM/attestation emitted structurally valid but empty hashes; the SBOM would export a half graph; `Put(Material)` compared two values that came from the same call, so it proved nothing - it now re-derives the digest from the content (pure CPU, no spawn returns); a nil entry list was treated as an empty dependency and silently produced an empty vendor directory; the OSV cache path was built from an unvalidated commit; and SPDXID derivation was not injective. Two ADR contradictions fixed. A test that asserted on an error *substring* now asserts on the exit *code* - a judgement pinned to wording blocks improvements to wording | delivered |
| v0.82 | closing the six items v0.81 had logged as known-unfixed. The sub-path rule existed in two inconsistent copies (one treated a backslash as a separator, the other did not, so `#path=..\..\x` passed) - unified into one predicate, and the path an *upstream* declares now gets the same check (it was passed through untouched and ends up at `os.RemoveAll(dst)`; only a coincidence kept it harmless). Atomic writes were unified into one package: of the three copies of `writeFileAtomic`, only the lock one had the v0.66 `Chmod(0644)`, so `ngm.json` and `ngm.mappings.json` were still being written as **0600** - files meant to be committed and shared. `--out` now uses the same atomic path instead of a bare write. `ngm attest` reads the lock once and uses those bytes for both parsing and hashing. JSON exports no longer HTML-escape (bytes that get signed must match the field text). The SPDX namespace now covers the digest, and purl sub-paths are percent-encoded | delivered |
| v0.83 | loopback addresses become legal (ADR-029): `localhost` and `127.0.0.1` are accepted, other single-label hosts still are not, and every boundary is written down (`[::1]`, ports, the `git://` protocol and the `net:` gate all stay as they are). The measurement also corrected a sentence from v0.80: IP and internal-domain hosts were always allowed — only single-label names were refused, which made `https://127.0.0.1/x` legal while `https://localhost/x` was not. Along the way a silent degradation was fixed: `newProjectEnv` swallowed `config.Load` errors, so a broken config quietly reverted the protocol to https and dropped the user's declared token env vars (losing their redaction); a broken config is now exit 3 naming the file and the field, while a missing config still means defaults. This version also carries an accident, recorded in full: it started work without checking the working tree, overwriting two untracked files of a parallel line of work (no Git copy, no editor history), which have been honestly reconstructed with their evidence boundaries marked | delivered · released |
| v0.84 | making "check the working tree first" a reading rather than a gate: the release-status script now prints a **worktree state** section (uncommitted count, every uncommitted version artifact named with the note that an overwrite is unrecoverable, and in-place edits of snapshot docs called out separately) plus a **before you call it delivered** block naming the two commands to run instead of trusting memory. It is not a gate on purpose: a tree mid-development is legitimately dirty, and an always-red gate is no gate. The version also records a counter-intuitive finding — the v0.83 number collision *would* have been caught by the v0.71 ADR-index net (ADR number 028 used by two files); it did not fire because that net was not run at collection time, so the value of a net is existence times when it runs. And a decision: the git:// protocol stays refused (unauthenticated, unencrypted) — a product rule is not relaxed to make a test convenient | delivered · released |
| v0.85 | the same reading, used two ways: the main test job (all three platforms) now asserts the worktree is **clean** after the tests — a CI checkout starts clean, so dirt can only mean a step wrote files into the repo — while the identical reading stays print-only locally, where a tree mid-development is legitimately dirty. The softness of a check is a property of its context, not of the check. A new `release-state` job runs the release-status and docs-link scripts with `fetch-depth: 0`, because the status script deliberately shouts MISSING TAG for every version when it cannot see tags, and a check that misreports is worse than no check. The unpushed-count assertion added in v0.84 got its teeth: flipping the number in either language fails with the arithmetic spelled out | delivered · released |
| v0.86 | the mirror reading stops being a written-down snapshot and becomes something you can take on demand: `scripts/check-mirror-coverage.sh` makes two requests (tag refs + releases), reports the mirror's tag count, release count and newest release, and derives **unpushed = delivered − mirror tags**. It is a reading, not a gate — pushing is the owner's action, so a gap does not fail; only "cannot read" exits 3 while saying so out loud, which is the second application of the v0.85 lesson (one fact, two uses, chosen by context). It runs in the `release-state` CI job with `continue-on-error` for the same reason, and a local fake API (httptest) pins its three outcomes: readable, HTTP 500 → 3, unreachable → 3. The check also caught an implementation bug: counting JSON by lines reads a compact response of 65 tags as 1 | delivered · released |
| v0.87 | "the two sides must agree" becomes a check instead of a claim: an entry-alignment net runs eleven `package.json` shapes (a `ngm.json` first layer, `exports` as a string / `"."` string / conditions / a bare condition map / `require` only / `default` before `require`, `main`+`types`, `index.js`, `index.cjs`, and the no-clue case) through the product's `InferEntry` on one side and through the checker's own printed sentence on the other, so it guards both what the report says and what the product actually uses. Teeth: removing the checker's two `exports` layers turns four of the shapes red at once. The checker deliberately does not call the product code — merging the two implementations would leave only self-attestation — and one known hole is written into the test rather than left to be discovered: the checker prints only the entry, so `types` has no mechanical reconciliation | delivered · released |
| v0.88 | three things that were stated but not guarded became guards. The checker now decides the entry **by declaration** (same as `InferEntry`) with `rel_path` mirrored character for character, stops accepting any root `.d.ts` as a types entry, and splits "is there an entry" from "is the declared file actually there" (new must `entry-file-exists`); the alignment net grew to 15 shapes × (entry + types), closing the known hole v0.87 wrote down. A new net that parses every `scripts/*.ps1` with PowerShell's own parser immediately found two shipped Windows entry points that **could not be parsed at all** — without a BOM, PowerShell 5.1 decodes UTF-8 as ANSI, and a Chinese lead byte swallows the following newline, merging code into the previous line (measured: 0 errors as UTF-8, 3 as ANSI; four of the nine files already had the BOM, so the convention was right and five had drifted). Both wrappers were repaired and actually run. Readings now land on the CI job summary page, where a source that cannot be read says so instead of disappearing | delivered · released |
| v0.89 | the disease behind v0.88's two unparseable `.ps1` files was not those files but the state of *nothing running them*. Every artifact under `scripts/` (19 of them) is now accounted for: a run-or-explain net follows the reference closure (tests → workflows → other scripts) and anything it cannot reach must be listed with a reason, keys included only if they exist on disk; every `.sh` passes `bash -n` (symmetric with v0.88's PowerShell parse net); every `scripts/<file>` named by a workflow must exist; and `packagegate-demo.sh` is actually run — the test requires its own `0 failed` line rather than a quiet exit 0. The sweep also found two entry points that no document even mentioned, and the convention is now written down: the `.ps1` twin only delegates, the `.sh` is the single implementation | delivered · released |
| v0.90 | the run-or-explain registry became a to-do list instead of a permanent exemption: five `.ps1` entry points are now actually run, each with a **deliberately failing input** so the test proves the exit code and the named failure survive delegation (an unqualified vendored fixture, a repo missing a tag, a fake API whose numbers must appear, an `-Out` file that must exist and be appended to) — the registry shrank from five entries to two. Wiring those in surfaced two more seam defects: two entry points swallowed parameters (`check-mirror-coverage.ps1` never forwarded `--api-base`, the one option that exists so a test can point the reading at a local fake API; two others dropped `--help`), now guarded by an option-parity net that requires **quoted forwarding** — its first version was fooled by its own explanatory comment until the teeth check turned red. And the docs link check, which every CI run pays for, measured **133.72s** because the inner loop spawned a process per link (248 files, over a thousand links); a pure-bash path normaliser brought it to **33.07s**, with a negative-case net keeping the semantics (a backticked example must stay ignored) | delivered · released |
| v0.91 | finishing the second step: v0.90 removed the per-link `cd … && pwd` (133.72s → 33.07s) but each link still forked a subshell for path normalisation; moving that into a single awk process left the checker at **1.33s** (≈100× versus where it started — and every CI run and full test pays it). The rewrite was authorised by a line-by-line comparison against the previous implementation over a 20-case hostile fixture (backticked examples, two links on one line, `..` climbing to the root, a root file with an empty base, anchors, external links, parentheses inside labels and targets, subdirectories, `.txt`, `vendor/`) — only `IDENTICAL` was good enough to commit, and the comparison caught two bugs of mine (`sub()` silently no-ops when the accumulator has no slash, so a second `..` did nothing; and an empty base produced a leading `/`, turning a relative link into an absolute one). That comparison is now a standing golden-case net, including one documented blind spot: a target containing `)` is invisible to the check. The run-or-explain registry shrank to a single entry — the Gitee reading turned out to be offline-testable all along (`GITEE_API_BASE` was always there), so its recorded reason had simply never been checked | delivered · released |
| v0.92 | closing the one blind spot v0.91 wrote down and deferred: a link whose destination contains balanced parentheses — `[x](./b(c).md)` — is perfectly legal, but the old matcher stopped at the first `)`, sliced the target down to `c`, and silently skipped it, so such links were neither reported nor counted (a check that misses things is worse than no check: it makes people feel covered). Measured first: zero such links exist in the repository's 253 markdown files, so strengthening the check could not turn anything red by surprise. The destination is now scanned with a paren counter, falling back to the old reading when parens are unbalanced, and the before/after fixture shows the delta exactly — one reported broken link before, two after, while a link with parentheses that *does* exist stays unreported (proving the new parser does not simply flag anything with parens). The golden cases now carry both sides, so the closure of that blind spot has a standing witness instead of a sentence in a retrospective | delivered · released |
| v0.93 | the same disease as v0.92, other face: the link check only looked at `.md`/`.txt` targets, so renaming `LICENSE`, moving `internal/errs/errs.go` or renaming `docs/guides/` would let documentation links rot silently. Measured first: of 2103 links, 2087 are `.md`, 13 point at **directories** and 3 at `.go` files — the last two kinds were never checked, yet all 2103 exist today (zero debt, so widening cannot turn anything red by surprise). The extension filter is gone; only targets that genuinely may be absent are skipped (`node_modules/`, `vendor/`, `ngm.vendor/`, `dist/`, `.git/`), and existence now has two grades: `.md`/`.txt` must still be a **file** (a link to a directory is not "that document is there"), everything else just has to exist, because 13 real links point at directories and `-f` would have reported them. Before/after on one fixture: two broken links reported, four after — the two additions being a missing source file and a missing directory, while targets that exist stay unreported | delivered · released |
| v0.94 | the `subPath` end-to-end, on the books since v0.79 as "needs a monorepo remote to be pushed", became a standing check — and the premise turned out to be wrong in two ways. A local `git daemon` does work (it ran, it served, the clone succeeded) and loopback hosts were opened for exactly that in v0.83's ADR-029, but `git://` is only an address form: the fetch transports are ssh and https only (`git.defaultProtocol`, see `protocol.go`), and install rebuilt the canonical into `https://127.0.0.1/...` and tried port 443. The second half of the premise was unnecessary: the subPath contract — filtering the sub-directory, recording `path`, deriving the entry from the sub-package's own `exports` — lives in the vendor and mappings layers, so the repo's existing local-mirror facilities (a mirror holds the whole repo, which is what a monorepo really looks like) plus `--offline` exercise it end to end: init → add `#path=packages/a@v1.0.0` → update --all --offline → install --offline → verify → mappings validate. The assertions are the contract itself: only the sub-package is vendored, its sibling is nowhere, mappings point at the sub-package's exports, and a nonexistent sub-path fails at update without leaving half a vendor tree | delivered · released |
| v0.95 | the fetch half moved onto this machine too: v0.94 proved the `subPath` contract needs no remote, and this version proves fetching needs no push either — an https server on local 443 hands every request to the **real `git http-backend`** (smart HTTP, CGI semantics) behind a freshly generated self-signed certificate, and then `init → add https://127.0.0.1/acme/lib.git@v1.0.0 → update --all → verify`. The backbone of the check is its first assertion: the server's request counter must be greater than zero, because otherwise this "end to end" might have been read out of a warm local mirror (which is exactly what v0.94's `--offline` path does) and would prove nothing. The other three: `ngm.lock` pins the upstream HEAD (the ref really was resolved), mappings are generated, verify passes. One boundary was measured and written down: `GIT_SSL_CAINFO` is an OpenSSL-build knob that Git for Windows' default **schannel** backend ignores (`schannel: SEC_E_UNTRUSTED_ROOT`), leaving "disable verification" as the only option — because the other two both amount to something a test must not do (installing a certificate into the machine's trust store, or switching TLS backends). So the test states what it does and does not prove: URL construction, smart HTTP, ref resolution and materialisation — not TLS chain validation | delivered · released |
| v0.96 | the link check's existence verdict was platform-dependent: `[x](./Guides/a.md)` (the directory on disk is `guides/`) passes on Windows and fails on the Linux CI — one repository, one set of rules, two verdicts, which is the "instrument that lies" shape this project keeps catching. Measured first: 2127 in-repo links, zero case mismatches, so the new check adds no reds and guards the next author. It lives in Go rather than back in the shell, because per-segment name comparison needs directory reads and the shell check had just been brought from one subprocess per link down to a single awk process (1.33s): existence stays with the shell (fast, case-insensitive-platform proof), case belongs to a Go net that walks each segment through `ReadDir` with a cache — both covering the same files and links, including v0.92's balanced-paren reading. The tooth builds a temporary tree (the real repository has no mismatched link to try): a correct link, a wrong-case one, a `../` form and one inside inline code — exactly one reported, with both spellings named One more thing, courtesy of this version's own retrospective: demonstrating a wrong-case link inside a fenced code block made the check report three *examples* — falsifying the header's own note that code blocks need no handling — so a two-line state machine now skips fenced blocks entirely, with golden cases for both sides (a link inside a fence is ignored, one just after it is not). | delivered · released |
| v0.97 | the link check has two implementations on purpose (the shell owns existence, Go owns case) — and "both pass" does not imply "they agree", which is v0.78's original lesson: two implementations of one thing, never reconciled, and the price was a misjudgement. This version puts them on one corpus of 25+ shapes and compares the three-tuple of every broken link (file:line:raw) sorted, then repeats it on the real repository (both empty, and Go must still see more than a thousand links, so the comparison cannot quietly become empty). It found three things at once: the Go side was missing v0.96's fenced-code-block rule, so an example inside a fence would be treated as a real link — the scanning core is now Go's single implementation, shared by the case check and this reconciliation; the shell had a semantic bug where a `/`-prefixed link (repo-root relative, as in Markdown) was resolved against the file's directory, so `[x](/LICENSE)` looked for `docs/adr/LICENSE` — measured first (zero such links exist, so the fix changes nothing today), and fixing it hit a second trap (stripping the leading slash must happen before normalisation, or `/README.md` is looked up as `C:/…/Git/README.md`); and a `filepath.Rel(absolute, relative)` call that errors on Windows and was being swallowed as "nothing to check". The 443 conflict from v0.95 also became a registered environment condition (skip with the reason printed) | delivered · released |
| v0.98 | the reconciliation idea turned on another pair: entry-path normalisation (the product's `relPath` versus the checker's `rel_path`), where just *reading* both implementations already showed two divergences — and both are impossible to notice on Windows. A backslash in `main` was handled by `filepath.ToSlash`, which converts on Windows and does not on Linux, so one `main: "src\\x.js"` yields different entries on two machines; multiple leading slashes were trimmed one-at-a-time by the product (`//src/x.js` → `.//src/x.js`) and all-at-once by the checker (`./src/x.js`) — same file, different printed path, and "what the report prints is what the product uses" is the entire point of that chain. Both fixed on the product side (the product is the contract), with an 11-cell net going through the public API so it can be registered, and the alignment corpus grown from 11 to 22 shapes (exports arrays, nulls, nested objects, ordering, whitespace, `*` patterns; main with backslashes, repeated slashes, whitespace, bare names) — 27 subtests green. One honest boundary is written into the test: the backslash cell cannot be distinguished behaviourally on this machine, its value is on Linux CI, and the teeth only fire for the slash cells. v0.97's reconciliation net was also added to the registry, which had been missed | delivered · released |
| v0.99 | a claims ledger: one sentence ("the two sides must agree") was worth two real holes. v0.98 concluded that "verbatim mirror" is a promise, and promises do not check themselves; this release inventories every such promise. Six words across Go non-test sources plus `scripts/*.sh|*.mjs` gave 14 hits, of which four were unguarded. The first: `wasmEngine.entryArgs` prepended `entry.Args` a second time (`buildInvocation` had already done it) while the comment right above said the two sides must agree — the existing test compared the output of the two *builders*, i.e. at the wrong level, and wasm entries usually have empty args, so three reasons stacked up to keep it alive. Fixed the v0.82 way: make it one place instead of two, with a judgment split across a behavioural layer (which cannot see the second prepend) and a structural one (which can) — the teeth prove the split is not decoration. The second: `mappings.lockFileName` claimed coverage by "CLI integration tests", but the constant was unexported, so no package could even see both names; it is exported now and compared where both can be imported. Two more promises had no net at all: the two SBOM formats must describe the same graph (edges normalised to parent → child, compared one by one; an incomplete graph drops them entirely on both sides), and `slugHosts`/`hostSlugs` must stay in sync (inverse maps, a public-API round trip, unknown hosts preserved). The main deliverable is the ledger itself — six words as explicit data, with failures for an unregistered promise, a reworded registration, or a net name that does not exist. Net product change: −5 lines | delivered · released |
| v0.100 | layer 1 gets the accounting layer 2 has had since v0.7, starting from a fair question ("if I only ever use the latest version of the code, does a repo with many commits waste my disk?") measured before it was answered. A real JS library (`express`, 6181 commits / 3982 refs) has an **18.41 MiB** mirror whose HEAD tree is **694.33 KiB** — about 26×, against roughly **228×** compression versus one snapshot per commit, so "many commits" is not where the cost is. Two accounts were invisible instead: **40% of the objects (21610/54155) belong only to non-HEAD refs**, and an interrupted `git clone --mirror` leaves a temporary pack that nothing owns (one measured here at **66.82 MiB** with zero usable objects and no `packed-refs`, while `HEAD`/`objects/`/`refs/` are all present — it *looks* like a working mirror), which `git gc` only removes after **14 days** (measured: 13 days survive, 15 are removed — `gc.pruneExpire`'s default `2.weeks`). So `store usage` now reports layer 1 per repo (bytes / packed history / residue) and names directories with **NO REFS**; `store prune` reclaims that residue after **1 hour**; a new `store gc` runs `git gc` per usable mirror (no network, no `--prune=now`, skipped directories named, and a partial failure exits **4** while still printing what was collected). `--single-branch` and partial clones are **refused** in ADR-030 — they would break `refType` resolution (ADR-004) and turn offline digest replay (ADR-008) into a download. A pre-existing flaw was fixed on the way: `store usage` returned early when the content store was absent, telling a layer-1-only machine there was nothing to report. Net **+588 lines**, 12 new nets, 3 teeth (all verified) | delivered · released |
| v0.101 | measured the three promises this repository makes about running other people's code, and **all three were false**. A sandboxed `import "npm:is-number@7.0.0"` really downloaded the package with every `--deny-*` in place (npm resolution bypasses `fetch`; `--no-remote` only covers http(s) modules), a dependency's own `deno.json` was auto-loaded because the sandbox cwd *is* that dependency, and `install` reported success while silently dropping one of two paths differing only by case — with the very next `verify` calling it critical. Fixed by naming three module-loading switches (`--no-npm --no-config --no-lock`), probing the **target filesystem** for case folding before the vendor tree is cleaned, and making exit 2 outrank 4 (an established tamper is not a retryable network blip). Two permission gaps: `run:git` never covered `ngm modules` (six `Options{Dir}` without a policy, on a path that *writes* refs), and `deny: ["env:X"]` never reached engine subprocesses — three enforcement points now share one `internal/envutil`. Two consumer exits: `verify --format=sarif\|github` (deterministic, no invented line numbers, levels follow blocking semantics; validated against the official SARIF 2.1.0 schema) and `ngm adopt` (ADR-040: describe an existing `node_modules` in ngm's own manifest format without taking over installation; registry provenance is explicitly not its axis). Five "output looks fine, conclusion is wrong" defects closed on the evidence path: `attest` read its manifest twice while the bytes get signed, `TagSignature` turned "git never ran" into a security verdict, `mappings` entries could escape the vendor tree, `why` bounded hits but not the search (1M paths / 626 MB while reporting no truncation), `atomicfile` never synced the directory entry. Also: the content store's own manifest is no longer trusted (a NUL in `Path` turned one record into two, and `BlobPath` followed a `../../…` "sha" out of the store), and `verify` no longer reports "we never asked" as "the ref moved". Three real Gitee fixtures, generated with plumbing so every machine gets the same SHAs. Deliverables include two new packages (`adopt`, `envutil`), one bound buffer package, ADR-039 and ADR-040, and a composite action; three of the repo's own nets were found red before any of this started | delivered |

---

## How to build locally

```bash
go build -ldflags "\
  -X github.com/idcu/ngm/internal/version.Version=0.4.0 \
  -X github.com/idcu/ngm/internal/version.GitCommit=$(git rev-parse --short HEAD) \
  -X github.com/idcu/ngm/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o ngm ./cmd/ngm
```

## Release artifacts

```bash
bash scripts/build-release.sh          # Linux / macOS / CI
powershell -File scripts/build-release.ps1   # Windows
```

Produces the six documented targets plus `SHA256SUMS`, all with build metadata injected:

| | |
|---|---|
| names | `ngm-<os>-<arch>[.exe]` with `GOOS`/`GOARCH` values (see `guides/installation.md`) |
| flags | `-trimpath` (reproducible) and `-ldflags -X` for `Version` / `GitCommit` / `BuildTime` |
| checksums | `SHA256SUMS`, one `<hex>  <name>` per line, consumable by `sha256sum -c` |

Pushing a `v*` tag runs `.github/workflows/release.yml`: it **tests before packaging** and then
publishes the assets. Names deliberately do not come from `uname` — `uname -m` reports
`x86_64`/`aarch64`, which do not match Go's `amd64`/`arm64`, so a `uname`-derived URL 404s.

## Performance baseline

```bash
NGM_BENCH=1 go test -count=1 -run TestBaseline -v ./cmd/ngm
```

Measures install (cold/warm), verify (online/offline/deep) at 10 and 100 dependencies, and digest
replay on a 100 MiB dependency — all on local fixtures, no network. It exists so the numbers in the
retrospective can be re-derived rather than trusted; it is skipped by default because it takes
minutes.

## Verifying the loop

```bash
go build ./... && go vet ./... && go test ./...
test -z "$(gofmt -l .)" || gofmt -l .

# milestone acceptance (one executable acceptance per milestone)
go test -count=1 -run 'TestM[1-7]Acceptance' -v ./cmd/ngm

# the engine integration test needs a real esbuild and skips without one
npm install --global esbuild && go test -count=1 -run TestM6Acceptance_RealEsbuild -v ./cmd/ngm
```

Every test uses local Git fixtures created with `t.TempDir()` — **no test touches the network**.

### The tests are immune to your local Git configuration

Fixtures explicitly override anything a developer's global config could break, so a
green run means the same thing on every machine:

| Your global setting | What would break without the override | Where it is handled |
|---------------------|----------------------------------------|---------------------|
| `commit.gpgsign` / `tag.gpgsign = true` | annotated-tag fixtures fail with `No secret key` | `testutils.GitInit` sets them to `false` per-repo |
| `core.autocrlf = true` | LF files silently become CRLF in blobs | per-repo `false`, plus `.gitattributes` |
| `core.hooksPath` / `init.templatedir` | your hooks run inside fixtures and fail the commit | `GitInit` points `hooksPath` at an empty dir; `git init --template=` |
| a stray `GIT_DIR` / `GIT_WORK_TREE` | git operates on the wrong repository | both fixtures and `internal/git` strip these from the child env |
| a malformed `~/.ngm/config.json` | `config validate` results depend on your machine | `testutils.IsolateUserEnv` redirects `NGM_HOME`/`HOME`/`USERPROFILE` |

If you hit an environment problem we have not covered, please report it — the fix
belongs in `GitInit` / `IsolateUserEnv`, not in a local workaround.

## Line endings

**LF everywhere.** See [`.gitattributes`](./.gitattributes) for why this is not just style:

- `gofmt -l` and `golangci-lint` treat CRLF Go files as unformatted
- golden files are compared byte-for-byte, so CRLF makes a snapshot fail forever
- M2's `archiveDigest` is a byte-level hash

CI enforces both (`fmt-check` job: `gofmt -l` plus a CRLF grep over `.go`/`.md`/`.yml`/`.golden`).

## Repository layout

```
cmd/ngm/                 CLI entry (one file per command)
├── acceptance_m1_test.go  M1 executable acceptance (offline, cross-platform)
internal/core/           language-neutral Git/module proof core (ADR-033)
├── atomicfile/          atomic file writes
├── digest/              archive + module manifest formats, LFS detection
├── errs/                NgmError + exit-code contract
├── git/                 git subprocess, ls-remote, mirror/partial clone, credential discipline
├── lock/                ngm.lock v2 schema, deterministic serialization
├── resolve/             URL canonicalization, refType, graph resolution, module targets
├── vendor/              mirror (l1) + module cache (l1.5) + content store (l2) + link tree (l3)
└── verify/              four checks, drift classification, JSON report

internal/
├── config/              ngm.json schema, three-tier merge, atomic read/write
├── mappings/            ngm.mappings.json schema, entry inference, validation
├── adapter/             engine catalog, argv translation, subprocess protocol, fallback, `self` stub
├── observability/       why / tree / outdated
├── supplychain/         OSV / policy / minimumReleaseAge
├── sbom/ · attest/      SBOM / attestation outputs
├── security/            permissions + sandbox
├── integrations/        Vite / esbuild / Deno / Webpack scaffolds
├── testutils/           golden files + Git fixture builders
└── version/             ldflags-injected build metadata
testdata/                fixtures/ and vectors/ (golden files)
.github/workflows/       CI (build/vet/test/-race/fmt/acceptance M1–M7/engine integration) + release
scripts/                 docs-link checkers, release builder (sh + ps1)
.gitattributes           LF enforcement (see "Line endings")
```

## Design notes

- **Two representations, one source of truth.** `github.com/org/repo` (canonical) is what git and the mirror layout use; `github:org/repo` (slug) is what `ngm.json`, the lock, and CLI output use. `resolve.Normalize` and `resolve.ParseSlug` are the only constructors.
- **Annotation peeling is non-negotiable.** `git ls-remote` emits two rows for an annotated tag (the tag object and its `^{}` commit). Taking the wrong row makes `verify` report false drift later.
- **`--ref-type` is required except for a full 40-char commit.** Inference can be wrong (a branch may be named `v1.2.3`), so ngm only defaults when the syntax is unambiguous; `--infer-ref-type` is an explicit, remote-evidence opt-in and fails on tag/branch ambiguity (ADR-035/036).
- **Tokens are never touched.** ngm passes the environment through and redacts `userinfo` from every error message; mirror `.git/config` files are asserted to contain no credentials.
- **The digest is over a manifest, never an archive.** Host archive APIs, tar/gzip parameters and `.gitattributes` export filters are all outside ngm's control, so hashing their output would make the same commit digest differently per host (ADR-008). ngm builds the byte stream itself, from the local mirror.
- **`NUL`-separated, byte-sorted, no newline conversion.** The manifest records are separated by a single `0x00` (the spaces around `\0` in the ADR are typography, not bytes); sorting is Go's byte-wise `<`, so no locale can change the result; CRLF is never rewritten. Any of these slipping would silently change every digest.
- **Unsupported content is refused, not hashed.** An LFS pointer is a *pointer*, not the file; a gitlink lives in another repository. Hashing either produces a proof of the wrong object, so both are hard errors.
- **Content trees are re-verified on write.** `ContentStore.Put` recomputes the digest from the mirror and rejects a mismatch (`exit 2`) — otherwise a bad write could make `verify` pass forever.
- **`install` never re-resolves, `update` always does.** A lock that silently follows upstream would make the proof worthless. Only `ngm update` moves a pinned commit; `install` reports lock staleness and refuses to drift.
- **Conflicts are detected before refs are collapsed.** Two upstreams requiring different versions of the same repo would otherwise be merged into one node (whichever came first), and the conflict would become invisible. Layer merging therefore compares `ref`/`refType` and fails with `exit 3` plus the full provenance chain.
- **`ngm.vendor/` is derived, so it is deleted and rebuilt.** Anything you edit inside it is lost on the next `install`. This is deliberate: a derived artifact that is partially preserved is worse than one that is always regenerated.
- **⚠️ hardlink mode shares inodes with the content store.** Editing a vendored file *in place* (e.g. an editor that writes through the same inode) therefore corrupts the content store too — and a later `install` will not notice, because the digest directory already exists. Use `vendor.linkMode: "copy"` when you need to edit vendored files. The detection path is `ngm verify --deep`, which replays the digest from the installed content tree — the only check that can see it, because comparing vendor against content is a no-op when both sides are the same inode.
- **Drift is classified by ancestry, not by inequality.** A branch whose ref moved and a branch whose history was force-pushed look identical from the outside; only `merge-base --is-ancestor` separates a normal advance (`expected`) from a rewrite (`unexpected`). When ancestry cannot be proven, ngm reports the *suspicious* verdict: an unprovable fast-forward must not pass a gate.
- **Content-addressed entries are shared, so `meta.json` is provenance, not identity.** Identical content in two dependencies (or two projects) legitimately resolves to a single store entry, and `meta.json` records whoever wrote it first. Treating its `repo`/`commit` fields as expected values would report deduplication as tampering. Identity is the digest (the directory name) plus the bytes — which is exactly what `--deep` re-derives.
- **`--deep` takes the *shape* from Git and the *bytes* from disk.** A filesystem cannot reproduce Git semantics — most notably the exec bit, which Windows cannot represent; inferring modes from the directory would make the same content hash differently per platform and fail healthy trees on Windows. So paths and modes come from the commit tree in the mirror, and only the bytes come from the installed tree.
- **"Could not check" is never "passed".** A network failure, or a cold mirror under `--offline`, is reported as exit 4 and outranks a clean verdict: an incomplete verification must not be readable as proof. Correspondingly, `--json` marks a check that could not run as `operational` instead of omitting it.
- **Engines are opaque, so ngm is explicit about what it cannot do.** The `self` stub refuses to produce an artifact — a stub that echoed its input would let CI report a build that never happened. The same reasoning makes esbuild refuse `typeCheck`: it strips type annotations without checking them, so a green build would say nothing about types while looking like it did.
- **The catalog only lists engines that are actually adapted.** Advertising `tsc` / `deno` / `postcss` before the adapters exist would make `ngm engines list` lie. Custom engines remain first-class: the subprocess driver is generic, and its argv mapping is pinned in `internal/adapter/subprocess.go` — `--kind=<kind>`, `--<key>=<value>` (sorted, so argv is reproducible), the input file as a positional argument, `--outfile=<path>` for output.
- **Capability is checked before availability.** "This engine cannot do that" and "this engine is not installed" are both exit 5, but the order decides whether one fix is enough: telling someone who configured esbuild as a type checker to *install esbuild* sends them to install a tool that will never be able to do the job.
- **No shell, ever.** Catalog commands are tokenized (quotes are honoured so Windows paths with spaces work) and executed directly — pipes, redirection and `$(...)` stay literal. The catalog arrives with the repository, so going through `sh -c` would hand arbitrary code execution to any pull request.
- **`--alias` points at `main`, not at the directory.** The mappings schema defines `main` as exactly "this dependency's entry file"; handing engines the directory instead makes resolution depend on each engine's own folder rules (esbuild looks for `package.json` / `index.js`, not `index.ts`).
- **Engine failures map to exit 1, and the engine's own exit code is kept in the message.** Passing it through would let an engine's private codes collide with ngm's public ones (an engine exiting 5 for a syntax error would read as "engine unavailable"). Nothing is lost: the code, the stderr, and the exact resolved command are all reported.

## License

**MIT** — see [`LICENSE`](./LICENSE).

Earlier revisions of this file said "all rights reserved, no license granted". That made
viewing the only thing anyone was permitted to do with it, which contradicts the one step
this project still needs: a real user. The change is recorded in
[`docs/development/v0.12-plan.md`](./docs/development/v0.12-plan.md), together with the other
findings from the same review pass.
