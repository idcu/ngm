# 删除记录（DELETIONS）

> **这份文件是什么**：每一次**结构性删除**（目录、公共 API、兼容层、发布产物、整批过程文档）的一行账。
> 判据：`TestV118DeletionLogIsComplete` —— 每条记录六个字段齐全、日期合法，且**记的路径不得还在版本库里**。
>
> **为什么需要**：结构性删除是这个仓库里最容易"只做一半"的动作——文件删了、引用没改、
> 判据假设没复查。更隐蔽的是**假删除**：记录说删了，`git ls-files` 里还在。
> 这条判据就查这一件事。
>
> **登记时机**：同一个提交里。不要事后补——补出来的记录没人能验证。
> **谁登记**：做删除的人。单人仓库自批准也要写明"为什么现在删"，否则三个月后没人知道。

## 模板

```text
## YYYY-MM-DD · 一句话标题
- 删除：<仓库相对路径>、<仓库相对路径>
- 日期：YYYY-MM-DD
- 批准：<人/角色>
- 为什么：<一句话；说清"为什么是现在"与"不删会怎样">
- 影响：<哪些引用已改；哪条判据的假设被打破；没有影响就写"无">
- 验证：<删除后跑过哪条命令，结论是什么>
```

---

## 2026-11-03 · `docs/handover/` 六份文件出库

- 删除：`docs/handover/README.md`、`docs/handover/acceptance.md`、`docs/handover/lytjs.md`、`docs/handover/ngm.md`、`docs/handover/plotpin.md`、`docs/handover/qforge.md`
- 日期：2026-11-03
- 批准：仓库维护者
- 为什么：这套接手文档按"项目"分册（ngm / lytjs / plotpin / qforge），与 `docs/` 自己的文档地图重复；
  留着就会出现"两份都说自己是权威"。它属于本地过程文档，与 `analysis/` 同类，不入库。
- 影响：三处指向它的死链改为「本地过程文档，不入库」；`docs/README.md` 的目录表移除一行；
  `docs/development/README.md` 的发布清单引用同步删除。无判据依赖该目录。
- 验证：`powershell -File scripts/check-docs-links.sh` → `all docs links OK`；`go test ./cmd/ngm -run TestDocsAnchorsResolve` 通过。

## 2026-11-03 · 方法论文档改名（`pivoise-methodology.md` → `engineering-discipline.md`）

- 删除：`docs/practice/pivoise-methodology.md`
- 日期：2026-11-03
- 批准：仓库维护者
- 为什么：文件名带项目名，内容却已写成通用方法论；保留旧名会让人以为"这只适用于枢衡"。
- 影响：`docs/README.md` 的 SSOT 表与多处 `§` 引用改指新文件名；`git mv` 保留了逐行历史。
- 验证：`TestDocsAnchorsResolve` 与 `TestV15DocExamples` 通过；`scripts/check-docs-links.sh` 无坏链。

## 2026-11-03 · 发布脚本与 SARIF 样例出库

- 删除：`scripts/upload-gitee-assets.ps1`、`ngm.sarif`
- 日期：2026-11-03
- 批准：仓库维护者
- 为什么：上传脚本被 `release` 子命令取代（幂等 + dry-run + 假 API 网），两个实现并存时没人知道该跑哪个；
  `ngm.sarif` 是 CI 产物的手工样例，构造方法已在测试里，样例本身会过期。
- 影响：`docs/guides/installation.md` 与 release 文档改为指向子命令；无判据读取该样例。
- 验证：`go test ./cmd/ngm` 全绿；`git grep upload-gitee-assets` 无结果。
