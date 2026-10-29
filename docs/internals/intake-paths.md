# 三条接入路径（含实测输出）

> 这一页回答一个具体问题：**一个已经存在的项目，第一次用 ngm 要付多少代价。**
> 三条路径按"代价从低到高"排列，每条都给出真实命令与真实输出
> （2026-10-29，Windows + git 2.x + node 24 + deno 1.45）。

---

## 路径 A：零改动（npm 项目，想先看看自己装了什么）

```bash
ngm adopt
```

实测输出（一个含 3 个包、其中一个来自 Git 的 `node_modules`）：

```
adopted 3 package(s): 1 with a git commit, 2 with registry integrity, 0 unknown
wrote ngm.proof.json (source: npm package-lock.json, coverage 1/3 traceable)
```

`ngm.proof.json` 里每个包带一个**与 `ngm verify` 同一套清单格式**的摘要
（同一个函数；普通文件一律记 `100644`，不看可执行位——见 [ADR-040](../adr/adr-040-adopt-proof-layer.md)），
来源按 `package-lock.json` 标注：

```json
{ "name": "@acme/core", "version": "2.1.0", "path": "node_modules/@acme/core",
  "digest": "sha256:cbd1501c1fee…", "files": 2, "bytes": 57,
  "provenance": { "kind": "git", "url": "https://github.com/acme/core.git",
                  "commit": "0123456789abcdef0123456789abcdef01234567" } }
```

它**不做**什么（也写进 `notes`）：不安装、不写 `node_modules`、不改任何 lock；
不判断代码是否安全；registry 条目的来源只有 `integrity`，ngm 不为它做来源解析。

### 真实 npm 树上的实测（2026-10-30）

上面那个夹具是手搭的。真正跑一次 `npm install is-number is-odd rimraf`（15 个包，
真实 lockfile v3，含 `.bin` 与菱形依赖）之后：

```
adopted 15 package(s): 0 with a git commit, 15 with registry integrity, 0 unknown
wrote ngm.proof.json (source: npm package-lock.json, coverage 0/15 traceable)
```

四个真实世界里必然出现、而夹具里没有的形状，都过了：

| 形状 | 结果 |
|---|---|
| `node_modules/.package-lock.json`（npm 的隐藏锁）与 `.bin/` | 都不算包（点前缀跳过）|
| **菱形依赖**：`is-number@7` 在顶层、`is-number@6` 在 `node_modules/is-odd/node_modules/` | **各成一条记录**，不同版本不合并 |
| `node_modules/rimraf/bin.js`（POSIX 上是 `100755`） | 仍记 `100644`——可执行位跨系统不可观测，记进去会让同一棵树在 Windows 与 Linux 上给出不同摘要 |
| 只改 mtime、内容不变 | 摘要不变（内容寻址，不是时间戳）|

**代价**：0 行改动。**收益**：一份可 diff、可归档的内容清单。

CI 里不需要自己拼命令——仓库根的 `action.yml` 有三个 mode，其中两个属于这条路径：

```yaml
- uses: actions/checkout@v4
- run: echo ok          # 先把 ngm 装到 PATH
- uses: ./
  with:
    mode: adopt-check    # 与上次的 ngm.proof.json 比较；有变化即失败
    proof-file: ngm.proof.json
```

写下的证明**可以被复核**（否则它就只是一次性快照，而"没人再读的审计结论"
与"从没做过审计"是同一件事）：

```bash
ngm adopt --check=ngm.proof.json
```

实测（同一棵树）：`unchanged: 3 package(s) match the proof in ngm.proof.json`，exit 0。
改掉一个依赖里的一个字节之后再跑：

```
✗ node_modules/left-pad — content changed
    was: sha256:cd040c09…
    now: sha256:28d83c2d…

1 difference(s) against ngm.proof.json — this tree is not the one the proof describes
  → if the change is intended, run `ngm adopt` again to record a new baseline
```

exit **2**。它按**路径**比较而不是按包名——`node_modules/a/node_modules/b` 与
`node_modules/b` 可以是不同版本，按名字比会把"嵌套依赖被换了"静默吞掉。

**代价的另一面**：复核的仍然是**快照**。它回答"字节变了没有"，不回答
"这些字节绑在哪个 commit 上"——后者需要 `ngm.lock`，也就是路径 B。

---

## 路径 B：只把 Git 依赖交给 ngm（pnpm / yarn / npm 项目都适用）

两套目录、两套 lock，互不覆盖：

| | 你的安装器管 | ngm 管 |
|---|---|---|
| 目录 | `node_modules/` | `ngm.vendor/` |
| lock | `pnpm-lock.yaml` / `package-lock.json` | `ngm.lock` |
| 对象 | registry 包 | **仅 Git 依赖** |

真实项目里的 Git 依赖是个位数（私有 fork、必须追到 commit 的上游），所以接入就是**几行声明**：

```bash
ngm add github:acme/core@v2.1.0 --ref-type=tag
ngm install
ngm verify
```

实测（本地 mirror 已就绪，全程离线）：

```
✓ github:acme/lib@96d7009dd35db66236c30ef2b01b12d0e3b5a0e0 (commit) → 96d7009 — match
verified 1 dependency(ies): 1 ok (exit 0)
```

这条路径给出的是**持续保证**：`refType` + commit + `archiveDigest` 绑在一起，
`install` 尊重 lock、`update` 是唯一会移动它的命令。
`ngm adopt` 对 pnpm 的 lock **不解析也不打算解析**（[ADR-040](../adr/adr-040-adopt-proof-layer.md)）；
pnpm 项目要 Git 来源，走的就是这两行 `ngm add`。

---

## 路径 C：让结果进 CI（PR 上看得见）

```bash
ngm verify --format=github
```

实测（vendor 被改了一个字节之后）：

```
::error title=ngm: github:acme/lib landing (critical)::github:acme/lib (commit@96d7009):
index.js: size differs (30 vs 20 bytes) — next: do NOT build from this tree; investigate
how the bytes changed, then re-run `ngm install` to restore provable content
```

退出码 **2**。想要 Code Scanning 而不是注解：

```bash
ngm verify --format=sarif > ngm.sarif
```

仓库根的 `action.yml` 把这条命令包成一个 composite action（**不替用户下载 ngm**：
用哪个版本、从哪校验，是使用者的一次决定）。

---

## 三条路径各自**不能**做什么

| 路径 | 能回答 | 不能回答 |
|---|---|---|
| A `adopt` | 磁盘上是哪些字节；其中几个能追到 commit | 这些字节明天会不会变（没有 lock 可比） |
| B `ngm add` + `install` | 这个 commit 的内容是不是锁定的那一份；上游有没有换过 | 这份内容**是否安全**（那是另一条轴） |
| C `--format` | 失败项怎么进别人的流水线 | 谁来处置（策略是消费方的事） |

> 一句话：ngm 证明的是**内容与 commit 的对应关系**，不是"代码无害"。
> 后者需要 Socket 那类检测工具，两者互补而非替代。
