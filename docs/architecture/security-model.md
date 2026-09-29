# 安全模型

## 核心问题

> 依赖管理器本身也可能成为攻击面。ngm 如何处理"不可信代码"？

---

## 威胁模型

| 威胁 | 描述 | ngm 的防护 |
|------|------|-----------|
| 恶意 Git 仓库 | 依赖的 postinstall 脚本执行恶意代码 | `postInstallPolicy: deny` |
| 传递依赖引入未授权仓库 | 攻击者诱导引入近似名/未审批仓库 | allowlistRepos 对传递依赖同样生效 |
| typosquatting | 近似名称的恶意仓库 | allowlistRepos |
| 供应链投毒 | 依赖被植入后门 | OSV.dev + minimumReleaseAge |
| 内容篡改/损坏 | 本地内容树被篡改或损坏 | archiveDigest 本地重放校验 |
| ref 漂移 | tag 重打 / branch 移动 | verify |
| token 泄露 | Git 凭证被恶意依赖读取 | 权限隔离 |

---

## 权限模型（借鉴 Deno）

Deno 的 default-deny 权限模型：

```bash
deno run \
  --allow-net=github.com \
  --allow-read=$NGM_VENDOR \
  --allow-run=git \
  --deny-run=npm \
  script.ts
```

ngm 把这套思路映射到自己的操作：

| ngm 操作 | 对应权限 | 默认 |
|---------|---------|------|
| 读项目文件 | `read:$PROJECT` | 允许 |
| 写 vendor/ | `write:$PROJECT/ngm.vendor` | 允许 |
| 写 lock file | `write:$PROJECT/ngm.lock` | 允许 |
| 网络（Git fetch） | `net:github.com,gitee.com` | 需配置 |
| 执行 git | `run:git` | 允许 |
| 执行引擎 | `run:esbuild,tsc,deno,postcss` | 需配置 |
| 读 Git 凭证 | `env:GITHUB_TOKEN` / `ssh-agent` | 见下方成熟度说明 |
| 执行依赖的 postinstall | `run:node` | **默认禁止** |

> **成熟度**：上表的 `允许 / 需配置 / 默认禁止` 是**权限模型的规划（v0.3）**，v0.1 尚未实现权限门禁。
> v0.1 的实际行为是：ngm **不读取、不解析、不缓存**任何 token，但会把环境变量**原样透传**给 `git`
> 子进程（`git` 自己要拿它做认证）。也就是说——**token 的安全边界由你的环境与 git 的凭证机制决定，
> 不是由 ngm 的权限系统保证**。已实现并可测试的保证见下文「v0.1 已落地的保证」。

---

## sandbox 模式（planned v0.3）

> **尚未实现**。v0.1 的 `ngm verify` 没有 `--sandbox`（flag 集只有 `--dir` / `--offline` / `--deep` /
> `--strict` / `--allow-drift` / `--json`），传入它会因用法错误得到 **`exit 3`，而不是下文写的 `exit 5`**。
> 本节描述的是目标设计。

```bash
ngm verify --sandbox
```

设计：

- core Go 做决策（哪些 ref 漂了、digest 是否匹配）
- 具体的验证逻辑（如运行依赖自带的校验脚本）在 Deno 沙箱中执行
- 沙箱只授予最小权限

### 为什么用 Deno 做沙箱

- Deno 原生支持 permission 参数
- 可限制 `--allow-read` 到具体目录
- 可限制 `--allow-net` 到具体 host
- 可限制 `--allow-run` 到具体可执行文件
- `--deny-run` 可显式拒绝危险操作

### 沙箱内可做的事

- 运行依赖的 `verify.js`（如果它提供了）
- 检查依赖的自述文件签名
- 执行用户自定义的 audit hook

### 沙箱内不可做的事

- 访问 `~/.ssh/`
- 访问 `~/.git-credentials`
- 运行 `rm -rf /`
- 访问网络（除非白名单）

### 未安装 Deno 时

`--sandbox` 需要 Deno：缺失时报错（exit 5）并提示安装，**不静默降级**为非沙箱执行（安全语义不允许隐式降级）。默认的 `ngm verify`（不带 `--sandbox`）不需要 Deno。

---

## token 与凭证管理

### 原则

**ngm 本身不应读取 Git 凭证，除非必要。**

### 推荐方式

1. **SSH agent**：`ssh-agent` 已加载 key，ngm 通过 `ssh` 协议 clone
2. **Git credential helper**：`git` 命令自己处理认证，ngm 不碰 token
3. **环境变量**：`GITHUB_TOKEN` / `GITLAB_TOKEN`，ngm 透传给 `git`
4. **配置文件**：`~/.ngm/config.json`，权限 600

### 不推荐

- 在 ngm.json 里写明文 token
- 在 lock file 里记录 token
- 把 token 传给引擎 adapter

### v0.1 已落地的保证

以下行为已实现并由测试锁定（`internal/git`、`internal/adapter`），是"原则"的可验证形式：

- **凭证只透传、不落盘**：`os.Environ()` 原样交给 `git` 子进程；ngm 不读取、不解析、不缓存任何 token。
  测试断言 mirror 的 `.git/config` 不含凭证，且 token 不出现在任何输出、lock 或 ngm.json 中。
- **输出脱敏**：错误消息与日志里的 `https://user:token@host/...` 被替换为 `https://***@host/...`。
  脱敏发生在 `internal/git` 的统一出口，因此后续新增的命令自动继承这条保证。
- **剔除仓库定位变量**：`GIT_DIR` / `GIT_WORK_TREE` / `GIT_INDEX_FILE` / `GIT_OBJECT_DIRECTORY` /
  `GIT_ALTERNATE_OBJECT_DIRECTORIES` / `GIT_COMMON_DIR` / `GIT_NAMESPACE` / `GIT_PREFIX`
  一律不传给子进程。否则用户 shell 里残留的 `GIT_DIR`（钩子脚本、`git worktree` 会话、
  复杂 CI 中常见）会让 mirror 操作落到**另一个**仓库——静默失效或数据错乱。
- **默认非交互**：注入 `GIT_TERMINAL_PROMPT=0`，无 tty 时不挂起；凭证不足时立即失败并给出配置提示。
- **清单不能注入命令**：引擎 `command` 从不经过 shell，按空白切分（允许引号包裹）后直接执行
  （见 [P4](../modules/p4-ecosystem.md)）；`ngm.engines.json` 随仓库进入 ngm，走 `sh -c`
  等于把任意命令执行权交给任何一个 PR。

---

## verify 的安全语义

### 退出码即策略

退出码定义唯一维护在[可观测性 · 退出码约定](./observability.md)（此处不再复制整表）。安全相关的只有三条：

- `2`（完整性失败）**必须**阻断构建——`--allow-drift` 对它无效
- `1`（非预期漂移）默认阻断，`--allow-drift` 可降级为 0
- `0` **不等于**"一切正常"：branch 前进（预期更新）也是 0，要严格拦下需 `--strict`

### `install --frozen-lockfile` / `install --offline`（planned v0.2）

> **尚未实现**。v0.1 的 `ngm install` 只有 `--dir` / `--digest` 两个 flag；下面三条命令是 v0.2 的目标形态。
> v0.1 的 `--offline` 只适用于 `ngm update` 与 `ngm verify`。

```bash
ngm install --frozen-lockfile            # 不解析新 ref、不改 lock；允许网络下载
ngm install --offline                    # 不访问网络，只用 content store / vendor；未命中即失败
ngm install --frozen-lockfile --offline  # 完全离线可复现安装（CI 首选，需预置 store/vendor）
```

- `--frozen-lockfile`：禁止解析新 ref、禁止修改 lock；lock 缺失或与 ngm.json 不一致 → exit 3。**它不禁止网络**——依赖未在 content store 命中时仍需下载
- `--offline`：禁止一切网络访问，本地资源未命中 → exit 4
- 两者组合即"完全离线可复现"：前提是 content store 或提交的 vendor 已就绪

---

## 诚实说明

1. **"无 registry"≠"去信任"**：仍需面对 Git host、凭证、force push
2. **sandbox 不是银弹**：Deno 沙箱只能限制 Deno 进程，C++ addon 可逃逸
3. **权限模型增加复杂度**：用户需要理解 `ngm.config.json` 的权限字段
4. **token 泄露风险始终存在**：最小权限 + 短期 token 是最佳实践

---

## 相关文档

- [信任模型](./trust-model.md)
- [运行时模型](./runtime-model.md)
- [供应链防护](./supply-chain.md)
- [ADR-007：Node 还是 Deno？](../adr/adr-007-runtime-node-deno.md)
