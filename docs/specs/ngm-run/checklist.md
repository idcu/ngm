# Checklist：`ngm run` / D1 / D2 / D3 / 发布核实

- [x] `ngm run <script>` 在 Windows 上真跑过：脚本退出 0、脚本失败退出码原样透传
- [x] `pre<name>` / `post<name>` 顺序执行，首个失败即停并返回它的码
- [x] `--if-present` 在脚本缺失时退 0 并打印 note
- [x] `--list` 人类输出与 `--json` 报告同源，且 `--json` 可解析
- [x] 参数透传：`--` 之后的参数原样追加到主脚本命令串末尾（pre/post 不收）
- [x] `node_modules/.bin`（含祖先，近者在前）注入子进程 PATH 前端
- [x] 缺 `.bin` 时失败会输出诊断，而不是让用户猜
- [x] `package.json.scripts` 权威、`ngm.json.scripts` 回退；同名两处 exit 3 且点名两处
- [x] 退出码契约：子进程码不映射到 0–6；POSIX 信号 → 128+sig（SIGINT 130）
- [x] 单一 `run:script` 闸门，默认 deny；拒绝 hint 含可粘贴 JSON 与两套闸门说明
- [x] `run` 未接入 install / verify / CI 自动流程
- [x] D1：`NGM_HOME` 真的改变全局配置解析位置，含 config 单测与 `config show` 端到端
- [x] D2：README 不再硬编码 tag 数，发布守卫不依赖该数字
- [x] D3：adopt 对 `@scope/name` 回退保留 scope，含 subpath 与 `#sha` 组合单测
- [x] 发布核实：`--drift-guard` 读数已归档，缺口与阻塞原因写明
- [ ] 收尾：`go build ./...`、`go vet ./...`、`go test -count=1 -timeout 40m ./...` 全绿
- [ ] 收尾：`bash scripts/check-docs-links.sh` 通过；`testdata/help.golden` 已重生成
- [ ] 两仓分批提交并推送（无强推 / 无历史改写 / 无 git config 改动）
