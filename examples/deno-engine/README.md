# deno-engine 示例

权威说明只有一个：[示例工程总览](../../docs/guides/examples.md)。本文件只给最短上手命令，不复制那份指南的正文。

## 命令序列

```bash
ngm config validate
ngm engines validate
ngm engines list --json
ngm typecheck --engine=deno
ngm build --engine=deno
```

Deno 不在内置清单里，因此本目录自带 `ngm.engines.json`，显式声明 `typeCheck` 与 `bundle` 两条 subprocess 条目；`engines validate` 在本机没有 Deno 时会如实报不可用，而不是假装通过。

## 离线边界

`smoke.sh` 是骨架：不联网、不下载 Deno；只在 Deno 已在本机时继续走类型检查与构建。

## 目录

- `ngm.json` — schemaVersion 3，`runtime: deno`
- `ngm.engines.json` — 显式 Deno 引擎声明
- `src/index.ts` — 最小入口
- `smoke.sh` — 离线冒烟骨架
