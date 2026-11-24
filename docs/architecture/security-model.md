# 安全模型

> **结论**：ngm 对"不可信代码"的答案是默认拒绝、显式授权、边界可审计。
> 所有会触网、起子进程、读凭证的地方都收束到少数几个施加点；每个拒绝都是 exit 3 且**未发起动作**。

---

## 威胁与边界

| 威胁 | 防护 |
|------|------|
| 恶意 Git 仓库的 postinstall 脚本 | `postInstallPolicy: deny` 默认；`allow` 也只在 Deno 沙箱内 |
| typosquatting / 未授权传递依赖 | `allowlistRepos` / `allowedGitHosts` 对传递依赖同样生效 |
| 供应链投毒 | OSV + `minimumReleaseAge`，绑 commit 而非 tag/branch |
| 内容篡改 / ref 漂移 | digest 本地重放 + verify |
| 参数被环境偷换 | 子进程环境剔除注入变量 + 远端地址形状校验 + lock 路径安全校验 |
| 凭证泄露 | 只透传不落盘；输出脱敏；沙箱内不可读 |

**边界**："无 registry"不等于"去信任"——仍需面对 Git host、凭证、force push。
沙箱限制的是文件系统与网络两个维度，不是"跑不可信代码也安全"的开关。

---

## 权限模型（借鉴 Deno 的 default-deny）

`~/.ngm/config.json` 的 `permissions` 段默认档位：`net:` 与 `run:` 需要配置，未授权 action 前即失败。

| 权限 | 施加点 | 拒绝表现 |
|------|--------|---------|
| `net:<host>` | mirror 的 clone/fetch 唯一出口、`resolve` 的 ls-remote、ngm 自己的 HTTP 出口（OSV 查询，主机取实际端点） | exit 3，未发起网络请求 |
| `run:git` | git 包唯一的子进程构造点，三个出口共用 | exit 3，git 从未启动 |
| `env:<NAME>` | git 子进程、引擎子进程、Deno 沙箱、`ngm run` 的脚本子进程 | 变量从子进程环境剔除，ngm 不读取其值 |
| `read:` / `write:` | 只在沙箱内施加：按依赖子树授予读，写恒不授予 | 沙箱外不施加（读项目、写 vendor/lock 本就是 ngm 职能） |
| `run:<引擎>` | `Catalog.Probe`（探测与执行同一路径），排在能力与可用性之后 | exit 3，引擎从未启动；没装仍是 exit 5 |
| `run:script` | 项目脚本的独立闸门（默认拒绝） | exit 3 |

**两套闸门不要混**：`ngm run` 执行的是你自己写的脚本，在沙箱**之外**运行，用独立的 `run:script` 授权；
install 期依赖的 postinstall 是别人的代码，由 `supplyChain.postInstallPolicy` 控制，即使 allow 也只在沙箱内。
前者不能用后者放行，反之亦然。

**规则**：deny 优先于 allow；匹配精确（`net:github.com` ≠ `net:api.github.com`，刻意不做通配）；
`ngm config validate` 会检查命名空间本身，写错直接 exit 3，不让一整份列表被静默跳过。

### `run:` 的身份判定

判定对象是**解析后的真实文件**，不是声明的程序名：

1. 用与执行同一个解析器解析（项目相对路径 → 项目目录；裸名字 → PATH，再回退 `node_modules/.bin`），展开符号链接
2. 名字授权（`run:git`）与路径授权（`run:./tools/git`）分两个空间，永不互串
3. 顺序：deny（任一命中）> 路径 allow > 名字 allow > 默认档位
4. 默认档位只对"裸名字 + 解析到项目外"生效——`run:git` 不覆盖仓库自带的 `./git` 或 PATH 解析进项目的同名文件
5. 项目内可执行文件只能用**路径授权**放行；路径形式声明不享受默认档位
6. wasm 例外：身份名是模块文件名（`run:engine.wasm`），不解析路径——WASM 运行时自带能力边界

**行为**：探测与执行走同一闸门，未授权引擎显示 `not probed`；项目内引擎的提示给出可粘贴的 `run:./<path>`。

---

## 施加点：不可信输入不碰信任根

以下校验无条件生效，不新增权限：

- **git 子进程环境**：`GIT_SSH_COMMAND` / `GIT_ASKPASS` / `GIT_CONFIG_*` / 仓库定位变量（`GIT_DIR` / `GIT_WORK_TREE` 等）
  一律剔除；保留凭证本身与用户自己的 `~/.gitconfig`（签名校验因此读用户自己的密钥配置，见 ADR 汇总表 ADR-014）
- **远端地址形状**：拒绝 `-` 开头、拒绝 `helper::<address>`（`ext::` / `fd::`）；只接受 https/http/ssh/git/file、
  scp 形式与本地路径。可疑形状仍按 scp host 计入 `net:` 门禁——绝不因形状可疑就当本地路径放行而触网绕过门禁
- **lock 路径字段**：`vendorPath` / `subPath` 必须是 vendor 根内的安全相对路径；拼接后再判一次，
  越界条目不参与任何 `Stat` / 哈希 / 脚本执行；清旧 vendor 与 `globalPath` 先解析符号链接
- **输出脱敏**：内嵌凭据的 remote（`https://user:token@host/...`）允许出现在配置里，不允许出现在输出里；
  脱敏在统一出口完成，新增命令自动继承
- **默认非交互**：注入 `GIT_TERMINAL_PROMPT=0`，无 tty 时不挂起，凭证不足立即失败
- **探测与执行同路**：可用性判定、`run:<engine>` 闸门、版本 spawn 收在一处；`--dry-run` 走不 spawn 的 argv 预检

---

## sandbox 模式

**唯一运行他人代码的地方**：`verify --sandbox` 执行依赖自带的 `verify.js`；`postInstallPolicy: allow` 执行
`postinstall.js`；`audit --hook` 执行团队自己的策略脚本（报告 JSON 从 stdin 进入，读权限给项目目录）。

| 用途 | 触发 | 判定 |
|------|------|------|
| 依赖自证 | `verify --sandbox` | 脚本只能否决：失败 → exit 2，`--allow-drift` 无效；成功不改变 ngm 结论 |
| 依赖安装步骤 | `postInstallPolicy: allow` | 失败 → exit 2 |
| 团队策略 | `audit --hook=<script>` | 否决 → exit 1（"有需要处理的东西"，与"发现漏洞"同类） |

**core 做决策，Deno 只执行被交给它的那一个脚本**。脚本输出照展示但不解析、不采信。

### 沙箱的硬边界

- **模块加载要单独关**：`--deny-net` 管 fetch，`import "npm:..."` 走运行时自己的 registry 客户端；
  因此还必须显式给 `--no-npm` / `--no-config`（工作目录里由上游控制的 `deno.json`）/ `--no-lock`
- **显式拒绝面**：`--deny-sys` / `--deny-ffi` / `--deny-hrtime` / `--no-remote` 逐条写进 argv——边界要能被读日志的人看见
- **`run:` 从不授予**：`--allow-run` 允许的进程不受 Deno 权限约束，实测能借它读到被禁读的文件；
  文件系统与网络两条约束会被一次派生同时绕开，所以类型层面就没有"可执行程序"这一项
- **写从不授予**：vendor 是可证明的，能改写它的钩子会让 digest 失效；需要写盘的钩子（编译原生模块）不被支持
- **最小环境**：子进程只拿运行时必需变量加显式授权的变量；`deny: ["env:X"]` 永远优先
- **内存有界**：三个捕获点（沙箱、subprocess 引擎、wasm）的 stdout/stderr 走 boundbuf（1 MiB，超出丢弃并计数）；
  产物通道例外（64 MiB，撞界即失败，绝不交付截断产物）
- **超时**是唯一的 CPU/时间限制（默认 30 秒，超时与"脚本报错"分别报告）

**未安装 Deno**：有脚本要跑时 exit 5 + 安装提示，不静默降级；没有脚本要跑时不需要 Deno。
版本门槛按能力判定（一次最小执行探测），不写死版本号。**没有内存 / CPU 上限**：Deno 没有稳定的跨平台开关。

---

## 凭证

**原则**：ngm 本身不读取 Git 凭证，除非必要。推荐顺序：SSH agent → Git credential helper →
环境变量 → `~/.ngm/config.json`（权限 600）。不要：在 `ngm.json` / lock 里写明文 token，或把 token 传给引擎。

已落实的保证：

- 凭证只透传、不落盘：`os.Environ()` 原样交给 git 子进程；mirror 的 `.git/config` 不含凭证，
  token 不出现在输出、lock 或 `ngm.json` 中
- 输出脱敏、仓库定位变量剔除、默认非交互（见上节）
- 引擎 `command` 不经过 shell（见[引擎 adapter](./engine-adapter.md)）

---

## verify 的安全语义

退出码定义唯一出处是[可观测性](./observability.md)，安全相关三条：

- 码 2（完整性失败）**必须**阻断构建，`--allow-drift` 无效
- 码 1（非预期漂移）默认阻断，`--allow-drift` 可降级为 0
- 码 0 不等于"一切正常"：branch 前进也是 0，严格拦下需 `--strict`

`install --frozen-lockfile` 管"装什么"（不解析新 ref、不改 lock，不禁止网络）；
`install --offline` 管"怎么拿"（禁一切网络，未命中 exit 4）；组合即完全离线可复现安装。

## 相关文档

- [架构总览](./overview.md) · [供应链防护](./supply-chain.md) · [引擎 adapter](./engine-adapter.md)
- [可观测性](./observability.md) · [ADR 汇总表](../adr/README.md)
