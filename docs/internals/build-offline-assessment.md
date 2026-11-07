# 构建可离线性评估（P2-10，2026-11-07）

> 结论先行：**当前不 vendor、不引入镜像**；把一个"无凭据冷构建"探测放进 CI，
> 用读数决定何时需要改。理由是实测表明公开模块在新机器上可以直接构建，而
> vendoring 的代价（体积、升级纪律、LICENSE 汇总）在"能用"之前就先付了。

## 1. 问题

`ngm` 的构建依赖两个外部模块：

```
require github.com/tetratelabs/wazero v1.9.0
require gitee.com/idcu-go/modproof v0.2.0
```

（版本由 `cmd/ngm/modproof_dependency_test.go` 钉住。）其中 `modproof` 位于
Gitee——一个**发布流水线还依赖它可读**的外部服务。问题是：

> 一台没有 Gitee 凭据、模块缓存为空的新机器，能否 `go build ./...`？

不能，就意味着"拿到源码也构建不出来"，与"依赖证明层"的定位冲突。

## 2. 实测（2026-11-07，Windows 本机）

条件：`GOMODCACHE` 指向全新目录、`GIT_TERMINAL_PROMPT=0`、
`GIT_CONFIG_GLOBAL` 指向空文件、`GIT_CONFIG_NOSYSTEM=1`（即没有任何凭据改写）。

```
go: downloading gitee.com/idcu-go/modproof v0.2.0
go: downloading github.com/tetratelabs/wazero v1.9.0
COLD_BUILD_EXIT=0 ELAPSED=14.2535414
```

结论：**两个模块都能匿名取到**，冷构建 14.3 秒完成。Gitee 上的 `modproof`
当前是公开可读的，构建不依赖 token。

同一读数在 CI 里由 `cold-build-no-credentials` job 复读：它不配置 `GITEE_TOKEN`、
用空 `GOMODCACHE` 构建；失败时把结论写进 job summary。该 job 是 advisory
（`continue-on-error`）——模块若改为私有，这一步**本来就该失败**，那是仓库
可见性或镜像的决策，不是某次 push 的错；但失败必须被看见，不能静默变绿。

## 3. 三个选项

| 选项 | 做法 | 代价 | 何时值得 |
|------|------|------|---------|
| A. 维持现状 + 探测 | 依赖公开可读；CI 每次读一次冷构建 | 依赖 Gitee 可用性与模块可见性；失败是"发现"而不是"修复" | 现在 |
| B. `go mod vendor` | 把 wazero + modproof 复制进 `vendor/`，构建不再触网 | 仓库体积增长（wazero 较大）；每次升级要重跑 `go mod vendor`；LICENSE 汇总；`vendor/` 与 ngm 自己的 `ngm.vendor/` 概念容易混淆（前者是 Go 构建输入，后者是运行时证明存储） | 模块转私有、或离线构建成为发布/审计硬需求 |
| C. 模块镜像 | 把 modproof 同步到 GitHub / 自建 GOPROXY，`replace` 到镜像 | 多一条同步链路与一个"镜像是否与源一致"的新问题（本项目的主题恰恰是证明一致性） | Gitee 长期不可用，且 B 的体积不可接受 |

## 4. 决定

选 **A**，并把触发条件写下来：

1. `cold-build-no-credentials` 连续失败（Gitee 不可达或模块转为私有）→ 立即评估 B；
2. 发布流程要求"离线可构建"（例如供应链审计）→ B；
3. 若选 B，`vendor/` 与 `ngm.vendor/` 的区别必须写进 `docs/architecture/`，
   否则下一个人会把两者当成同一件事。

## 5. 复核

CI：`.github/workflows/ci.yml` 的 `cold-build-no-credentials` job。

本机等价命令（注意 `GOMODCACHE` 必须指向空目录，否则读到旧缓存就测不到"冷"）：

```bash
GOMODCACHE="$PWD/.tmp-gomodcache" \
GIT_TERMINAL_PROMPT=0 GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
go build ./...
```
