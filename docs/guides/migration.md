# 迁移指南

ngm 只管理 Git 依赖与本地来源；registry 包继续用 pnpm / npm / yarn。本文给出从现有项目接入 ngm 的路径，以及旧清单 / lock 的迁移入口。

## 从 npm / yarn / pnpm 接入

把 Git 依赖从 `package.json` 搬到 `ngm.json`，registry 依赖留在原地：

```json
// package.json
{ "dependencies": { "express": "^4.21.0" } }
```

```json
// ngm.json
{
  "schemaVersion": 3,
  "name": "github.com/my-org/my-app",
  "version": "0.1.0",
  "runtime": "node",
  "dependencies": [
    { "name": "github:my-org/utils", "ref": "v1.2.3", "refType": "tag" }
  ]
}
```

```bash
ngm init github.com/my-org/my-app --runtime=node
ngm add github:my-org/utils@v1.2.3 --ref-type=tag
ngm install
ngm config validate
ngm build --engine=esbuild
```

之后让构建工具读 `ngm.mappings.json`（见[构建指南](./build.md)）。

## 从 lytd 接入

lytd 的构建与类型检查能力由引擎接管，其余交给专业工具：

| lytd 能力 | ngm 的做法 |
|---|---|
| 构建（bundle / transform） | engine adapter → esbuild；deno 需自行声明 |
| 类型检查 | engine adapter → tsc / deno |
| CSS | engine adapter → postcss |
| 测试 | Vitest / Deno test |
| 文档 | TypeDoc / Deno doc |
| Dev Server / HMR | Vite / Deno |
| LSP | tsserver / Deno LSP |

把 `lytd build` 换成 `ngm build --engine=esbuild`，`lytd typecheck` 换成 `ngm typecheck --engine=typescript`。ngm 不替代 lytd 的全部能力，只接管可证明的依赖与构建入口。

## 旧清单与旧 lock

`schemaVersion` 只接受 3，v1/v2 清单会被 exit 3 拒绝；2.x lock 用 `ngm migrate` 迁移：

```bash
ngm migrate --dry-run   # 只输出变更与备份路径，不写文件
ngm migrate             # 落地：原件另存为 .bak
ngm config validate
```

迁移只搬能安全映射的字段；缺 digest 的条目列入 `skipped` 且全量不写，需要人工重装。已是 v3 时幂等返回 0。`ngm lock migrate` 这个命令并不存在——3.x 的 lock 直接废止旧版，重新生成即可。

## 边界

- 迁移有摩擦：需要同时维护 `package.json` 与 `ngm.json`。
- ngm 不是 drop-in 替换：测试、文档、Dev Server 都要换工具。
- 纯 registry 依赖的项目没有必要迁移；只有 Git 依赖较多、需要可证明性时才值得。
- mappings 是桥接层，增加一层间接性，但让外部工具能解析 vendor 里的裸导入。

## 相关文档

- [依赖管理](./dependency-management.md) · [构建](./build.md) · [配置详解](./configuration.md)
- [生态定位](./positioning.md) · [ADR 索引](../adr/README.md)
