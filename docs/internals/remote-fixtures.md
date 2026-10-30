# 远端夹具：三个真实的 Gitee 仓库

> 这三仓存在的原因只有一条：**有些东西本地 mirror 假不出来。**
> 它们不是 demo，也不是集成测试的全部——它们是"宿主集成"那一层的唯一证据。

仓库地址：

| 仓库 | 用途 |
|------|------|
| `https://gitee.com/idcu/ngm-fixture-basic` | 两种 tag 语义 + 分叉分支 + CRLF/可执行位/symlink |
| `https://gitee.com/idcu/ngm-fixture-monorepo` | 一仓两个模块目录，各带 `ngm.json` |
| `https://gitee.com/idcu/ngm-fixture-content` | 病态内容：大小写对、BOM、非 ASCII 文件名、空文件、深层路径 |

---

## 为什么需要它们（以及为什么只有这三件事）

| 只有真实远端能证明 | 本地为什么不行 |
|---|---|
| annotated tag 在 `ls-remote` 里是**两行**（`refs/tags/x` 与 `refs/tags/x^{}`），ngm 必须锁到**剥开后**的 commit | 手工造 mirror 时你可以直接写 refs，于是"广播长什么样"从来没被测过 |
| 真实 `git clone`（pack 传输、auth、redirect） | 本地是文件路径复制，跳过了传输层 |
| `net:<host>` 门禁在**出网之前**拒绝 | 本地路径根本不经过这条检查 |

反过来，**漂移三态、摘要重放、落地完整性**这些不上远端：本地夹具（`scUpstream` 一类）
可以反复破坏与重建，远端不能。把行为断言堆到远端，换来的是"测试变慢"和"夹具腐坏后难以归因"。

---

## 判据长什么样

| 判据 | 证明什么 | 跑法 |
|---|---|---|
| `TestV112FixtureGeneratorReproducesTheSameSHAs` | 生成器是**确定**的：任何机器重跑得到同一批 SHA | 默认跑（离线，~6 s）|
| `TestV112FixtureContentIsWhatTheDocsClaim` | 三个仓库真的有它们名字里承诺的形状 | 默认跑（离线，~3 s）|
| `TestNetFixturesHaveNotBeenRewritten` | 远端还是当初那三个（腐坏守卫） | `NGM_NET_FIXTURES=1` |
| `TestNetFixtureAnnotatedTagPeelsToTheCommit` | 真宿主上 annotated tag 被剥到正确的 commit | 同上 |
| `TestNetFixtureLightweightTagResolvesToItsCommit` | lightweight tag 走另一条路径且结果正确 | 同上 |
| `TestNetFixtureNetworkGateRefusesBeforeConnecting` | `net:gitee.com` 被拒时**没有留下任何 mirror** | 同上 |

```bash
# 本机（需要 gitee 可达）
NGM_NET_FIXTURES=1 go test ./cmd/ngm -run 'TestNetFixture' -v
```

默认 `t.Skip`。这是仓库纪律的直接后果：**测试不依赖公网**。

---

## 契约：记录在案的 SHA

生成器固定了作者、时间、消息与内容，所以 SHA 是确定的。当前的记录：

```
ngm-fixture-basic
  refs/heads/main           725cb2ebfb3da6209371a37ebe47677b2821cd70
  refs/heads/legacy         6530ad09ab1f3f7065e8df34154f981bfc0622e9
  refs/tags/v1.0.0          14ae2ef6731a35a561677eb4de569beaed878450   （tag 对象）
  refs/tags/v1.0.0^{}       05701ae7e09d2716cf02824d4dfa56c4595392f5   （剥开后的 commit）
  refs/tags/v1.1.0-light    725cb2ebfb3da6209371a37ebe47677b2821cd70   （lightweight）

ngm-fixture-monorepo
  refs/heads/main                   a17e0c3b084a6b396c1bf3ef72833646faf2e056
  refs/tags/packages-a-v1.0.0       03e8dfce08ac4fca3f9cf449c9f6881f4c3916b2
  refs/tags/packages-a-v1.0.0^{}    978c1d43f504e23817731c92ff5aee9630376348
  refs/tags/packages-b-v1.0.0       75d1cc68583fba41ed41fb406b5b8aa16de5c879
  refs/tags/packages-b-v1.0.0^{}    a17e0c3b084a6b396c1bf3ef72833646faf2e056

ngm-fixture-content
  refs/heads/main           8d2b7a6cf5fb9c2e16960632bc8e9f41987ba827
  refs/tags/v1.0.0          7a97e7512d50ddff20a64c1f51db3e8f4b9f4581
  refs/tags/v1.0.0^{}       8d2b7a6cf5fb9c2e16960632bc8e9f41987ba827
```

**它们只读**。一个被移动过的 tag 仍然"能用"，只是下游断言开始以"解析出来的 commit 不对"
失败——那听起来像 ngm 的 bug。所以 `TestNetFixturesHaveNotBeenRewritten` 先跑；
它红了就说明**夹具**变了，不是产品变了。

真要改夹具：重新生成 → 推送 → **同一次提交里**同时更新
`cmd/ngm/fixtures_remote_test.go` 的 `expectedFixtureRefs` 和这一页。
两处不一致，腐坏守卫就形同虚设。

---

## 重新生成与推送（人工一次）

```bash
# 1) 生成到某个目录（不会写任何远端）
NGM_FIXTURES_OUT=/tmp/ngm-fixtures go test ./cmd/ngm -run TestV112PushInstructions -v

# 2) 逐个推送（仓库已存在且为空；--mirror 连 refs/heads 与 refs/tags 一起推）
git -C /tmp/ngm-fixtures/ngm-fixture-basic     push --mirror https://gitee.com/idcu/ngm-fixture-basic.git
git -C /tmp/ngm-fixtures/ngm-fixture-monorepo  push --mirror https://gitee.com/idcu/ngm-fixture-monorepo.git
git -C /tmp/ngm-fixtures/ngm-fixture-content   push --mirror https://gitee.com/idcu/ngm-fixture-content.git

# 3) 复核
NGM_NET_FIXTURES=1 go test ./cmd/ngm -run TestNetFixturesHaveNotBeenRewritten -v
```

生成器用**底层命令**（`hash-object` / `update-index` / `write-tree` / `commit-tree` /
`update-ref`）把内容直接写进树里，而不是先落到文件系统再 `git add`。这不是风格问题：

- `Foo.txt` 与 `foo.txt` **无法**共存于 Windows 的文件系统；
- symlink 与 `100755` 在 Windows 上默认也创建不了。

写进树里，三个平台就都能生成**同一批 SHA**。

---

## 这三仓**不**证明什么

- **不**证明漂移三态（本地覆盖，且远端不该被反复破坏）；
- **不**证明与真实 registry 的互操作（ngm 明确不做 registry）；
- **不**证明性能（远端 clone 受网络支配，不能当基准）；
- **不**证明"有人在用"——这三个仓库是**我们的**夹具，属于作者，不是用户。

最后一条值得单独写：把自家夹具跑绿，不等于产品被采用。这两件事在本项目里已经被
混淆过一次（100 个版本、0 个外部用户），所以这一页的开头就写明它们的用途边界。
