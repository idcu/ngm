# ADR-045：`--json` 的契约层级——解码语义是契约，字节不是

- **状态**：已定（2026-11-19）
- **范围**：所有 `--json` 的成功输出与错误信封；`sbom` / `attest` 的产物另论
- **背景**：评估报告把"收敛多套 JSON encoder"（F1）列成待办，却一直没做——因为
  做之前得先回答一个没人回答的问题：**`--json` 的字节算不算契约？**
  实际状态是四套实现（`writeJSON` / `writeModulesJSON` / `writeIntegrationsJSON` /
  `gc` 内联），三种错误文案，两种转义策略（`gc` 用 `SetEscapeHTML(false)`，
  其余走默认），而没有任何一处写下"哪种才是对的"。

## 决策

1. **契约层级是解码后的语义**：字段名、类型、`version`、数组顺序无关性、错误信封的形状
   （`{"version":1,"error":{"code","exitCode","message","hint"}}`）、退出码。
   这些由判据守着（`json_contract_test` / `json_error_envelope_test` / 各命令的形状表）。
2. **字节不是契约。** 报告类 `--json` 统一用 Go 的默认编码：`json.MarshalIndent(v, "", "  ")`
   加末尾一个 `\n`，**含 HTML 转义**（`&` `<` `>` 写成 `\u0026` 等）。
   理由：这是语言默认、9 处里 8 处已经如此，且解码后与不转义**完全等价**。
   把它写成一条决策，是为了让"要不要统一"不再每轮重开一次。
3. **按输出类别分三档，不按命令名分**：

   | 类别 | 谁 | 转义 | 理由 |
   |---|---|---|---|
   | **产物** | `sbom` / `attest` 的输出 | 不转义 | 字节本身是交付物，要被人比对、签名、算哈希 |
   | **报告** | 所有 `--json` 的成功输出 | 转义（Go 默认） | 解码语义是契约；字节只是实现 |
   | **错误信封** | `writeJSONError` | 不转义 | 它装的是**一句人话**（常含路径），`\u003c` 会让人读原始输出时更糟 |

   判别标准是"这份字节会不会被当成证据拿去比"，而不是"这个命令叫什么"。
4. **一个实现**：`cmd/ngm/json_document.go` 的 `writeJSONDocument` 是 stdout 上唯一的
   JSON 写出点。失败统一报 `write report: …` 并退 6（ADR-026：结论已得出、送不出去）。
5. **按字节 diff `--json` 是未定义行为。** 需要稳定字节的场合用产物而不是报告
   （`attest`、`sbom`、`ngm.lock` 本身就是字节稳定的）。

## 后果

- `ngm gc --json` 的字节会变（只在字符串含 `&` `<` `>` 时），**解码后不变**。
- F1 关闭：四套实现合成一个，`json.NewEncoder` 在 `cmd/ngm` 里除了错误信封
  （`main.go`）与这个文件之外不再出现，由判据守着。
- 报告的类型仍可以有自己的 `Marshal()`（`writeReport` 走它）——那是"谁能决定字节"
  的分层：**产物**自己决定，**报告**用默认编码。

## 补记（2026-11-21，逐行审计 Batch B）

1. **失败路径 stdout 恰好一份错误信封，不写成功报告**（审计 A-01）。此前 `install --json` 把
   重定向后的 `humanOut`（stderr）当作 `runErr` 的 stdout 形参，错误信封进了人读通道，
   而 `emitWriteReport` 仍往真正的 stdout 写一份成功形状的 `writeReport`。
   现在 `installFresh` / `installFromLock` / `autoVerifyAfterLock` 各有独立的 `errOut`，
   `jsonErrorSink` 记录"信封已写"并在收尾时抑制报告。**这不是按退出码抑制**：
   `verifyOnLock` 的漂移 exit 1 是结论不是错误，报告仍写。
2. **"接受 `--json` 的命令，任何参数级失败都给信封"成为统一规则**（审计 A-02/A-05）。
   `modules`（7 处）与 `integrations`（1 处）的早期失败改走 `c.rejectExtraArgs`；
   `modules` 顶层无/未知子命令用 `failEarly`。行为级枚举判据覆盖所有 JSON-aware 命令，
   不再逐命令补漏（P1-4 只迁移 5 个命令的教训）。
3. **`modules index --dry-run --json` 与正常路径同 schema**（审计 A-06）：都写
   `modulesIndexReport`，dry-run 仅以 `dryRun:true` 区分；裸 `collectionIndex` 只在非 `--json` 输出。
4. **`config validate` 的成功文本必须最后打印**（审计 A-03）：非法权限项此前先打印
   `ngm.json OK` 再 exit 3，按 stdout 文本判定的 CI 会误绿。
5. **SARIF / GitHub 注解级别跟最终退出码同源**（审计 K-01）：`--allow-drift` 下的
   unexpected 降到 warning，`--strict` 下的 expected 升到 error；不能只按 DriftKind 渲染。
   JSON 报告里的 `strict` / `allowDrift` 字段由此有了对应的机器可读结论。

## 重启条件

1. 出现**按字节**比对报告 JSON 的消费者（CI 缓存键、签名、diff 工具）；
2. 有人需要"报告也字节稳定"来复现某次判定——那时应当把它做成一份**产物**
   （新命令或新文件），而不是把报告的编码策略改回去。
