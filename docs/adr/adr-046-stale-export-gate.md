# ADR-046：过期 lock 的审计产物默认拒绝导出

- **状态**：已定（2026-11-20）
- **范围**：`ngm sbom` / `ngm attest` 在 `ngm.json` 与 `ngm.lock` 不一致时的行为
- **背景**：v0.81 起两条命令在 lock 过期时写一条 stderr warning，然后照常导出。
  对 `attest` 这尤其危险——它的产物**就是拿来签名的**，CI 常把 stdout 直接管道给签名工具，
  stderr 的 warning 最容易被忽略。于是签下一份"结构合法、哈希正确、描述的却是不存在状态"的语句；
  签名之后没有任何机制能发现它。README 对 attest"同一 lock 产出相同字节"的强调反而放大了风险：
  过期 lock 的稳定字节看起来完全可信。

## 决策

1. **默认拒绝**：`lockManifestMismatch` 命中时，`sbom` / `attest` 以 exit 3 结束，不写任何产物。
   错误消息点名不一致的依赖，并给出可执行出路（`ngm install` 刷新 lock）。
2. **显式逃生口 `--allow-stale`**：确实要导出一份"描述过期 lock"的产物时，调用方必须显式给出
   这个 flag；此时仍把事实写进 stderr，措辞标明"仅因 --allow-stale 才导出"。
3. **不把新鲜度写进产物字节**：`lockStale` 不作为字段塞进 statement / SBOM——那会破坏
   "同一 lock 产出相同字节"的确定性（ADR-045 的产物档）。新鲜度由**退出码 + stderr** 表达。
4. 两条命令同一门槛，不因"SBOM 不像 attestation 那样显然要签名"而放宽：SBOM 同样会被
   归档、比对、喂给下游合规流程。

## 理由

- 签名是不可逆的信息释放：宁可拒绝导出，也不要让用户签一份自己没注意已经过期的证据。
- "写 warning 然后照做"把安全判断托付给最容易被自动化的通道（stderr），
  这与本项目一贯的"机器可读的结论必须在 stdout / 退出码里"相反。
- 逃生口是显式的、可审计的（记在命令行里），而不是靠环境或默认值偷偷放行。

## 后果

- 既有 CI 若在 lock 过期时依赖 sbom/attest 的零退出，会开始失败——这正是期望的信号：
  它说明该跑 `ngm install` 了。需要保留旧行为的流水线必须显式加 `--allow-stale`。
- `docs/guides/cli.md` 的两条命令 EXIT CODES 与 `CHANGELOG.md` 同步更新。
- 判据：`TestV136StaleLockExportIsRefused` 构造改过 ngm.json 的夹具，断言默认 exit 3、
  无产物、`--allow-stale` 才产出。

## 相关文档

- [ADR-045：--json 的契约层级](./adr-045-json-output-contract.md)
- [ADR-027：attestation](./adr-027-attestation.md)
- [CLI 参考](../guides/cli.md)
