# ngm

> Git-first Dependency Provenance Layer

ngm is a Node.js / Deno package manager with **provable** Git dependency tracking. Every dependency is locked to a specific commit, content-addressed by an `archiveDigest` (SHA-256 over the canonical file listing), and verifiable on demand via `ngm verify`.

Status: **v0.1 … v0.8 delivered in source** (`v0.1.0` … `v0.4.0` tagged and published, each with six
platform binaries plus `SHA256SUMS`). `v0.5` was the convergence and delivery pass; `v0.6` made the
remaining conclusions *decidable* (a git spawn budget as a CI gate, real-world reproducibility forms,
measured content-store growth); `v0.7` made the content store's footprint **visible**
(`ngm store usage`) and its residues reclaimable (`ngm store prune`); and `v0.8` replaced the layer-2
layout with a **blob pool + tree manifests**, cutting the measured cost of 12 commits from
**20.0× the real source delta to 0.6×** (`1.88 MiB → 59.9 KiB`).
Per-version plans and retrospectives live in [`docs/development/`](./docs/development/README.md).

All eight tags `v0.1.0` … `v0.8.0` now exist (the last four were caught-up on 2026-10-02 after
being delivered-but-untagged — the same mistake the release checklist's step 0 was written to
prevent, made once for `v0.2`~`v0.4` and then again for four more versions). That pairing is no
longer left to memory: `scripts/check-release-status.sh` (CI job `release-status`) requires
**a retrospective ⇔ a tag**. See [`docs/README.md` §发布状态](./docs/README.md#发布状态) and
[`docs/development/v0.8-retrospective.md`](./docs/development/v0.8-retrospective.md).
GitHub releases for `v0.5.0` … `v0.8.0` appear once the mirror forwards the tags; their Gitee
attachments still need the manual step.
Prebuilt binaries for `v0.1.0` … `v0.4.0` are available from **both** sources with identical
`SHA256SUMS` (verified byte-for-byte); see
[`docs/development/README.md`](./docs/development/README.md) for the release checklist and
[`docs/guides/installation.md`](./docs/guides/installation.md) for which version is downloadable where.

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
| v0.2 | OSV/`audit`, policy engine (minimumReleaseAge / allowlist / postInstallPolicy), `why` / `tree` / `outdated`, tsc·deno·postcss adapters, `install --frozen-lockfile` / `--offline` | delivered, untagged |
| v0.3 | wasm adapter, Deno sandbox, enforced `permissions`, Vite / esbuild / Deno / Webpack scaffolds, mappings sub-paths | delivered, untagged |
| v0.4 | `ngm typedecl`, `verify --signatures` / `--require-signed`, mechanical "declared but not wired" config check | delivered, untagged |

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

**All rights reserved.** No license is granted. This repository is published for viewing and
evaluation; copying, modifying, redistributing or reusing it — in whole or in part — requires
prior written permission from the author. A license may be added later; until then the above
applies.
