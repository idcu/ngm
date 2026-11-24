# 引擎 adapter 模型

> **结论**：ngm core 不做自研引擎。所有构建能力通过 adapter 调外部工具，第三方永久优先。
> adapter 层只回答"怎么调"，不回答"调什么"——后者由引擎清单与能力声明决定。

---

## 数据流

```
ngm.json 的 engines.<kind> / --engine
        │  选择（primary → fallbacks 顺序尝试）
        ▼
Catalog.Probe（可用性判定 + run:<engine> 闸门 + 版本 spawn 同一条路径）
        │
        ▼
adapter（embed / subprocess / wasm）→ 引擎进程 → 结构化结果
        │
        ▼
失败映射：未声明 exit 3 / 没装 exit 5 / 引擎自己失败 exit 1 / gate 拒绝 exit 3
```

**状态**：条目按 `(name, kind)` 唯一；同一个 esbuild 可注册 `bundle` + `transform` 两个 kind。
内置清单只收录已适配引擎；新增条目一律 `optional`——没装不算 issue，用到那一刻才失败并给安装提示。

**失败语义**：三类很容易误读的失败必须分开——

- 未声明该 kind 的引擎 → exit 3，报错点名 kind、列出候选、指向 `ngm.engines.json`
- 声明了但没装 / 干不了该能力 → exit 5（拿 esbuild 顶类型检查会被明确拒绝，不静默当作通过）
- 引擎跑起来了但自己失败 → exit 1，引擎原始退出码保留在消息与 `--json` 里，不透传

`run:<engine>` 闸门排在可用性与能力之后：引擎根本没装时正确的码是 5，顺序反了会给用户一条永远没用的授权建议。

---

## Adapter 类型

| adapter | 调用方式 | 适用场景 | 状态 |
|---------|---------|---------|------|
| `embed` | Go 直接调用 | 兜底、dry-run、离线 stub（`self`） | 已实现（非生产级） |
| `subprocess` | spawn CLI | esbuild / tsc / deno / postcss | **默认方式** |
| `wasm` | wazero 内执行 WASI preview1 命令模块 | 安全敏感场景 | 已实现（见 ADR 汇总表 ADR-011） |
| `remote` | 网络调用 | 自托管构建服务 | **按决定排除**：源码要离开本机，产物无法本地证明（见 ADR 汇总表 ADR-013、ADR-020） |

**边界**：wasm 只跑 WASI 命令模块；期待 JS 宿主导入（`env.*`）的 wasm 构建不在范围内，那属于
`subprocess` + node 的范畴。engine adapter 是配置与编排层，若只是分别调外部命令，ngm 的差异化不在这里。

---

## 引擎接口

```go
type Engine interface {
    Name() string
    Kind() EngineKind
    Version() (string, error)
    Available() bool
}

const (
    KindTransform EngineKind = "transform"
    KindBundle    EngineKind = "bundle"
    KindTypeCheck EngineKind = "typeCheck"
    KindTypeDecl  EngineKind = "typeDecl"
    KindCSS       EngineKind = "css"
)

// 每个能力方法都接收 context.Context：引擎是子进程，必须能被取消。
type TransformEngine interface {
    Name() string
    Transform(ctx context.Context, input []byte, opts TransformOptions) (*TransformResult, error)
}
// BundleEngine / TypeCheckEngine / TypeDeclEngine / CSSEngine 同风格。
```

接口只覆盖"怎么调"；**选项 → argv 的映射**属于 subprocess 协议，按能力定义与各 kind 的清单条目绑定。
引擎 `command` 从不经过 shell：按空白切分（允许引号包裹）后直接执行，否则随仓库进入的
`ngm.engines.json` 等于把任意命令执行权交给任何一个 PR。

---

## 内置清单

内置条目只收录已适配引擎：

| kind | 内置引擎 | 不声明时的行为 |
|------|---------|---------------|
| `bundle` / `transform` | `esbuild` | 内置默认选中，直接可用 |
| `typeCheck` / `typeDecl` | `typescript`（`tsc`），`optional` | exit 3，提示写哪个键 |
| `css` | `postcss`，`optional` | exit 3；`esbuild` 不覆盖 css |
| `deno` | **刻意不进内置清单** | 需自行声明（bundle 是实验特性，且会抢掉 typeCheck 默认顺序） |

`--minify` 遇到不支持的引擎（如 postcss 无内建压缩）必须**明确告知**被忽略；引擎版本/摘要绑定不符时
分别 exit 5 / exit 2，CSS 压缩能力不支持时 fail-closed（exit 3），见 ADR 汇总表 ADR-058。
`ngm engines list` / `ngm engines validate` 的探测与执行走同一道闸门，未授权引擎显示 `not probed`。

---

## 子进程与资源边界

- **输出有界**：stderr 恒 1 MiB，stdout 默认 1 MiB，超出丢弃并计数，末尾追加截断说明；对子进程仍报写入成功，
  要的是有界失败而不是半途 SIGPIPE。例外：`transform` / `css` / 不带输出文件的 `build` 里 stdout 就是产物，
  走 64 MiB 的 `artifactLimit`，且撞上界即失败——绝不交付被截断的产物。
- **环境干净**：`GIT_*` 重定向变量、shell 注入变量从子进程环境剔除；凭证只透传、不读取、不落盘。
- **身份判定**：`run:<target>` 判定的是解析后的真实文件（展开符号链接），名字授权与路径授权永不互串；
  项目内可执行文件只能用路径授权放行。wasm 的身份名是模块文件名，不解析路径（见 ADR 汇总表 ADR-038 周边与[安全模型](./security-model.md)）。

---

## 诚实说明

1. 统一 interface 不等于底层引擎统一；抽象过深会破坏底层引擎的表达能力
2. subprocess 有启动开销，高频调用（HMR）不适合反复 spawn——那类能力本就不在 ngm 范围内
3. `self` 引擎只做兜底、dry-run、离线 stub，不做生产级实现

## 相关文档

- [架构总览](./overview.md) · [安全模型](./security-model.md) · [能力矩阵](../internals/capability-matrix.md)
- [配置详解](../guides/configuration.md) · [构建指南](../guides/build.md) · [ADR 汇总表](../adr/README.md)
