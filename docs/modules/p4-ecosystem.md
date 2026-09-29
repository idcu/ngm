# P4 — 生态与协议

> engine adapter 协议 + mappings 协议

---

## 职责

P4 定义 ngm 与外部世界的**协议**：engine adapter 协议、mappings 协议。

（构建工具的具体集成脚手架归 [P5 — 外部工具集成](./p5-integrations.md)。）

---

## 模块结构

```
internal/
├── adapter/             # engine adapter 协议
│   ├── protocol.go      # adapter 输入输出协议
│   └── discovery.go     # 引擎发现与版本检测
└── mappings/            # mappings 协议
    ├── schema.go        # JSON schema
    ├── generate.go      # 生成
    └── validate.go      # 校验
```

（完整模块树见 [P0 — ngm core](./p0-core.md)；外部集成模块见 [P5](./p5-integrations.md)。）

---

## engine adapter 协议

### subprocess 协议规范

ngm 与 engine 之间通过 subprocess 通信，约定：

#### 输入

| 方式 | 参数 | 适用 |
|------|------|------|
| stdin | `--stdin` | 小文件，避免临时文件 |
| 文件路径 | 直接传路径参数 | 大文件/多文件 |
| 配置文件 | `--config <path>` | 复杂配置 |

#### 输出

| 通道 | 内容 |
|------|------|
| stdout | 产物 / JSON 结果 |
| stderr | 日志 / 警告 / 错误 |
| 退出码 | 0 成功，非 0 失败 |

#### 选项 → argv 映射

P4 规定通道语义，**选项到命令行的映射**如下（这是 ngm 对协议的确定化，自定义引擎照此实现即可）：

| 输入 | 传给引擎的参数 |
|------|----------------|
| 能力类别 | `--kind=<transform\|bundle\|typeCheck\|typeDecl\|css>` |
| 输入（bundle / typeCheck / typeDecl） | 入口文件作为**位置参数**（放最后） |
| 输入（transform / css） | 内容走 stdin，并带 `--stdin` |
| 选项 | 每个键一个 `--<key>=<value>`，**按键排序**（保证 argv 可复现） |
| 输出 | `--outfile=<path>`；不带时产物走 stdout |
| 布尔选项 | `true` → `--<key>`（不带值）；`false` / `null` → 该参数不出现 |

两条纪律：

- **不经过 shell**。命令按空白切分（支持用双引号包裹含空白的片段，如 Windows 的
  `"C:\Program Files\tool.exe"`），随后直接 `execve`；管道、重定向、`$(...)`
  一律作为字面参数。清单随仓库进入 ngm，走 `sh -c` 等于把任意命令执行权交给任何一个 PR。
- **一个二进制可以注册多条清单条目**（如 esbuild 同时提供 `transform` 与 `bundle`）。
  `--kind` 就是让引擎区分"这一次该做什么"的唯一依据。

`esbuild` 有专门的分派（`--bundle` / `--alias:` / `--define:` 是它自己的语义）；
未登记名字的引擎走上面的通用映射。

#### 错误格式

ngm 解析引擎的错误输出，映射到统一错误：

```go
type EngineError struct {
    Code      int
    Message   string
    Stderr    string
    Retryable bool
    Fallback  bool
    Hint      string // ngm 侧扩展：给用户的下一步建议
}
```

映射到退出码（[可观测性](../architecture/observability.md)）：

- 引擎**不可用**（不在 PATH）→ `exit 5`，hint 给出具体安装命令
- 引擎**运行失败**（非零退出）→ `exit 1`，**原始 stderr 原样保留**（引擎的诊断比 ngm 的转述有用）

> 引擎自己的退出码**不透传**：ngm 的 0–5 是对外契约，让引擎的私有码穿透会与 ngm 自身语义冲突
> （某引擎恰好用 5 表示语法错误时，读日志的人会以为"引擎不可用"）。原码保留在消息与 `--json` 中。

`Retryable` 在 v0.1 只用于**告知**（hint 里写"重跑可能成功"），不驱动重试循环。

#### 用户自定义引擎

用户可以在 `ngm.engines.json` 声明自定义引擎：

```json
{
  "version": 1,
  "engines": [
    {
      "name": "my-transform",
      "kind": "transform",
      "adapter": "subprocess",
      "command": "my-transform-cli",
      "version": "0.1.0",
      "supportedInput": [".ts", ".tsx"],
      "defaultOptions": {}
    }
  ]
}
```

只要遵守 subprocess 协议，ngm 就能调用。

---

## mappings 协议

### schema

```json
{
  "version": 1,
  "mappings": [
    {
      "from": "github:my-org/utils",
      "to": "./ngm.vendor/github.com/my-org/utils",
      "main": "./index.js",
      "types": "./index.d.ts"
    }
  ]
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `version` | number | 是 | 协议版本，当前 1 |
| `from` | string | 是 | 依赖标识（Git URL 形式） |
| `to` | string | 是 | vendor 中的实际路径 |
| `main` | string | 否 | 入口文件 |
| `types` | string | 否 | 类型声明文件 |

### 生成

`ngm install` 后自动生成 `ngm.mappings.json`。

### 校验

```bash
ngm mappings validate
```

检查：

- `from` 是否在 ngm.lock 中
- `to` 路径是否存在
- `main` / `types` 文件是否存在

---

## 外部工具集成

各构建工具（Vite / esbuild / Deno / Webpack）消费 mappings 的接入示例与集成脚手架，唯一维护在：

- 用户侧接入示例：[构建指南](../guides/build.md)
- 集成模块与命令：[P5 — 外部工具集成](./p5-integrations.md)

---

## integrations 模块

集成脚手架模块与 `ngm integrations` 命令见 [P5 — 外部工具集成](./p5-integrations.md)。

---

## 诚实说明

1. **mappings 是一层间接性**：增加复杂度，但必要
2. **ngm 的价值在依赖证明**：mappings 只是让构建工具能找到 vendor 里的代码
3. **不追求插件数量**：ngm 的插件生态不可能与 Vite 比

---

## 成熟度

| 子模块 | 成熟度 | 备注 |
|--------|--------|------|
| adapter 协议 | done (v0.1) | subprocess 协议 |
| mappings schema | done (v0.1) | |
| mappings 生成 | done (v0.1) | |
| 外部工具集成（Vite / esbuild / Deno / Webpack） | planned (v0.3) | 见 [P5](./p5-integrations.md) |

> 成熟度口径与唯一事实源[能力矩阵](../internals/capability-matrix.md)一致。

---

## 相关文档

- [P2 — 构建引擎](./p2-build.md)
- [P5 — 外部工具集成](./p5-integrations.md)
- [架构：引擎 adapter](../architecture/engine-adapter.md)
- [架构：运行时模型](../architecture/runtime-model.md)
- [构建指南](../guides/build.md)
