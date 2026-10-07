# ngm

> Git-first Dependency Provenance Layer

ngm is a Node.js / Deno package manager with **provable** Git dependency tracking. Every dependency is locked to a specific commit, content-addressed by an `archiveDigest` (SHA-256 over the canonical file listing), and verifiable on demand via `ngm verify`.

Status: **v0.1 … v0.52 delivered in source**, and all fifty-two tags `v0.1.0` … `v0.52.0` exist.
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

**Release state, read from the APIs on 2026-10-04** (not from this file's memory):

| source | releases | what is missing |
|--------|----------|-----------------|
| GitHub | **48 of 52** | **`v0.5.0` … `v0.8.0`** — the mirror forwarded those tags without triggering `release.yml` |
| Gitee | **3 of 52** | **`v0.1.0`**, plus `v0.5.0` … `v0.52.0` |

New versions are released by the tag push itself, and that path has now worked forty-four times
(`v0.9.0` … `v0.29.0`) — with one miss: `v0.28.0` first pushed with a red CI, and its tag was re-pointed at the fix.

**Back-filling those forty-nine historical artifacts is deferred by decision (2026-10-04)** — not
forgotten: every version in the gap has its capabilities in source (`v0.5`…`v0.52` compile with the
"from source" path in [`docs/guides/installation.md`](./docs/guides/installation.md)), and the newest
version is downloadable today. What it costs is a credential — `POST /actions/workflows/…/dispatches`
returns 401 without a token, and the Gitee uploader needs a `GITEE_TOKEN` — so resuming it is one
command per version. The decision, its rationale and the signals that would make it worth resuming
are recorded in
[`docs/development/README.md`](./docs/development/README.md#发布清单每个版本).

Until then, **`v0.4.0` is the newest version downloadable from both sources, and `v0.52.0` is the
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

## What works today

### M0 — 工程骨架与配置层

- `ngm --version` / `ngm --help`
- `ngm init <name> --runtime=node|deno` — generate a `ngm.json` template
- `ngm config validate` — schema-check the project config
- `ngm config show` — print the merged effective configuration (builtin → global → project)

### M1 — Git 层

- **URL canonicalization** — 4 protocols (`https` / `git@host:path` / `git://` / shorthand) plus `ssh://`, explicit host, and bare canonical forms all normalize to `host/org/repo`; `Normalize` is idempotent
- **Slug form** — `github:org/repo` for `ngm.json` / lock / CLI output, distinct from the canonical URL used for git operations
- **refType resolution** — `tag` / `branch` / `commit` → commit, with **annotated tag peeling** (`^{}`) so the lock records a commit, never a tag object
- **mirror** — `~/.ngm/mirror/<host>/<org>/<repo>.git`, `git clone --mirror` on first use then `git fetch --prune`; resolution prefers the local mirror, so `--offline` works
- **credential passthrough** — ngm never reads or stores tokens; the environment is passed to git as-is and all output is redacted (`https://user:token@host` → `https://***@host`)
- `ngm add <url>[@<ref>] --ref-type <type>` — writes `ngm.json`; `--ref-type` is required (inference is only ever a hint)
- `ngm update [<dep>|--all] [--offline]` — re-resolves refs to commits

### M2 — archiveDigest (ADR-008)

- **Canonical manifest** — `ngm-archive-digest/v1`: header + one `path NUL mode NUL sha256 NUL` record per non-directory tree entry, sorted by **UTF-8 byte order**. `archiveDigest = "sha256:" + sha256(manifest)`
- **No newline conversion** — blobs are hashed as-is; a frozen vector contains a CRLF file to prove it
- **No Git host archive API** — the manifest is built from the local mirror, so digest generation is fully offline
- **LFS pointers and gitlinks are rejected** (`exit 3`), never silently hashed
- **content store** — `~/.ngm/content/sha256/<hex>/{tree/,meta.json}`; the tree is unpacked and byte-verified, symlinks are recreated, exec bits preserved
- `ngm update --digest` — print the `archiveDigest`; `--store` also populates the content store

Frozen vectors live in [`testdata/vectors/`](./testdata/vectors/) and are asserted on all three CI platforms.

### M3 — 依赖图与确定性 lock

- **Transitive dependencies** — resolved breadth-first from each upstream's `ngm.json` only (never `package.json`; a warning is emitted if upstreams declare Git deps there). 4-way concurrency within a layer, deterministic results
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
- **`linkMode`** — `auto` (hardlink, falling back to a full-tree copy) / `hardlink` (no fallback) / `copy` / `symlink` (the dependency directory is linked as a whole). The **actually used** mode is reported, so a silent fallback cannot hide
- **monorepo sub-paths** — only the sub-directory is materialized (`ngm.vendor/…/packages/core`), matching `dependency-resolution.md`
- **cache (layer 4)** — `~/.ngm/cache/{metadata,osv,tmp}`, disposable by contract; `ngm cache clean` never touches mirror or content store
- **mappings** — `ngm.mappings.json` with three-tier entry inference (`ngm.json` → `package.json` exports → `./index.ts|js`); a missing entry is a warning, not a failure (the spec says the mapping is still generated)
- **`ngm mappings validate`** — checks `from` against the lock, `to` on disk, and `main`/`types` existence
- **`VerifyVendorTree`** — full file-by-file sha256 comparison of vendor against content (no sampling), reused by M5's `verify`

### M5 — verify

`ngm verify` runs three checks per dependency and answers one question: **is what is on disk still what the lock claims?**

- **check 1 — ref → commit.** Re-resolves the declared `refType` and compares it with the pinned commit.
- **check 2 — digest replay.** Rebuilds the manifest from the local mirror at that commit and recomputes `archiveDigest` (fully local, works offline).
- **check 3 — landing integrity.** Content-store metadata plus vendor tree structure (paths, symlink targets, sizes). `--deep` adds byte-level verification: the digest is replayed from the *installed* content tree and every vendor file is hashed against it.

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
| v0.19 | **the text users see is an interface too**: every command's usage now writes its own `EXIT CODES` (8 new + a correction to `install`, which had silently omitted 2 and 5), `config` learned `--dir` and stopped reporting OK for a directory with no manifest, and the internal stage labels (`M\d`) leaked into user-visible strings were removed | delivered · released |

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
internal/
├── config/              ngm.json schema, three-tier merge, atomic read/write
├── errs/                NgmError + exit-code contract (0–5)
├── git/                 git subprocess, ls-remote parsing, mirror clone/fetch, credential discipline
├── resolve/             URL canonicalization, slug, protocol → clone URL, refType resolution, add-spec parsing
├── digest/              ADR-008 manifest format, digest, LFS pointer detection
├── lock/                ngm.lock schema, deterministic serialization, atomic read/write
├── mappings/            ngm.mappings.json schema, entry inference, validation
├── vendor/              mirror (l1) + content store (l2) + link tree (l3) + cache (l4) + integrity check
├── verify/              `ngm verify` engine: three checks, drift classification, exit-code contract, JSON report
├── adapter/             engine catalog, argv translation, subprocess protocol, fallback, `self` stub
├── testutils/           golden files + Git fixture builders (annotated tag, force push, LFS, gitlink, monorepo)
├── version/             ldflags-injected build metadata
└── ...                  (digest / lock / supplychain / adapter / mappings / observability — land in M2+)
testdata/                fixtures/ and vectors/ (golden files)
.github/workflows/       CI (build/vet/test/-race/fmt/acceptance M1–M7/engine integration) + release
scripts/                 docs-link checkers, release builder (sh + ps1)
.gitattributes           LF enforcement (see "Line endings")
```

## Design notes

- **Two representations, one source of truth.** `github.com/org/repo` (canonical) is what git and the mirror layout use; `github:org/repo` (slug) is what `ngm.json`, the lock, and CLI output use. `resolve.Normalize` and `resolve.ParseSlug` are the only constructors.
- **Annotation peeling is non-negotiable.** `git ls-remote` emits two rows for an annotated tag (the tag object and its `^{}` commit). Taking the wrong row makes `verify` report false drift later.
- **`--ref-type` is required.** Inference can be wrong (a branch may be named `v1.2.3`), so ngm surfaces it as a hint and never writes it silently — the declaration stays self-describing.
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
