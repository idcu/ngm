# 测试

ngm core **不做自研测试框架**：测试交给 Vitest / Deno test / Node test，ngm 的差异化在依赖证明层。

## 怎么做

Node 项目用 Vitest：

```bash
pnpm add -D vitest
vitest run
```

Deno 项目用内置测试运行器：

```bash
deno test --allow-read=./ngm.vendor
```

测试代码里的裸导入通过 mappings 解析——Vitest / Vite / esbuild 读 `ngm.mappings.json`，把 `github:my-org/utils` 映射到 vendor 路径。

```ts
// vitest.config.ts
import mappings from './ngm.mappings.json'
import { defineConfig } from 'vitest/config'
export default defineConfig({
  resolve: {
    alias: mappings.mappings.map(m => ({ find: m.from, replacement: m.to }))
  }
})
```

## 测试前的证明链

ngm 的价值在测试跑之前：确保被测代码是可证明的代码。

```bash
ngm install     # 锁定依赖
ngm verify      # 检查漂移
ngm audit       # 审计已知漏洞（需网络；--offline 只读缓存）
vitest run      # 用外部框架跑测试
```

`ngm audit` 走 OSV.dev，按 commit 查询并缓存；查询失败 exit 4，不会把失败伪装成"干净"。

## 边界

1. **ngm 没有 test 命令**：`ngm test` 不存在，用 `vitest` / `deno test` / `node --test`。
2. ngm 不追求测试生态：Vitest / Deno test / Node test 已经足够好。
3. 测试框架之间不共享"可证明"语义——那是 `ngm.lock` 与 `ngm verify` 的职责，与用哪个 runner 正交。

## 相关文档

- [构建](./build.md) · [依赖管理](./dependency-management.md) · [CLI 参考](./cli.md)
