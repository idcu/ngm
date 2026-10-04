# ADR-025：引擎选择**不设**内置默认

- **状态**：已定（2026-10-04）
- **范围**：`typecheck` / `typedecl` / `css` 三个 kind 在没有声明引擎时该怎么做
- **上游**：[ADR-012](./adr-012-sandbox.md)（沙箱与自检）· [ADR-020](./adr-020-remote-adapter-shelved.md)（"不做"如何写）· [engine-adapter.md](../architecture/engine-adapter.md)

> **结论**：`typeCheck` / `typeDecl` / `css` **没有内置默认引擎**，这是**设计而不是待办**。
> 缺声明时的报错**必须点名 kind、列出目录里的候选、并指向 `ngm.engines.json`**。
> 本版不改行为，只把这件事写成决定——因为"等一个默认值"不是决定，
> 它只是把问题留在没人负责的地方。

---

## 背景：一个已经挂在候选列表里两版的问题

v0.14 修文档时发现："esbuild 能做 css"这个说法被写错了三处。
改正文档时顺带确认了机制：**这三个 kind 需要一个引擎，而目录里没有预置**。
于是它成了"要不要给个内置默认"这个待决项，一挂就是两版（v0.15、v0.16、v0.17 都列了它）。

本轮实测（在本机跑真实 `ngm`，不构造）：

```
$ ngm typecheck                → exit 3   ConfigInvalid: no engine configured for `typeCheck`
$ ngm typedecl                 → exit 3   ConfigInvalid: no output directory
$ ngm css                      → exit 3   ConfigInvalid: no input file
$ ngm typecheck --engine=esbuild
                                → exit 3   ConfigInvalid: no `typeCheck` engine named "esbuild" in the catalog
```

## 决策：不做。理由三条

1. **默认会随"机器上装了什么"静默变化**
   今天唯一可用的是 A，明天装了 B，默认就换成 B：同一份项目、同一条命令，行为变了，
   而**没有任何文件被改动**。这与 ADR-018 拒掉可达性删除是同一个形状——
   **需要一种项目并不拥有的信息**（这里是"本机装了哪些引擎"）来决定的行为，不该成为默认。

2. **引擎选择决定"产物的字节来自哪里"，属可证明性的一部分**
   它必须由**项目里的一份文件**决定（`ngm.engines.json`），
   而不是由执行环境决定。**可预期 > 顺手**。

3. **报错本身已经可行动**
   `no engine configured for typeCheck` + hint 指向 `ngm.engines.json`；
   目录里只有 `typescript` 时，`engines list` 会把它（以及它的可用性）列出来。

## 代价与配套

- 新用户第一次跑 `ngm typecheck` 会撞到 exit 3。
- 配套：在 [CLI 参考](../guides/cli.md) 的 `typecheck` / `typedecl` / `css` 三行写明
  "没有内置默认，需要先在 `ngm.engines.json` 里声明"。
- 不做：为了"第一次就能跑"而引入自动探测/自动选择。

---

## 什么条件下重新考虑

1. 出现**真实案例**：有人因为缺默认而卡住（而不是"猜他可能会卡住"）。
2. 引擎选择**进入 lock**（那时它就有一个项目级的事实来源，默认才有据可依）。

---

## 相关文档

- [ADR-012](./adr-012-sandbox.md) · [ADR-020](./adr-020-remote-adapter-shelved.md)（同样是把"不做"写清）
- [engine-adapter.md](../architecture/engine-adapter.md)（目录与内置引擎）· [CLI 参考](../guides/cli.md)
- [v0.19 复盘](../development/v0.19-retrospective.md)
