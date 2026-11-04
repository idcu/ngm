# ADR-042：为**项目脚本**开一个沙箱外的执行面（`ngm run`）

- **状态**：已定并已实施（v0.120）
- **日期**：2026-11-04
- **范围**：命令面、权限模型、退出码契约

> **结论**：`ngm run <script>` 只编排**用户自己写的项目脚本**：`package.json.scripts`
> 权威、`ngm.json.scripts` 回退，平台 shell 原样执行，`node_modules/.bin` 注入 PATH，
> 退出码原样透传。它由一条独立的 `run:script` 授权（默认拒绝），**与 install 期
> 依赖的 postinstall 策略是两套闸门**；用户显式调用之前，脚本不会被执行。

## 背景

装上 ngm 之后，跑项目脚本仍要拉 npm——两个工具并存、两套生命周期、两份环境变量。
评估（`docs/analysis/ngm-assessment-2026-11-04.md`）确认 ngm 缺少的只是“**编排**”这一层：
脚本发现、PATH 注入、参数透传、pre/post、退出码与信号转发。安装、registry、
peer 求解不在其中。

## 决策

1. **只做编排，不做安装器**。ngm 不生成 `node_modules/.bin` 的 shim，也不模拟 npm 的
   依赖安装；脚本里的命令能跑，是因为依赖已经以某种方式装好。
2. **沙箱外执行**。`run` 启动的进程继承调用者的环境与文件系统权限——这是用户
   显式要求执行自己的代码，把它塞进 Deno 沙箱既无意义（脚本本来就能做任何事），
   也会让“构建”这类正常工作无法完成。
3. **单独的权限闸门** `run:script`，默认 deny。理由：install 期的
   `supplyChain.postInstallPolicy` 管的是**别人的**代码；`run` 管的是**你的**代码。
   两条决策需要的证据不同，混成一条会让“我允许了构建脚本”顺带允许依赖的 postinstall。
4. **退出码不映射**。子进程的码原样透传（信号见 `128 + signal`），绝不折算成 0–6；
   ngm 自己的失败仍用 3（脚本缺失/冲突/权限/用法）与 5（平台 shell 不可用）。
   把脚本的 1 说成 ngm 的策略失败会污染整张退出码表。
5. **环境变量最小集**：`INIT_CWD`、`npm_lifecycle_event`、`npm_lifecycle_script`、
   `npm_package_name`、`npm_package_version`。不设 `npm_execpath`，不承诺
   `npm_config_*`——**这是契约，不是“近似 npm”**。模拟得越像，越容易被当成兼容层。

## 代价与边界

- 脚本看到的 `node_modules/.bin` 是既有的，ngm 不会为缺失的依赖补链；
  缺失时报的是平台 shell 的“找不到命令”，并在 stderr 追加一行诊断。
- Windows 无 POSIX 信号语义：130 只在子进程自己产生时透传；映射函数另有一条单测。
- 不做并发编排、watch、任务依赖图、`npm publish`、peer 求解。
- **不接进 install / verify / CI 自动流程**：`run` 只在命令行被显式调用时执行。

## 后果

- 用户可以 `ngm run build` 而不需要 npm（前提是依赖已装好）。
- 权限提示里给出可直接粘贴的 `{"permissions":{"allow":["run:script"]}}`。
- 退出码契约网新增“透传族”说明；`contracts/exit-codes.json` 不变（0–6 语义不变）。
