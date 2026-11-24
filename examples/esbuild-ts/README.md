# esbuild-ts 示例

权威说明只有一个：[示例工程总览](../../docs/guides/examples.md)。本文件只给最短上手命令，不复制那份指南的正文。

## 命令序列

```bash
ngm config validate
ngm install
ngm build --engine=esbuild
ngm transform src/index.ts --engine=esbuild --outfile=dist/index.transformed.js
ngm integrations add esbuild
ngm mappings validate
```

`build` 负责 bundle，`transform` 只处理单文件；`integrations add esbuild` 从 mappings 生成 esbuild 配置，`mappings validate` 对账 lock 与 vendor。

## 离线边界

`smoke.sh` 是骨架：只做本地可做的检查，不联网、不下载工具；需要 esbuild 已在本机 PATH 上才会走到构建步骤。

## 目录

- `ngm.json` — schemaVersion 3 清单，声明 esbuild 为 bundle / transform 引擎
- `src/index.ts` — 最小入口
- `smoke.sh` — 离线冒烟骨架
