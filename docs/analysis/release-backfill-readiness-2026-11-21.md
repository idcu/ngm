# 发布覆盖 · 回填就绪清单（2026-11-21）

> **状态：清单，未执行**（凭据阻塞 + 既有决定：38 个缺口是**声明不回填**，不是待办）。
> 依据 [release-pipeline.md](../guides/release-pipeline.md) §1–2（权威读数）与
> [competitive-followup spec](../specs/assessment-2026-11-21-competitive-followup/spec.md) D4。
> **口径纠正**：spec/tasks 初稿写"回填 38 个 tag"是错的——那 38 个是 `release-pipeline.md`
> 明确**不回填**的区间（`v0.5.0`–`v0.8.0`、`v0.65.0`–`v0.98.0`），理由是"没有人在等那些二进制，
> 功能都在源码里，任何人可按 tag 构建"。本清单尊重这个决定，不把它复活成待办。

## 1. 权威读数（2026-11-03，与 release-pipeline.md 同源）

| 读数 | GitHub | Gitee |
|---|---|---|
| 正式 tag | 101 | 101（镜像） |
| 有 release | **63** + 1 预发布（`v0.102.0-dev.2`） | **3**（`v0.2.0`–`v0.4.0`） |
| 缺口 | 38（**声明不回填**） | 98（含 38 同区间 + 未启用上传） |

读数被 `TestV126` 守着——回填后若数字变化，必须与 `release-pipeline.md` 同步更新，不能只改一处。

## 2. 实际可做的（按优先级）

| 项 | 前置 | 动作 | 完成定义 |
|---|---|---|---|
| 1. Gitee 侧 3 个已有 release 之外补 `v0.99`–`v0.101` + `dev.2` | `GITEE_TOKEN` + `GITEE_UPLOAD` 启用 | `scripts/gitee-release.sh`（已就绪） | `check-gitee-release-status.sh` 绿；`release-pipeline.md` §1 的 Gitee 读数同步 |
| 2. GitHub 侧维持现状 | 无 | **不补 38 个**（既有决定） | 读数 63 不变（除非有人提出真实需求，见 §3 重启条件） |
| 3. 下游 pin 校验 | 无 | CI drift guard 已守 `ngm-repo` 的 `.ngm-version` = `v0.102.0-dev.2`（已发布） | CI 绿 |

## 3. 重启条件（38 个缺口何时 reconsider）

`release-pipeline.md` 的决定是"现在没有人在等"，不是"永远不做"。任一出现即重开：

1. 有用户报告需要某个缺口 tag 的预编译二进制（具体版本 + 场景）；
2. 自动化工具或下游仓库开始按 release 附件存在性做门禁；
3. 主管部门/合规要求特定版本有可下载产物。

在无此类证据前，不为了"数字好看"补 38 个——那会制造 38 次构建 + 38 份需要长期维护的附件，
而它们的唯一价值是让表格从 63 变成 101。

## 4. 相关文档

- [release-pipeline.md §1–2](../guides/release-pipeline.md)（权威读数与回填决定）
- [README 当前状态](../../README.md)（63/3/38 的对外表述）
- [competitive-followup spec D4](../specs/assessment-2026-11-21-competitive-followup/spec.md)
