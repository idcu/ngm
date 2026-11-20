# Spec：2026-11-20 评估之后（第二批，剩余项）

依据：[全维度代码评估（2026-11-20）](../../analysis/ngm-full-assessment-2026-11-20.md) +
[第一批 followup](../assessment-2026-11-20-followup/spec.md)。
基线：`c5f1dcc`（第一批 A–G 已全部合入，P0×1 + P1×7 清零并推送）。
本 spec 只定义**第一批刻意没做、且今天仍真实存在**的下一轮工作；逐项清单见 [tasks.md](./tasks.md)。
禁止强推、改历史、改 git config。

## 0. 结论：本轮按 H → I → J 三批推进

第一批把"会造出错误结论 / 违反安全承诺 / 破坏契约"的项全部修完（P0/P1）。剩下的都不是"能不能用"的问题，
而是"一致性、诚实度、结构性债"。按能否安全落地分三批：

1. **H 精确性与安全提示（P2/P3，低风险，先做）**：
   - `auto` link 模式逐文件降级，却承诺"整树复制"——混合树里就地编辑仍污染 content store（M1）。
   - `HostFromRemoteURL("foo:bar")` 落入 scp 分支返回 host `foo` → 触发 `net:foo`，fail-closed 但提示误导。
2. **I 覆盖率与判据诚实度（P2，需先定契约）**：
   - 跨包覆盖率门槛只守 `internal/core/verify`（78%），其余产品代码无门禁——门槛校准平台与读数要同源（11-19 教训）。
   - `v089` "run-or-explain" 判据按**引用**记账，不按**执行**：一个名字写进说明文字也算"被提到"。
3. **J 结构性技术债（P2，高风险，先写 ADR 再动，或明确永久不做）**：
   - `package main` 44 文件 / 约 2.2 万行；usage 常量与 flag 声明双源；契约网过剩（roadmap 停止清单）。
   - 这三项收益/风险不匹配，**本轮不直接重构**，先各写一条"触发条件"，达到条件才排期。

**非工程项单列**：发布资产补齐（63/3/38/101 里的 release 缺口）需 `GITEE_TOKEN` 与 GitHub dispatch 权限，
不占工程排期——见 §5。

## 1. 不变量（任何批次都不得破坏）

- 退出码 0–6 单码语义不变；完整性失败(2) 不被资源缺失(4) 或工具缺失(5) 覆盖（ADR-039）。
- `run:git` / `net:<host>` / `run:<engine>` / `run:script` 闸门不合并、不互相放行；触网路径启动子进程前必过 `CheckNetAccess`。
- `internal/core` 不 import 语言/JS 层；`--json` stdout 恰好一份 JSON；不接受 `--json` 的命令失败时 stdout 为空（v0.16 规则④）。
- 序列化确定性：排序 + 固定缩进/LF/末尾换行；写进报告的切片顺序必须可复现。
- 所有 `t.Skip` 必须登记进 skip 账本；文档读数与 `docs/guides/release-pipeline.md` 同源（63/3/38/101 被 `TestV126` 守着）。
- 修复必须带**变异检验**：撤掉即红、装上即绿（除非该项本身只改文档或只改 CI 发布annotation）。
- **不改变已发布的 0–6 退出码含义、不动 `ngm run` 既有语义、不新增 registry 能力、不删 ADR / 逐版本归档。**

## 2. 批次与耦合

| 批次 | 内容 | 前置 | 提交粒度 | 风险 |
|---|---|---|---|---|
| H | H1 auto link 语义对齐 · H2 scp host 提示 | — | 每提交 1 项 | 低（H1 需先定契约，见 §3.1） |
| I | I1 覆盖率口径 · I2 v089 判据诚实度 | H 合入后 | 每提交 1 项 | 中（I1 动 CI 门禁，需平台同源；I2 需先定契约） |
| J | J1/J2/J3 结构性债的触发条件 ADR | — | 每提交 1 条 ADR | 低（只写文档，不改代码） |

## 3. 必须先做的契约决定

**3.1 `auto` link 的"降级"到底是什么语义？（H1）**
现状：`linkEntries`（`links.go:436-489`）从 hardlink 起步，首次失败就把整棵后续树改成 copy，
返回 `Mode=LinkCopy`；但**已经 hardlink 的那部分仍是硬链接**——一棵"半 hardlink、半 copy"的混合树。
`vendor-layers.md:141` 却承诺"跨卷不支持时**整树复制**"，`:249` 又警告就地编辑 hardlink 会污染 content store。
两个选项：
- (a) **兑现"整树复制"**：落地前预探测 hardlink 在该卷可行性，不可行则整棵树全部 copy（不产生混合树）；
  代价是一次预探测 + 可能放弃本来能 hardlink 的文件。
- (b) **修正文档为"逐文件降级"**：明确 auto 可能产生混合树，并把就地编辑污染的风险写清；代价是削弱承诺、用户仍可能踩坑。
默认建议 **(a)**——它让"就地编辑 auto 落地的 vendor 不污染 content store"这条承诺真正成立。
需改 `LinkResult` 语义（让 mode 反映实际整树模式）与 `vendor-layers.md`，并写 ADR 补记；**先定再改代码**。

**3.2 覆盖率门槛的口径与平台（I1）**
`ci.yml` 的 floor=78 是按 Linux 实测 80.5% 减 2.5pp 校准的，平台名已写进 annotation——这部分是对的。
要决定的只是**是否扩到 verify 之外的包**：
- 建议**不新建第二个门槛**（roadmap 停止清单：判据网已过剩）。改为把"每包覆盖率读数"发 annotation
  （像 verify 那样 `::notice`），让覆盖率的**下滑可见**，但不设闸；门槛只在有真实回归触发时再加。
- 若决定加门槛，必须先按**实际测量平台**校准，并把平台名写进 summary（11-19 教训：拿 Windows 82.1% 当 Linux 门槛 = 绊线）。

**3.3 v089 判据收紧到什么程度（I2）—— 已拍板：不改（2026-11-20）**
选项 (a) 保持按引用 / (b) 增加"登记理由仍成立"的反向核对。取 **(a)**：它守的"从没被提到不可能
悄悄发生"下限已成立，真正"跑过一次"由各脚本自己的判据另守；而 (b) 等于新增一张契约网
（停止清单禁止），且它抓的最后一个真问题 build-release.ps1 双实现已在 D4 修掉。理由与重启条件
写进 tasks I2，避免下轮重开。

## 4. 验收总闸（每批提交前都要过）

```bash
go build ./... && go vet ./...
go test -count=1 ./internal/...                      # 单元与核心库
go test -count=1 ./cmd/ngm -run '<本批判据>'          # 定向判据
bash scripts/check-docs-links.sh docs README.md
node scripts/check-discipline.mjs                     # 必须仍是 13/13
git diff --check && gofmt -l <changed .go>            # 必须为空
```

- 改了源码的每一项：加一条判据 + 变异检验；新增 `t.Skip` 必须同步 skip 账本。
- 改了 JSON 形状的项：同步 `docs/guides/cli.md` 形状表（`TestV26` 反向守护）。
- 改了文档读数 / 引用数的项：与 `docs/guides/release-pipeline.md` 同源（`TestV126`、`TestV71` ADR 索引守护）。
- 新增 ADR 必须同步 `docs/adr/README.md` 索引（`TestV71` 守护），否则全量判据红。

## 5. 明确不做（停止清单仍然有效）

- **不新建"契约网"**：roadmap 已记录"最后一个产品缺陷是 v0.55，之后 10 版抓的全是文档与判据自己"。
  本轮 I 批只让读数**可见**（annotation），不为假想回归加闸。
- **不删 ADR（46 个）与逐版本归档（211 个）**：17+ 测试引用它们。
- **不直接重构 `package main` / usage 双源**：J 批只写触发条件 ADR；没有真实触发就不动代码。
- **承接第一批的"不改"决定**（理由已写进第一批 tasks.md，避免重开）：
  - M11 OSV `CheckNet` nil fail-open —— 已记录的决定，由调用方必传 + `TestV108` 守着；
  - M9 空值 flag 语义分裂 —— 逐命令语义有意为之；
  - M14 modproof 离线降级 —— 评估描述与代码不符，实际返回 Operational(4)，不是 warning；
  - 中英混杂 —— 统一会重写全部 golden，无功能收益。
- **非工程项（不占排期）**：发布资产补齐（GitHub 63/101 + Gitee 3，缺口 38/98）需 `GITEE_TOKEN` /
  workflow dispatch 权限；`scripts/gitee-release.sh` 与 `check-gitee-release-status.sh` 已就绪，属人工动作。

## 6. 相关文档

- [全维度代码评估（2026-11-20）](../../analysis/ngm-full-assessment-2026-11-20.md) —— 本轮依据
- [第一批 followup](../assessment-2026-11-20-followup/spec.md) · [第一批 tasks](../assessment-2026-11-20-followup/tasks.md) —— 已完成
- [能力矩阵](../../internals/capability-matrix.md) · [路线图（停止清单）](../../internals/roadmap.md)
- [ADR-039](../../adr/adr-039-exit-code-precedence.md) · [ADR-045](../../adr/adr-045-json-output-contract.md) —— 退出码与 JSON 契约
