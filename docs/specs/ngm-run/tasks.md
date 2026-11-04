# Tasks：`ngm run` / D1 / D2 / D3 / 发布核实

按批次执行，每批单独验证后再进入下一批。批次内可并行编辑，但测试必须真跑。

## 批次 1：run 骨架与执行

- [x] T1.1 `internal/runscript/runscript.go`：`Set`/`Script`、`Load`（package.json 权威 + ngm.json 回退 + 冲突 exit 3）、`BinDirs`（祖先 `.bin`，近者优先）、`Exec`（shell/参数/env）。
- [x] T1.2 `internal/runscript/exitcode_unix.go`、`exitcode_windows.go`：信号映射 128+sig；Windows 透传。
- [x] T1.3 `internal/runscript/runscript_test.go`：来源/冲突/回退、pre/post 命名、BinDirs 顺序、env 最小集、参数拼接、130 映射判据。
- [x] T1.4 `cmd/ngm/run.go`：flag（`--dir`/`--if-present`/`--list`/`--json`）、首个 `--` 切分、列表/JSON、执行。
- [x] T1.5 `cmd/ngm/run_test.go`：10 条黑盒（Windows 真跑；POSIX 分支用 `sh`）。
- [x] T1.6 验证：`go build ./...` + 定向 `go test`；记录 Windows 真跑输出与退出码。

## 批次 2：闸门、契约、文档

- [x] T2.1 `run:script` 检查点：执行前一次判定；拒绝 hint 含可粘贴 JSON + 两套闸门说明。
- [x] T2.2 `root.go` 命令表 + `help.go`（含“透传族”段）。
- [x] T2.3 契约网登记：`config_matrix_test.go`、`exit_codes_contract_test.go`（0/3/5 + 透传族）、`exit_codes_sources_test.go`、`doc_field_tables_test.go`、`doc_shape_reverse_test.go`。
- [x] T2.4 `ProjectFile.Scripts` 字段 + `docs/guides/configuration.md` 字段表。
- [x] T2.5 文档：README 命令表 27→28、`docs/guides/cli.md`、`security-model.md`、`observability.md`、ADR-042 + 索引、CHANGELOG Unreleased、评估文档 §7 状态。
- [x] T2.6 重生成 `testdata/help.golden`；验证 `bash scripts/check-docs-links.sh`；跑契约网定向测试。

## 批次 3：D1 / D2 / D3

- [x] T3.1 `config.EffectiveGlobalPath` + `LoadFrom`；`env.go`/`engine.go`/`config.go` 接线；`testenv_test.go` 写配置路径同步。
- [x] T3.2 D1 测试：config 单测 + `config show` 在 NGM_HOME-only 配置下生效。
- [x] T3.3 D2：README 两处去硬编码；确认 `check-release-status.sh` 不依赖该数字。
- [x] T3.4 D3：`adopt.fallbackName` 保留 scope；三条单测（scope / 嵌套 subpath / `#sha`）。
- [x] T3.5 验证：`go test ./internal/config ./internal/adopt ./cmd/ngm -run '...'`。

## 批次 4：发布核实

- [x] T4.1 `bash scripts/check-release-status.sh --drift-guard` 读数归档（tag/release/产物）。
- [ ] T4.2 缺产物且凭据可用 → 已有 workflow 对已存在 tag 幂等重跑（`--clobber` 由脚本处理）。
  - **阻塞**：本机无 `gh`、无 `GITHUB_TOKEN`/`GH_TOKEN`，GitHub 侧无凭据；`release.yml` 的
    `workflow_dispatch`（输入 `tag`）可直接对 `v0.102.0-dev.1` 重跑。未执行，未伪造附件。
- [x] T4.3 阻塞 → 报告写“未执行”，确认 `workflow_dispatch` 入口。

## 收尾

- [x] T5.1 `go build ./...`、`go vet ./...`、`go test -count=1 -timeout 40m ./...`、`bash scripts/check-docs-links.sh`。
- [x] T5.2 分批 commit + push（两仓按需；禁止强推/改历史/改 git config）。
- [x] T5.3 中文 Markdown 报告：结论先行，按用户要求分块。
