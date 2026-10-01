# ADR-015：commit 型依赖不做 ref 解析

> **状态**：已定（v0.5 A 组）。**只追加**：ADR-010 的"ref 问远端、对象按需取"不变，
> 本条把它的边界再收一次——**有些 ref 连问都不必问**。

---

## 背景

v0.1 复盘 §3.3 的结论一直没有被推翻过：**verify 的成本几乎全在 git 子进程启动上**
（哈希与 I/O 都很便宜：100 MiB 的单次 digest 重放 452ms）。v0.5 复测里在线 verify
仍然"贴着 3s 目标线"（三次采样 2.201 / 3.068 / 2.556s）。

但在此之前，"一次 verify 到底起了几个 git"只能靠**读代码数**——于是任何"少起一个进程"
的改动都只能靠掐表验证，而掐表测的是噪声加信号。v0.5 先补了仪器（`git.SpawnCount()`，
见 `internal/git/spawn.go`），再动手。第一次用它就拿到了下面这张清单。

### 实测：一次在线 verify 的子进程清单（本机，3 个依赖）

```
在线 verify，3 个 commit 依赖：6 次（2.00 次/依赖）
在线 verify，3 个 tag   依赖：12 次（4.00 次/依赖）
```

单次成本（同机实测，裸仓库）：

| 调用 | 成本 | 用途 |
|------|------|------|
| `git config --get remote.origin.url` | **30.6 ms** | 取 mirror 的远端地址 |
| `git ls-remote`（本地裸仓库） | **77.8 ms** | tag / branch 解析 |
| `git ls-tree` | **36.4 ms** | digest 重放的清单 |

按每个 tag 依赖 4 次计约 183 ms；100 依赖 ÷ 8 并发 ≈ 2.2s，与实测中位 2.56s 基本吻合
（差的那些是网络与磁盘的抖动）。**这条模型解释得通，因此可以拿它预测改动收益。**

### 浪费在哪

`RemoteRefResolver`（`cmd/ngm/env.go`）在线模式下**无条件**先取一次 mirror 的
`remote.origin.url`，再把地址交给 `resolve.ResolveRef`：

```go
url, _ := git.MirrorRemoteURL(...)          // ← 一个子进程
return resolve.ResolveRef(..., GitURL: url)
```

而 `ResolveRef` 对 commit 型走的是：

```go
case RefTypeCommit:
	return normalizeCommitRef(ref)          // ← 纯字符串处理，不看 GitURL
```

也就是说：**每个 commit 型依赖白起一个 git，而它的结果被丢掉了**。

---

## 决策

**`refType=commit` 的依赖不做 ref 解析：ngm 不为此启动任何 git 子进程。**

- 在线模式下，commit 型短路，直接返回规范化后的 hash（0 次 spawn）。
- tag / branch 不变（仍要问远端，ADR-010 不动）。
- 离线模式本来就走本地 mirror，不受影响。

---

## 理由

**1. 内容寻址的对象不需要远端广播的 refs。** commit 自己就是答案。

**2. `ls-remote` 根本回答不了"远端是否还有这个 commit"。** 它列的是 **ref**，
不是对象。一个未被任何 ref 指向的 commit，即使远端还留着，`ls-remote` 也不会提它；
反过来，它被删了也一样看不出来。所以这里**没有**任何能力可以"因为跳过而失去"。

**3. 现有行为本来就没在查那件事。** 改造前的那次 `config --get` 只是取地址，
而 commit 分支拿到地址后连用都没用——因此这次改动是**行为等价**的，
不是"用一点检出能力换性能"。

**4. 对象是否存在，由必须读它的那一步负责。** ADR-010 的按需取物已经在做这件事：
digest 重放读不到对象时报 `errCommitMissing`（"commit 不在本地 mirror 里"），
那是**有证据**的结论；而在这里"顺便问一下远端"只会得到一个既非充分也非必要的信号。

**5. 零成本这件事可以被断言。** `TestV05RefResolutionSpawns` 断言 commit 型 **0** 次、
tag 型 **至少 1** 次——两条互为对照，缺一条就可能被"计数器写坏了"或"什么都没变"蒙过去。

---

## 实测结果（2026-10-01，本机 Windows 10 / 8 逻辑核）

| 依赖类型 | 改造前 | 改造后 |
|----------|--------|--------|
| commit 型 | 3.00 次/依赖 | **2.00 次/依赖** |
| tag 型 | 4.00 次/依赖 | 4.00 次/依赖（不受影响） |

commit 型剩下的 2 次是**每个依赖都要付**的 digest 重放（`ls-tree` + `cat-file --batch`），
与 refType 无关。

> **本 ADR 不改善 100 依赖的 tag 基准**——那正是 metrics 里"贴着 3s 线"的那个场景。
> 按上面的成本表，tag 型剩下的第 4 次是 `config --get`（30.6 ms，约占每依赖 spawn 成本的 17%）：
> 它在 mirror 的 `config` 文件里是明文，理论上可以**直接读文件**而不起进程。
> 本轮**不做**，理由与下一步都记在 [v0.5 计划 A4](../development/v0.5-plan.md)：
> 手写 git config 解析要处理 include / 引号 / 多值等情形，必须带保守回退，收益 17% ——
> 值得做，但要以"形状不符就回退子进程"为前提，且要有等价性断言。

---

## 后果

**正面**

- commit 型依赖的 verify 成本下降约 1/3（3 → 2 次 spawn，按上表约 183ms → ~110ms/依赖）。
- 少一次**可能失败**的调用：mirror 缺失或 config 损坏时不再走"降级分支"，
  逻辑更直白（commit 型根本不看 mirror）。
- 多了一台仪器（`git.SpawnCount()`）：之后的取数改动都可以先看次数、再看时间。

**负面 / 代价**

- commit 型的 ref 检查从此**只有**"lock 里的 commit 等于声明的 commit"这一条
  （改造前也仅此一条），真正的把关是 digest 重放。这一点必须写在文档里，
  否则读者会以为 ref 检查对 commit 型也做了远端核对。
- 计数器是进程内的全局状态：测试并行时会被互相影响，因此断言必须
  `ResetSpawnCount()` → 单次调用 → 读数的模式，不能跨用例累计。

---

## 未采纳的替代方案

| 方案 | 为什么不做 |
|------|-----------|
| 提高依赖级并发度 | 实测 8 已是拐点（4→8 收益明显，8→16 无收益，见 `DefaultConcurrency` 注释） |
| 合并同仓库的多次 git 调用 | v0.1 复盘已撤回：该建议成立于"多依赖共享同一仓库"，而基准是 100 个独立仓库 |
| 把 mirror 的远端地址缓存起来 | 100 个依赖 = 100 个不同 mirror，缓存无收益 |
| 直接读 mirror 的 config 取地址 | 见上"实测结果"：收益真实（17%）但需保守回退，记入 v0.5 A4 待做 |

---

## 相关文档

- [ADR-010 在线 verify 的取数策略](./adr-010-online-verify-fetch-policy.md)（本条是它边界的一次收紧）
- [v0.1 复盘 §3.3](../development/v0.1-retrospective.md)（"成本在 spawn"的原始证据）
- [v0.5 计划 A 组](../development/v0.5-plan.md) / [v0.5 复核](../development/v0.5-review.md)
- [健康度指标](../internals/metrics.md)（性能目标与采样）
