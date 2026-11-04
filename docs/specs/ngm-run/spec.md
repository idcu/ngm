# Spec：`ngm run` + D1/D2/D3 + 发布核实

依据：`docs/analysis/ngm-assessment-2026-11-04.md`（评估结论）与用户 2026-11-04 的拍板决策。
状态：待批准后执行；批准即按批次提交、推送（禁止强推/改历史/改 git config）。

## 0. 结论

新增 `ngm run <script>`，只做“项目脚本编排”，不做安装器、不做 registry、不执行依赖 lifecycle。
新增单一权限闸门 `run:script`（默认 deny），与 install 期 `supplyChain.postInstallPolicy` 是两套闸门。
其余三个缺陷按“实现追上文档 / 去硬编码 / 保 scope”处理。

## 1. `ngm run` 语义（契约）

### 1.1 脚本来源

- `package.json.scripts` 为权威来源。
- `ngm.json.scripts` 为回退来源（新增字段，`map[string]string`）。
- 同名脚本同时出现在两处 → **exit 3**，报错点名 `package.json` 与 `ngm.json` 两处来源，禁止静默覆盖。
- 两处都没有该脚本 → exit 3（列出可用脚本名，最多 10 个）；`--if-present` 时 exit 0 并在 stderr 打一行 note。
- `ngm.json` 不存在不是错误（纯 npm 项目可用）；存在但损坏 → exit 3（沿用既有配置错误路径）。

### 1.2 pre/post

按 **npm 约定**：`pre<name>`、`<name>`、`post<name>`（用户原文写作 `<name>pre`/`<name>post`，按“按 npm 约定”执行并按 npm 真实命名实现；文档明确写这三个键名）。
执行顺序 pre → main → post；任一步非 0 立即停止，`ngm run` 返回**首个失败步**的退出码。
每步的 `npm_lifecycle_event` 分别为 `pre<name>` / `<name>` / `post<name>`，`npm_lifecycle_script` 为该步命令串。

### 1.3 shell 与参数

- POSIX：`sh -c <command>`；Windows：`cmd.exe /d /s /c <command>`。
- 不做二次引号处理，交给平台 shell（与 npm 一致）。
- 第一个 `--` 之前只允许出现一个位置参数（脚本名），更多位置参数 → exit 3 并提示“额外参数请放在 `--` 之后”。
- `--` 之后的参数**原样**按空格拼接追加到脚本串末尾。

### 1.4 PATH 与环境

- 按 npm 的查找习惯，从项目目录向上逐级祖先收集已存在的 `node_modules/.bin`，**近者优先**，整体注入到子进程 PATH 前端。
- 未找到任何 `.bin` 时：stderr 打一行 note（不致命），脚本里缺失的可执行文件由平台 shell 报错并透传其退出码。
- 环境变量最小集（契约，不是“近似 npm”）：
  `INIT_CWD`（ngm 进程启动目录）、`npm_lifecycle_event`、`npm_lifecycle_script`、`npm_package_name`、`npm_package_version`（优先 package.json，缺失时用 ngm.json）。
- **不设** `npm_execpath`；**不承诺** `npm_config_*`；文档明确写“只承诺这五个”。

### 1.5 退出码

- 子进程退出码**原样透传**，绝不映射为 ngm 的 0–6 内部错误码。
- 子进程被信号终止：`128 + signal`（SIGINT→130、SIGTERM→143）；Windows 无信号语义，由子进程自身产生的 130 原样透传。
- ngm 自身错误：0（成功/`--if-present` 跳过）、3（脚本缺失/来源冲突/权限拒绝/用法错误）、5（项目 shell 不可用）。
- 退出码契约网单独登记“透传族”：help 与 observability 文档在 0–6 单码表之外单列一段说明，不新增单码。

### 1.6 权限

- 单一闸门 `run:script`，在**任何一步执行前**判定一次（pre/post 不重复判定）。
- 默认 deny（沿用 `DefaultTier` 的 `TierConfigurable` 行为）；首次拒绝时 hint 含可直接粘贴的 JSON：`{"permissions":{"allow":["run:script"]}}`。
- hint 必须额外说明：项目脚本在 **ngm 沙箱之外**执行；与 install 期依赖脚本的 `supplyChain.postInstallPolicy` 是两套闸门。
- **禁止**把 `run` 接入 install / verify / CI 自动流程（只在用户显式调用时执行）。

### 1.7 列表

- `--list`：人类可读，按名字排序，列出 name、来源、命令、是否有 pre/post。
- `--json`：与 `--list` 同源数据；单独给 `--json` 等价于 `--list --json`。
- JSON 形状：
  `{version, project, scripts:[{name, command, source, pre?, post?}]}`。

### 1.8 不做

并发编排、watch、任务依赖图、`npm publish`、peer 求解、“命令名即脚本名”简写。

## 2. 逐文件改动

### 2.1 新增 `internal/runscript/`

`runscript.go`：
- `type Source string`（`"package.json"` / `"ngm.json"`）。
- `type Script struct { Name, Command string; Source Source; Pre, Post string }`。
- `type Set struct { Project string; Scripts []Script }` + `Lookup(name) (Script, bool)`。
- `Load(dir string) (*Set, error)`：读 `package.json`（`name`/`version`/`scripts`）与 `ngm.json.scripts`；冲突返回 `errs.CodeConfigInvalid` 并点名两处；两者都空返回“no scripts defined”。
- `BinDirs(dir string) []string`：向上收集 `node_modules/.bin`，近者在前。
- `type ExecSpec { Dir, Command, Event, PackageName, PackageVersion string; ExtraArgs []string; BinDirs []string }`。
- `Exec(ctx, spec, stdout, stderr io.Writer) (int, error)`：选 shell、拼参数、构造 env、`exec.CommandContext`；非 `ExitError` 的错误（shell 不存在）返回 `errs.CodeNotFound`（exit 5）。

`exitcode_unix.go`（`//go:build !windows`）：`WaitStatus` → `128+sig`，未退出用 `ExitCode()`。
`exitcode_windows.go`（`//go:build windows`）：直接用 `ExitCode()`。

`runscript_test.go`：来源/冲突/回退、pre/post 识别、BinDirs 祖先顺序、env 最小集、参数拼接、退出码映射（Windows 用一个 `exit /b 130` 脚本 + 映射函数判据）。

### 2.2 新增 `cmd/ngm/run.go`

- `runUsage`（帮助文本，含 EXIT CODES 段：0/3/5 + 缩进的“任意其他码透传”说明）。
- `runRun(ctx, args, stdout, stderr) int`：自定义拆分首个 `--`；flag：`--dir`、`--if-present`、`--list`、`--json`。
- 复用 `newProjectEnv(dir)` 取 `Policy`；`pol.CheckRun("script")` 失败时用 `errs.Wrap(CodeConfigInvalid, ...)` 覆盖 hint（保留可粘贴 JSON + 两套闸门说明）。
- 输出：`runListReport`（`--list/--json`）；成功执行保持静默。
- 子进程输出直接写 `stdout/stderr`。

`run_test.go`（真跑，Windows 上必须实际执行）：
1. 脚本成功 → 0；
2. 参数透传（`--` 后两个参数）；
3. pre 失败短路（post 不执行，退出码 = pre 的）；
4. `--if-present` 缺失脚本 → 0；
5. `--list --json` 形状；
6. 无 `.bin` + 脚本调用不存在的命令 → 非 0，输出含 not-found；
7. 有 `.bin` 时能解析本地可执行（Windows 写 `.cmd`，POSIX 写可执行 sh 脚本）；
8. `run:script` 未授权 → 3 + 可粘贴 JSON；
9. 同名来源冲突 → 3 且点名两处；
10. 退出码 130：POSIX 用 `kill -INT $$`；Windows 用 `exit /b 130` 透传；信号映射函数另有单元判据。

### 2.3 命令登记（否则契约网红）

- `cmd/ngm/root.go`：`commands` 加 `{Name:"run", Run:runRun, Usage:runUsage}`。
- `cmd/ngm/help.go`：`rootUsage` 命令表加 `run` 行；退出码段加“透传族”说明（不新增单码）。
- `cmd/ngm/config_matrix_test.go`：加 `"run": {"args": ["ok"]}` 或与现有矩阵形状一致的一条。
- `cmd/ngm/exit_codes_contract_test.go`：为 run 登记 0/3/5 的可测来源（0 需先授予 `run:script`；5 走 `exitCodeElsewhere` 清空 PATH）。
- `cmd/ngm/exit_codes_sources_test.go`：确保 run.go 的返回路径不匹配 `return 1` 等 token（返回变量）。
- `cmd/ngm/doc_field_tables_test.go` / `doc_shape_reverse_test.go`：登记 `run` → `./run.go`、`runListReport`，并在形状表加一行。
- `testdata/help.golden` 重生成。

### 2.4 配置与文档

- `internal/config/config.go`：`ProjectFile` 加 `Scripts map[string]string `json:"scripts,omitempty"``。
- `docs/guides/configuration.md`：ngm.json 字段表加 `scripts`；`NGM_HOME` 语义单列一节。
- `README.md`：命令表 27→28；`NGM_HOME` 描述与实现一致；D2 去掉硬编码 tag 数。
- `docs/guides/cli.md`：命令清单加 `run`（含最小 env 契约与两套闸门）。
- `docs/architecture/security-model.md`：权限表加 `run:script`；说明沙箱外执行。
- `docs/architecture/observability.md`：退出码表后加“透传族”段落。
- `docs/adr/ADR-042-*.md` + `docs/adr/README.md` 索引：为什么开这个沙箱外执行面。
- `CHANGELOG.md`：新增 `## Unreleased` 段。
- 评估文档 `§7` D1/D2/D3 状态改为“已修”，补修复 commit 引用。

## 3. 缺陷修复

### D1 `NGM_HOME`

根因：`vendor.DefaultLayout()` 尊重 `NGM_HOME`，但全局配置走 `config.GlobalPath(home)`（永远 `<home>/.ngm/config.json`），`homeDirOrEmpty()` 也不会换算 NGM_HOME；`env.go` 的 `globalConfigSource()` 因此指向错误文件。

修复（最小面）：
- `internal/config/config.go` 新增：
  - `func EffectiveGlobalPath(home string) string`：`NGM_HOME` 非空 → `<abs(NGM_HOME)>/config.json`；否则 `GlobalPath(home)`。
  - `func LoadFrom(projectDir, globalPath string) (*Result, error)`；现有 `Load(projectDir, home)` 改为 `LoadFrom(projectDir, EffectiveGlobalPath(home))`。
- `cmd/ngm/env.go`：`newProjectEnv` 与 `globalConfigSource` 走 `EffectiveGlobalPath`。
- `cmd/ngm/engine.go`、`cmd/ngm/config.go`（validate/show）同步。
- `cmd/ngm/testenv_test.go` 的 `WriteUserConfig` 写 `config.EffectiveGlobalPath(home)`，保证隔离测试与新语义一致。
- 测试：`internal/config` 单测（NGM_HOME 指向临时目录时 `LoadFrom`/`EffectiveGlobalPath` 读到该处配置；未设置时回退），`ngm config show` 黑盒一条（配置只在 NGM_HOME 下也存在时生效）。
- 文档：若仍有意把某些安全配置固定在真实 home，则单列例外；本实现无例外。

### D2 tag 计数漂移

根因：README:250/549 硬编码“101 个 tag”，实测 103。
修复：删除具体数字，改为“上百个 release tag（精确数量以 `git tag` 读数为准，本行不锁定）”。
确认 `scripts/check-release-status.sh` 不解析该数字；`--drift-guard` 只比最新 tag 与版本/产物，保持绿。

### D3 adopt scoped 回退

根因：`readPackageName(dir, filepath.Base(dir))` 在缺 `package.json` 时丢 scope。
修复：新增 `fallbackName(dir)`：目录形如 `.../node_modules/@scope/<name>` 且是直接子目录 → `@scope/<name>`；否则保持 basename。
单测（`internal/adopt/*_test.go`）：
1. `node_modules/@scope/reg` 无 package.json → `@scope/reg`；
2. 嵌套 `node_modules/a/node_modules/@scope/b` → `@scope/b`（subpath 场景）；
3. lock `resolved` 为 `git+https://…/@scope/name.git#<sha>` + 目录 `node_modules/@scope/name` → provenance 的 `url` + `commit` 正确。

## 4. 发布核实

1. 运行 `bash scripts/check-release-status.sh --drift-guard`，记录 0.102.x 的 tag / release / 产物读数（含 API 输出）。
2. 若缺产物且凭据可用：对**已存在 tag** 用已有 release workflow 的幂等重跑（`--clobber` 由脚本处理）；不新造 tag、不手造附件、不改历史 tag。
3. 若网络/凭据阻塞：报告写“未执行”，并确认 `.github/workflows/release.yml` 的 `workflow_dispatch` 入口可用。
4. ngm-repo 只在 gate 明确需要时更新。

## 5. 验证与收尾

- 批次 1（run 核心）：`go test -run 'Run' ./cmd/ngm ./internal/runscript`（含 Windows 真跑）。
- 批次 2（闸门/契约/文档）：`go test -run 'TestV22|TestV25|TestV26|TestV37|TestV57|TestV15|TestV54|Doc|Help' ./cmd/ngm`。
- 批次 3（D1/D2/D3）：对应包单测 + `go test ./internal/config ./internal/adopt`。
- 批次 4（发布核实）：脚本输出归档进报告。
- 收尾：`go build ./...`、`go vet ./...`、`go test -count=1 -timeout 40m ./...`；`bash scripts/check-docs-links.sh`；重生成 `testdata/help.golden`。
- 不新增外部 Go 依赖；不改发布矩阵。
- 分批 commit + push（两仓按需）；禁止强推/改写历史/改 git config。

## 6. 验收判据

1. `ngm run` 在 Windows 上真跑通并返回 0；失败步短路与透传码有黑盒证据。
2. `run:script` 默认拒绝且 hint 可直接粘贴；两套闸门在 help 与文档中可见。
3. D1 有“配置只在 NGM_HOME 下也能生效”的黑盒证据；D3 三条单测绿；D2 无硬编码数字且守卫绿。
4. 收尾命令全绿；help golden 与文档命令表一致（28 命令）。
5. 报告按用户要求分块，明确未执行项及原因。
