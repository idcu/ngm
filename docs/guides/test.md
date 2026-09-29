# 测试

> ngm core **不做自研测试框架**。测试交给 Vitest / Deno test / Node test。

---

## 为什么 ngm 不做 test runner

| 竞品 | 现状 | ngm 自研的必要性 |
|------|------|----------------|
| Vitest | 已成熟，Vite 生态一等公民 | 无 |
| Deno test | 内置，零配置 | 无 |
| Node test runner | Node 内置 `--test` | 无 |
| Jest | 生态庞大 | 无 |

ngm 的差异化在**依赖证明层**，不在测试框架。

---

## Node 项目：用 Vitest

### 安装

```bash
pnpm add -D vitest
```

### 配置

```ts
// vitest.config.ts
import { defineConfig } from 'vitest/config'
import mappings from './ngm.mappings.json'

export default defineConfig({
  resolve: {
    alias: mappings.mappings.map(m => ({
      find: m.from,
      replacement: m.to
    }))
  }
})
```

### 运行

```bash
vitest run
```

---

## Deno 项目：用 deno test

### 运行

```bash
deno test --allow-read
```

### 权限

```bash
# 只允许读 vendor 目录
deno test --allow-read=./ngm.vendor
```

---

## ngm 的测试价值在依赖证明

虽然 ngm 不做 test runner，但 ngm 保证：

> 测试跑的代码，是**可证明的**代码。

```bash
# 1. 锁定依赖
ngm install

# 2. 检查漂移
ngm verify

# 3. 审计漏洞（v0.2；现在跑会得到 exit 3）
# ngm audit

# 4. 跑测试（用 Vitest / Deno test）
vitest run
```

---

## 测试中的 vendor 隔离

测试代码引用 Git 依赖时，通过 mappings 解析：

```ts
// test/utils.test.ts
import { helper } from 'github:my-org/utils'

test('helper works', () => {
  expect(helper()).toBe('expected')
})
```

构建工具（Vitest/Vite/esbuild）读取 `ngm.mappings.json`，把 `github:my-org/utils` 映射到 vendor 路径。

---

## 诚实说明

1. **ngm 没有 test 命令**：`ngm test` 不存在，用 `vitest` / `deno test` / `node --test`
2. **ngm 的价值在测试前的依赖证明**：确保测试环境用的是可信代码
3. **不追求测试生态**：Vitest/Deno test 已经足够好

---

## 相关文档

- [构建](./build.md)
- [依赖管理](./dependency-management.md)
- [Node vs Deno](./node-vs-deno.md)
