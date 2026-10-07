package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// TestV16JSONReportsTellTheTruth 固定"机器可读输出不能说谎"的契约。
//
// 7 个命令带 `--json`（verify / audit / tree / why / outdated / engines / integrations），
// 而它们的报告是**给脚本读的**。这个契约此前只有零散覆盖，且**没有任何一条断言
// 把"报告里写的退出码"与"进程真实的退出码"绑在一起**——三处 payload 都带
// `exitCode` / `summary.exitCode` 字段（三份测试分别只查了"键存在"或"某个场景里等于 0"）。
//
// 每次调用都必须满足四条：
//
//	① stdout 要么为空，要么是**恰好一份**合法 JSON 文档——不允许半份文档；
//	② **退出码与不带 --json 时相同**（同一夹具、同一状态）；
//	③ 报告里若带 `exitCode` / `summary.exitCode`，它**必须等于进程退出码**；
//	④ 输入错误（依赖不在图里、引擎不在目录里、工具名不合法）时，stdout **要么为空、
//	   要么是恰好一份错误信封**：
//	   · **空**：这个命令根本不接受 `--json`（出错时不该假装有机器可读输出）；
//	   · **错误信封**（v0.51 起）：命令接受 `--json` 而这次失败 ⇒ 一份**完整**的
//	     `{"version":1,"error":{code,exitCode,message,hint}}`。
//
//	   规矩的精神没变——**"我没有报告可给"不能用半份 JSON 表达**：
//	   报告仍然不许在失败时出现半份；而**错误信封不是报告**，
//	   它说的是"我失败了、原因与下一步在这里"。
//
//	   为什么改（v0.51 实测）：在此之前，脚本在失败路径上只能拿到
//	   "退出码 + 空的 stdout + 一段人读文本"——知道出了事，却读不出是什么事。
//
// 第 ③ 条是本版的核心，也是**最容易被静默破坏**的一条：任何人改了闸门逻辑，
// 进程退出码会变，而报告里那个字段（如果没人绑过）会继续写旧值——
// 那正是本项目最反对的形状：**读数在说谎，而所有检查都是绿的**。
//
// 状态序列是刻意排序的（干净 → 漂移 → 篡改），因为后两种状态会互相掩盖：
// 先篡改会让后面所有 verify 都退 2，漂移语义就再也测不出来了（v0.16 的探针踩过一次）。
func TestV16JSONReportsTellTheTruth(t *testing.T) {
	home := isolateUserEnv(t)
	testutils.MustHaveGit(t)

	// 夹具：一个 tag 依赖 + 一个 branch 依赖 + 一个只用于篡改的文件。
	up := testutils.NewGitRepo(t)
	up.WriteFile("index.ts", "export const dep = 1\n")
	up.Commit("feat: dep")
	up.Tag("v1.0.0", false)
	seedMirror(t, "github:v16/dep", up.Dir)

	branch := testutils.NewGitRepo(t)
	branch.WriteFile("lib.ts", "export const lib = 1\n")
	branch.Commit("feat: lib")
	seedMirror(t, "github:v16/lib", branch.Dir)

	proj := newProject(t)
	for _, a := range [][]string{
		{"add", "github:v16/dep@v1.0.0", "--ref-type=tag", "--dir=" + proj},
		{"add", "github:v16/lib@main", "--ref-type=branch", "--dir=" + proj},
		{"install", "--dir=" + proj},
	} {
		if code, out := runCaptureCode(t, a...); code != 0 {
			t.Fatalf("%v exit=%d:\n%s", a, code, out)
		}
	}

	// check 跑一条命令的两种模式，逐条核对四条契约。
	check := func(label string, args ...string) int {
		t.Helper()
		jsonArgs := insertJSON(args)

		plainStdout := &bytes.Buffer{}
		plainStderr := &bytes.Buffer{}
		plainCode := dispatch(args, plainStdout, plainStderr)

		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		code := dispatch(jsonArgs, stdout, stderr)

		// ① stdout 是空，或恰好一份合法 JSON。
		//
		// 判据是"**一份合法 JSON 文档**"，不是"一个对象"：`engines list --json` 是
		// **数组**（代码注释里写明 engineRow 是它的元素），而其余六个命令是对象。
		// 这个差异是当前机器接口的一部分——**本网固定它，但不去统一它**
		// （统一形状是破坏性变更，且要另立决定）。
		trimmed := bytes.TrimSpace(stdout.Bytes())
		var doc map[string]any
		if len(trimmed) > 0 {
			var top any
			if err := json.Unmarshal(trimmed, &top); err != nil {
				t.Errorf("%s: stdout is not a single JSON document: %v\n%s", label, err, trimmed)
			} else {
				doc, _ = top.(map[string]any)
				if doc == nil {
					t.Logf("%-46s top-level shape: %s", label, shapeOf(top))
				} else if len(doc) > 0 {
					t.Logf("%-46s top-level keys: %v", label, docKeys(doc))
				}
			}
		}

		// ② --json 不改退出码。
		if code != plainCode {
			t.Errorf("%s: exit=%d with --json but %d without — a flag must not change the verdict\n%s",
				label, code, plainCode, firstLine(plainStderr.String()))
		}

		// ③ 报告里的退出码必须等于进程退出码。
		if reported, ok := docExitCode(doc); ok && reported != code {
			t.Errorf("%s: the report says exitCode=%d but the process exits %d — the machine-readable "+
				"output is lying\n%s", label, reported, code, trimmed)
		}

		// ④ 出错时不允许留下半份报告：非零码且 stdout 非空，只在报告本身是结论时允许。
		// （verify 的失败态**就是**一份报告，所以这条不能写成"非零码 ⇒ stdout 为空"。）
		t.Logf("%-46s plain=%d json=%d stdout=%dB", label, plainCode, code, len(trimmed))
		return code
	}

	// ---- 干净状态 ----
	if got := check("verify (clean)", "verify", "--dir="+proj); got != 0 {
		t.Fatalf("verify on a clean project exit=%d", got)
	}
	check("verify --deep (clean)", "verify", "--deep", "--dir="+proj)
	check("tree", "tree", "--dir="+proj)
	check("why <dep in graph>", "why", "github:v16/dep", "--dir="+proj)
	check("outdated --offline", "outdated", "--offline", "--dir="+proj)
	check("engines list", "engines", "list", "--dir="+proj)
	check("engines info esbuild", "engines", "info", "esbuild", "--dir="+proj)
	check("engines validate", "engines", "validate", "--dir="+proj)
	check("integrations add vite", "integrations", "add", "vite", "--dir="+proj)
	check("integrations add vite (up to date)", "integrations", "add", "vite", "--dir="+proj)

	// ---- audit：成功与"发现漏洞"两条路都必须给出完整报告 ----
	//
	// audit 是本组里唯一**报告内容会改变退出码**的命令（0 = 无超阈值漏洞，1 = 有），
	// 也正是"报告里的 exitCode 必须等于进程退出码"最容易失守的地方。
	// 用 `--no-cache` 让两条路互不干扰（缓存是按 commit 存的）。
	cleanSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(cleanSrv.Close)
	t.Setenv("NGM_OSV_URL", cleanSrv.URL)
	grantNetFor(t, home, cleanSrv)
	if got := check("audit (clean)", "audit", "--no-cache", "--dir="+proj); got != 0 {
		t.Errorf("a clean audit must exit 0; exit=%d", got)
	}

	vulnSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"vulns":[{"id":"GHSA-v16-1","summary":"v16 finding","severity":"HIGH"}]}`)
	}))
	t.Cleanup(vulnSrv.Close)
	t.Setenv("NGM_OSV_URL", vulnSrv.URL)
	grantNetFor(t, home, vulnSrv)
	if got := check("audit (one HIGH finding)", "audit", "--no-cache", "--dir="+proj); got != 1 {
		t.Errorf("a known vulnerability above the threshold must exit 1; exit=%d", got)
	}

	// ---- 输入错误：stdout 要么为空、要么是恰好一份**错误信封**（v0.51 起） ----
	for _, c := range []struct {
		label string
		args  []string
		want  int
	}{
		{"why <dep not in graph>", []string{"why", "github:v16/nope", "--dir=" + proj}, 3},
		{"engines info <not in catalog>", []string{"engines", "info", "v16-no-such", "--dir=" + proj}, 5},
		{"integrations add <bad tool>", []string{"integrations", "add", "v16-no-such", "--dir=" + proj}, 3},
	} {
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		code := dispatch(insertJSON(c.args), stdout, stderr)
		if code != c.want {
			t.Errorf("%s: exit=%d want %d\n%s", c.label, code, c.want, stderr.String())
		}
		body := bytes.TrimSpace(stdout.Bytes())
		if len(body) == 0 {
			continue // 空是允许的（命令不接受 --json 的情形）
		}
		var doc struct {
			Version int `json:"version"`
			Error   *struct {
				Code     string `json:"code"`
				ExitCode int    `json:"exitCode"`
				Message  string `json:"message"`
				Hint     string `json:"hint"`
			} `json:"error"`
		}
		if derr := json.Unmarshal(body, &doc); derr != nil {
			t.Errorf("%s: stdout is neither empty nor one JSON document: %v\n%s",
				c.label, derr, stdout.String())
			continue
		}
		if doc.Error == nil {
			t.Errorf("%s: 失败时 stdout 只能是一份**错误信封**，而它是一份报告（没有 error 段）:\n%s",
				c.label, stdout.String())
			continue
		}
		if doc.Error.ExitCode != code {
			t.Errorf("%s: 信封里写着 exitCode=%d，而进程退 %d——机器可读输出在说谎",
				c.label, doc.Error.ExitCode, code)
		}
	}

	// ---- 漂移：branch 前进 ⇒ expected（默认不失败，--strict 才失败）----
	branch.WriteFile("lib.ts", "export const lib = 2\n")
	branch.Commit("feat: lib 2")
	seedMirror(t, "github:v16/lib", branch.Dir)

	if got := check("verify (expected drift)", "verify", "--dir="+proj); got != 0 {
		t.Errorf("an expected branch advance must not fail verify by default; exit=%d", got)
	}
	if got := check("verify --strict (expected drift)", "verify", "--strict", "--dir="+proj); got != 1 {
		t.Errorf("--strict must turn an expected advance into exit 1 (RefDrift); exit=%d", got)
	}
	if got := check("verify --allow-drift", "verify", "--allow-drift", "--dir="+proj); got != 0 {
		t.Errorf("--allow-drift must keep the default verdict; exit=%d", got)
	}

	// ---- 完整性：篡改 vendor 字节 ⇒ exit 2 ----
	tamperVendorFile(t, filepath.Join(proj, "ngm.vendor"))
	if got := check("verify (tampered bytes)", "verify", "--dir="+proj); got != 2 {
		t.Errorf("tampered vendor bytes must be a digest mismatch (2); exit=%d", got)
	}
	if got := check("verify --deep (tampered bytes)", "verify", "--deep", "--dir="+proj); got != 2 {
		t.Errorf("--deep on tampered bytes must be 2; exit=%d", got)
	}
}

// insertJSON 在子命令之后插入 --json：`ngm <cmd> --json …`。
// 这条形状对全部 7 个命令都成立（子命令自己的 flagset 用 normalizeArgs 解析，
// 因此 `engines --json list` 与 `engines list --json` 等价）。
func insertJSON(args []string) []string {
	return append([]string{args[0], "--json"}, args[1:]...)
}

// shapeOf 描述非对象的顶层形状（数组 / 标量）。
func shapeOf(top any) string {
	switch v := top.(type) {
	case []any:
		return "array of " + itoa(len(v)) + " element(s)"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	case nil:
		return "null"
	}
	return "?"
}

// docKeys 返回对象的顶层键（排序后，输出稳定）。
func docKeys(doc map[string]any) []string {
	keys := make([]string, 0, len(doc))
	for k := range doc {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// docExitCode 取出报告里的退出码：顶层 exitCode，或 summary.exitCode。
func docExitCode(doc map[string]any) (int, bool) {
	if doc == nil {
		return 0, false
	}
	if v, ok := doc["exitCode"]; ok {
		if f, isFloat := v.(float64); isFloat {
			return int(f), true
		}
	}
	if s, ok := doc["summary"].(map[string]any); ok {
		if v, ok := s["exitCode"]; ok {
			if f, isFloat := v.(float64); isFloat {
				return int(f), true
			}
		}
	}
	return 0, false
}

// tamperVendorFile 改写 vendor 里第一个 .ts 文件，制造完整性失败。
func tamperVendorFile(t *testing.T, vendorDir string) {
	t.Helper()
	found := ""
	_ = filepath.WalkDir(vendorDir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(p, ".ts") {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatalf("no vendored .ts file under %s", vendorDir)
	}
	if err := os.WriteFile(found, []byte("export const tampered = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
