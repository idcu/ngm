# ADR-043：暂不生成 vendored 依赖的 `.bin` shim

- **状态**：已决定暂缓（2026-11-18）
- **范围**：`ngm run` / `ngm build` 的项目本地工具解析
- **背景**：2026-11-18 开发计划 D2 曾列"为声明了 `bin` 的 vendored Git 依赖生成
  `ngm.vendor/.bin/` shim"，目标是让纯 Git 依赖项目在没有 npm 的情况下也能把
  依赖里的 CLI 当命令用。

## 决策

**本轮不实现**。保留现状：ngm 只把既有的 `node_modules/.bin`（项目及祖先目录）
注入 `ngm run` 的子进程 PATH（`internal/runscript/runscript.go` 的 `BinDirs`），
不为 `ngm.vendor/` 里的依赖生成任何 shim。

## 理由

1. **它越过 ADR-042 明确写下的边界。** ADR-042 的"代价与边界"写着：
   "脚本看到的 `node_modules/.bin` 是既有的，ngm 不会为缺失的依赖补链"。
   生成 shim 等于把 ngm 从"内容证明层 + 脚本编排"推进"依赖的 CLI 安装器"，
   这是产品定位的改变，不是一个补丁。
2. **它从第三方内容里创建一个可执行面。** 当前 `ngm.vendor/` 的文件被 digest
   钉死、默认 hardlink、从不执行；生成 shim 会让"依赖声明的 `bin` 字段"变成
   一条 PATH 上可调用的命令。这与 ngm "安装不执行别人的代码"的安全承诺
   （ADR-009/012）方向相反，需要独立威胁模型，不能在一次 P2 任务里顺带做。
3. **收益场景尚未证实。** 真正需要它的项目必须同时满足：所有依赖是 Git 依赖、
   且这些依赖自带 CLI、且不用 npm。这个交集目前没有用户证据；
   `docs/internals/project-state.md` 记录的"最大风险是采用"仍然成立。
4. **替代路径已经存在。** 这类项目可以把 CLI 路径写进脚本（例如
   `"build": "node ./ngm.vendor/github.com/org/tool/bin.js"`），
   或用系统级安装获得工具——无需 ngm 代生成 shim。

## 重启条件

任一条出现即重新评估（写 ADR-044 后再动代码）：

1. 出现真实项目需要"纯 Git 依赖 + 自带 CLI + 无 npm"且写全路径不可接受；
2. 有一份针对"从 vendored 内容生成可执行文件"的威胁模型评审，明确
   shim 与 `run:script` / `postinstallPolicy` 两套闸门的关系；
3. 产品定位正式扩展为"依赖的 CLI 安装器"。

## 后果

- `ngm run` 仍只注入既有 `node_modules/.bin`；vendored CLI 只能显式写路径调用。
- `ngm build` 的引擎解析（`resolveProgram`）仍只查项目 `.bin` 与进程 PATH，
  不查 `ngm.vendor/.bin`。
- 2026-11-18 任务清单的 D2 标记为"按 ADR-043 暂缓"，不是未完成的功能债。
