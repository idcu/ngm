# third-party-engine 示例

一个完全自足的外部引擎样例：项目自带引擎清单与一个最小 subprocess 脚本，证明协议不依赖任何预置引擎。权威契约只有一个：[P4 — 生态与协议](../../docs/architecture/engine-adapter.md)；本文只给最短上手命令。

## 命令序列

```bash
ngm config validate
ngm engines validate
ngm engines list --json
ngm build --engine=thirdparty-bundle --outfile=dist/index.js
```

`engines validate` 会实际 spawn 脚本的 `--version` 探测；本机没有 `bash` 时会如实报不可用，而不是假装通过。`build --engine=thirdparty-bundle` 走清单里的 `command`，把入口作为位置参数、`--kind=bundle` 与 `--outfile=` 交给脚本，产物写入 `dist/index.js`。

## 目录

- `ngm.json` — schemaVersion 3，`engines.bundle` 指向 `thirdparty-bundle`
- `ngm.engines.json` — 自带清单，唯一一条 `thirdparty-bundle` 的 subprocess 条目
- `bin/thirdparty-bundle.sh` — 遵守 P4 的最小脚本：版本探测、能力分派、stdout/文件产物、退出码
- `src/index.ts` — 最小入口
- `smoke.sh` — 离线冒烟骨架（不下载任何工具）

## 边界

脚本只演示协议，不做打包/压缩；它不写 `ngm.lock`，也不把证明当成产物认证。要接入真正的工具，照 [P4 用户自定义引擎](../../docs/architecture/engine-adapter.md) 替换 `command` 即可。
