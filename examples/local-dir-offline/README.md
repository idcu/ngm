# local-dir-offline 示例

权威说明只有一个：[示例工程总览](../../docs/guides/examples.md)。本文件只给最短上手命令，不复制那份指南的正文。

## 命令序列

```bash
ngm config validate
ngm install --offline --frozen-lockfile
ngm verify --offline --content
ngm mappings validate
```

依赖来自仓库内的 `vendor-src/fixture-pkg`（`local-dir` 来源），`ref: auto` 让 ngm 计算目录内容摘要。首次生成本地锁文件后把它提交，其它机器与流水线即可用上面的命令全离线复验。

## 离线边界

`smoke.sh` 是骨架：不向网络发起请求，也不下载工具；缺少本地锁文件时只打印跳过说明。

## 目录

- `ngm.json` — schemaVersion 3，含一条 local-dir 声明
- `vendor-src/fixture-pkg/` — 被依赖的最小本地目录
- `smoke.sh` — 离线冒烟骨架
