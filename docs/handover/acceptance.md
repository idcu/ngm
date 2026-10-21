# 整条链验收清单（跨四个项目）

> 这份清单回答一个问题：**四个项目各自整改完之后，这条链真的成立吗？**
> 它不是门禁，是一次**验尸**：证明链路成立，不证明各处质量——质量由每个项目自己的测试守。
> 只在最后跑一次；任何一步红了，都不要对外说"可证明"。

## 前置

- 本机有 `git` 与 `ngm`；能访问 gitee
- lytjs 的适配分支已打标签（按 [README.md](./README.md) 的约定）
- qforge / plotpin 已各自完成"用 ngm 装回切片"与"切换并删旧"
- 建议在**目标交付平台**上各跑一次（Windows 的 `--offline`、hardlink 与 Linux 不同）
- 四仓状态与 [README.md](./README.md) 的「状态快照」一致；不一致 ⇒ **先重测再验收**（各方案都有
  「开工前置检查」，三条命令即可确认）

## 步骤 1 · 切片能独立消费（不依赖任何应用）

```
ngm init github.com/idcu/smoke-app --runtime=node
ngm add gitee:lytjs/lytjs#path=packages/common/packages/zip@<标签> --ref-type=tag
ngm install
ngm verify
```

**期望**：退出码 0；`ngm.lock` 里该依赖有 commit 与 digest；`verify` 报 `✓ match`；
`ngm.mappings.json` 的 `path` 与 `#path=` 一致，`main` 指向 `ngm-dist/index.mjs`。

## 步骤 2 · 切片真的是自包含的

```
Select-String -Path packages\common\packages\zip\ngm-dist\index.mjs -Pattern '@lytjs/'
```

**期望**：**0 命中**，退出码 0。若命中，说明内部依赖没被内联（最常见的成因是 tsup 配置里的
`external` 没被覆盖，见 [lytjs.md](./lytjs.md) §3.1），消费者会得到"模块找不到"。

## 步骤 3 · 裸导入能被构建解析

```
ngm build src/index.ts --outfile=dist/app.js
```

**期望**：退出码 0；产物里出现切片的实现（不是 external 引用）。
最快的判据是 `--dry-run` 打出来的 argv 里有形如 `--alias:` 的解析结果。

## 步骤 4 · 离线可复现（这条是"可证明"的核心）

```
ngm install --frozen-lockfile --offline
```

**期望**：退出码 0，且**零 git 子进程**（v0.13 立的不变量）。
反面：把 vendor 删掉、mirror 是冷的，再跑一次 ⇒ 必须**明确失败（exit 4）**，
不许静默降级成"看起来装好了"。

## 步骤 5 · 篡改能被发现

```
ngm verify --deep
```

**期望**：`✓ match`，退出码 0。把 `ngm.vendor/` 里任意一个文件改一个字节，再跑 ⇒
报 `critical`，退出码 **2**（这个码不许被降级）。

## 步骤 6 · 两个应用的离线形态（最容易被忽略、也最值钱）

断网；克隆 qforge / plotpin；**不跑** `ngm install`；直接双击 `index.html`。

| 应用 | 要实际点一遍的路径 |
|------|-------------------|
| qforge | 导入 xlsx → 导入 docx → 导出 xlsx → 导出 docx → 备份/恢复 |
| plotpin | 导入 DXF → 导入 PDF 底图 → 导出 xlsx 台账 → 导出多页 PDF |

**期望**：功能可用；浏览器控制台**无 404**（404 说明脚本清单或路径没改全）。

## 步骤 7 · 商业交付物

```
ngm sbom --out=dist/sbom.json
ngm attest --out=dist/attestation.json
```

**期望**：两个文件生成，退出码 0；同一份 lock 导出两次，除时间戳字段外**逐字节相同**。
许可证字段目前是 `NOASSERTION`（ngm 明确不猜）——[ngm.md](./ngm.md) 的 N4 落地后，
应能看到上游**声明**的许可，而"结论"仍是 `NOASSERTION`。

## 步骤 8 · 两个应用的产物侧自检

```
powershell -NoProfile -File <ngm>\scripts\check-vendored-standard.ps1 -Dir <qforge>
powershell -NoProfile -File <ngm>\scripts\check-vendored-standard.ps1 -Dir <plotpin>
```

**期望**：退出 **0**（不是 2——2 表示"一个依赖都没查"）。再故意删掉一个依赖的目录，必须红。

## 不通过时的分诊（哪一步红，大概率是谁的问题）

| 红的步骤 | 先查这里 |
|---------|---------|
| 1 添加/安装失败 | lytjs A3 / A4（切片清单或根清单）；Gitee 权限（`net:gitee.com` 需先允许） |
| 1 `verify` 报 digest 不匹配 | lytjs A8（产物在标记之后又被改动——标签必须指向含产物的提交） |
| 2 命中 `@lytjs/` | lytjs A1（tsup 的 `external` 未覆盖） |
| 3 构建找不到模块 | 消费方的 `ngm.mappings.json` 是否生成；`main` 是否指向 `ngm-dist/index.mjs` |
| 4 出现 git 子进程 / 触网 | 消费方是否提交了 `ngm.lock`；是否真用 `--offline` |
| 5 篡改后仍报 match | 先确认跑的是 `verify --deep`：hardlink 模式下 vendor 与 content store 是同一个 inode，**浅校验看不出**，`--deep` 才能发现 |
| 6 控制台 404 | 应用 R8（`index.html` 脚本清单没改全）；`ngm.vendor/` 是否被误加进 `.gitignore` |
| 7 两次导出不一致 | 是否有字段带了时间戳/随机量（除 `resolvedAt` 外都不该有） |
| 8 退出 2 | 应用的 `ngm.mappings.json` 缺失（等于"一个依赖都没查"） |

## 记录表（跑完填这张，作为交付证据）

| 项 | 值 |
|----|-----|
| 日期 / 执行人 | |
| 平台（OS / 架构） | |
| lytjs 分支与标签 | |
| 消费方仓库与 lock 哈希（`git hash-object ngm.lock`） | |
| ngm 版本（`ngm --version`） | |
| 步骤 1–8 结果（绿/红 + 红的那一步的分诊结论） | |
| 结论（这条链是否成立） | |

## 一句话结论模板

> 第 1–8 步全绿 = 这条链成立：**同一个 commit、同一份内容、离线可重建、篡改可发现**。
> 任何一步红，都只说明一件事：现在还不能这么对外说。
