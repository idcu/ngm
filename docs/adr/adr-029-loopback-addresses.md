# ADR-029：把**回环**作为仓库地址收下（且只收一类）

- 状态：已定**并已实施**（2026-10-13，v0.83）
- 编号：本决策最初写成 ADR-028，与并行的「直接依赖边写进 ngm.lock」（[ADR-028](./adr-028-dependency-graph-in-lock.md)）撞号 ⇒ 改为 **029**（v0.83 复盘 §1 记了这次覆盖事故）
- 相关：[ADR-008 archiveDigest](./adr-008-archive-digest.md)（身份与内容的分工）·
  [ADR-009 供应链策略](./adr-009-supply-chain-policy.md)（`net:` 门禁）·
  [ADR-014 自证文件签名](./adr-014-self-report-signatures.md)（"谁说的"与"是不是真的"）

## 背景

`ngm` 用 **host + path** 标识一个仓库（canonical 形式，见 `docs/architecture/dependency-resolution.md`）。
主机名校验一直要求"含点"，并且只认三种短写（`github:` / `gitee:` / `gitlab:`）——
这条规则的本意是**反笔误**：`githubb:org/repo` 不该被当成一个域名。

v0.80 想在没有外网的机器上跑 `subPath` 端到端（这台机器连不上 GitHub，
只有 Gitee 可达），于是试了本机 `git daemon`。实测撞到三件事：

| # | 事实 | 性质 |
|---|------|------|
| 1 | `git://localhost/mine/jsonkit` 被拒：`unknown host "localhost"` | **本 ADR 处理的那一件** |
| 2 | `https://192.168.1.10/org/repo` **本来就允许**（host 含点即可） | 复核结论：内网 IP / 内网域名从来不是问题 |
| 3 | `git.defaultProtocol` 只允许 `{ssh, https}` ⇒ `git://` 说不出口 | 另一条独立决定，见"代价与边界" |

也就是说：被拒的**只有单标签主机名**这一类，而 `localhost` 是其中唯一
有正当理由的一个——而且它与 `127.0.0.1` 的行为不一致（后者一直能用）：
`https://127.0.0.1/x/y` 可以，`https://localhost/x/y` 不行。**同一件事两种结论**。

## 决定

**收下回环，且只收 `localhost` 与 `127.0.0.1` 这两个字面量**：

- `git://localhost/org/repo` · `https://localhost/org/repo` · `localhost/org/repo` ·
  `localhost:org/repo` —— 四种写法归一到同一个 canonical `localhost/org/repo`；
- 其余单标签主机名（`myserver`、`git`、`local`）**照旧拒绝**，并且报错里
  把三条出路一次说清：短写、完整域名、或回环；
- `[::1]` **不收**（它在 URL 形式那一站就被挡下，见"代价与边界"）。

## 理由

1. **不一致比严格更坏**：`127.0.0.1` 能用而 `localhost` 不能用，是同一件事的两种结论。
   要么两个都收，要么两个都不收；而"都不收"会让"在本机跑一遍"这件事彻底不可做。
2. **反笔误的价值没有被削弱**：`localhost` 不是一个会写错的域名；被它挡住的是
   **唯一一类本机地址**。九成的笔误（`githubb:`、`gilab:`）仍然被拒。
3. **它换来的是"没有外网也能走完整流水线"**：本地 git 服务器 / 本地镜像 /
   离线开发环境（本机正是这个处境：GitHub 不可达）。测试与 dogfood 都吃这一条。
4. **身份仍然是稳定的**：回环地址不依赖 DNS，也不随网络环境变化；
   `archiveDigest` 仍然把"内容"钉死在 commit 上（ADR-008），
   地址放宽不会让"锁定的内容"变得可替换。

## 代价与边界（写清楚，而不是留着让人猜）

| 边界 | 为什么 |
|------|--------|
| **只收两个字面量**（`localhost` / `127.0.0.1`） | 拒绝面保持窄：其余单标签主机名照旧是笔误 |
| `[::1]` 不收 | IPv6 在 URL 形式的主机处理那一站就被挡下（`git://[::1]/…` 走不到主机校验）。"声称支持却根本走不到"比不收更坏——要收它得先改那一站，留给需要它的 ADR |
| 端口仍然拒绝 | 身份是 host + path；`(host, path, port)` 会让同一个仓库有两个身份。本地 `git daemon` 用默认端口 9418 即可 |
| `git://` **仍然说不出口** | `git.defaultProtocol` 只允许 `{ssh, https}`：`git://` 无认证、无加密，全局拒绝是**有意**的。因此本 ADR 的收益不是"本机 daemon 直接可用"，而是"本机 https/ssh 服务可用 + 地址规则自洽" |
| `net:` 门禁不动 | 地址能不能**写**（本 ADR）与网络能不能**去**（ADR-009）是两个独立决定，用户仍须显式 `allow` |
| 局域网 IP / 内网域名 | **一直是允许的**（host 含点即可），本 ADR 没有改它，只是把它写进文档免得被误读 |

## 后果

- `validateHost` 先判回环；`splitHostPath` 的裸 canonical 分支同样收 `localhost/…`，
  保证"同一仓库的多种写法归一"（判据里有断言）。
- 自检器/文档的形式表补一行；`docs/architecture/dependency-resolution.md` 的表格同步。
- 判据：`internal/resolve/loopback_test.go`（收下的四种写法归一 + 继续拒绝的面 + 报错点名出路）。
- **本版另一处与地址无关的修正**（同一轮实测发现）：坏掉的全局配置此前被
  `newProjectEnv` 静默忽略（`cerr == nil`），于是 `defaultProtocol` 与
  `tokenEnvVars` 会悄悄退回默认——前者换协议、后者漏脱敏。现在坏配置直接 `exit 3`
  并点名文件与字段（见 v0.83 复盘）。
