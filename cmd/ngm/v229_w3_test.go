package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
)

// TestV229RunExplainHasNoSideEffects 固定 E10 的只读边界（spec §3.6）。
//
// 变异检验：把 runExplain 里的 runPlanSteps 换成 runscript.Exec（或让它把
// CheckRun 的 deny 变成 exit 3）→ "无副作用"/"未执行"/"deny 仍出计划"三处立刻红；
// 恢复 → 绿。
func TestV229RunExplainHasNoSideEffects(t *testing.T) {
	home := isolateUserEnv(t)
	// 显式写 deny，不依赖"默认状态"，也不受其它测试注入的 run:<engine> 影响。
	writeV229Policy(t, home, `{"permissions":{"deny":["run:script"]}}`)

	proj := newRunProject(t, `{`+
		`"prebuild":"echo PRE >> trace.txt",`+
		`"build":"echo MAIN >> trace.txt",`+
		`"postbuild":"echo POST >> trace.txt"}`)
	binDir := filepath.Join(proj, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	beforeProj := v229Snapshot(t, proj)
	beforeHome := v229Snapshot(t, home)

	// ---- 人读模式：deny 下仍出计划，且固定文案含"未执行" ----
	var human, herr bytes.Buffer
	if code := dispatch([]string{"run", "--explain", "build", "--dir=" + proj}, &human, &herr); code != 0 {
		t.Fatalf("explain exit=%d (deny 应只呈现、不报错)\n%s", code, human.String()+herr.String())
	}
	text := human.String()
	for _, want := range []string{"未执行", "当前策略会拒绝（未执行）", "run:script",
		"PATH injection", "plan (pre -> main -> post)", "prebuild", "postbuild"} {
		if !strings.Contains(text, want) {
			t.Errorf("human explain missing %q:\n%s", want, text)
		}
	}

	t.Run("no side effects", func(t *testing.T) {
		if got := v229Snapshot(t, proj); !reflect.DeepEqual(got, beforeProj) {
			t.Errorf("explain changed the project tree:\nbefore=%v\nafter=%v", beforeProj, got)
		}
		if got := v229Snapshot(t, home); !reflect.DeepEqual(got, beforeHome) {
			t.Errorf("explain changed the global config/home:\nbefore=%v\nafter=%v", beforeHome, got)
		}
		if _, err := os.Stat(filepath.Join(proj, "trace.txt")); !os.IsNotExist(err) {
			t.Fatalf("explain executed a script (trace.txt exists): %v", err)
		}
	})

	// ---- JSON 模式：顺序/来源/PATH/授权状态 ----
	var jout, jerr bytes.Buffer
	if code := dispatch([]string{"run", "--explain", "--json", "build", "--dir=" + proj}, &jout, &jerr); code != 0 {
		t.Fatalf("explain --json exit=%d\n%s", code, jerr.String())
	}
	var rep runExplainReport
	if err := json.Unmarshal(jout.Bytes(), &rep); err != nil {
		t.Fatalf("explain --json is not one document: %v\n%s", err, jout.String())
	}
	if rep.Version != 1 || rep.Script != "build" {
		t.Errorf("unexpected report head: %+v", rep)
	}
	if !strings.Contains(rep.Note, "未执行") || !strings.Contains(rep.Auth.Decision, "未执行") {
		t.Errorf("fixed 未执行 wording missing: %+v", rep)
	}
	if rep.Auth.Permission != "run:script" || rep.Auth.Allowed {
		t.Errorf("auth state wrong under deny: %+v", rep.Auth)
	}
	if len(rep.Steps) != 3 {
		t.Fatalf("steps=%+v, want pre/main/post", rep.Steps)
	}
	wantPhases := []string{"pre", "main", "post"}
	wantEvents := []string{"prebuild", "build", "postbuild"}
	for i, st := range rep.Steps {
		if st.Phase != wantPhases[i] || st.Event != wantEvents[i] || st.Source != "package.json" {
			t.Errorf("step %d = %+v, want %s/%s/package.json", i, st, wantPhases[i], wantEvents[i])
		}
	}
	foundBin := false
	for _, d := range rep.PathDirs {
		if d == binDir {
			foundBin = true
		}
	}
	if !foundBin {
		t.Errorf("injected PATH missing %q in %v", binDir, rep.PathDirs)
	}

	// ---- 顺序同源：真实 run 的执行顺序必须与 explain 的步骤一致 ----
	grantRunScript(t, home) // 之后才授权：证明 explain 本身不需要也不写入授权
	code, out := runCaptureCode(t, "run", "build", "--dir="+proj)
	if code != 0 {
		t.Fatalf("real run exit=%d\n%s", code, out)
	}
	raw, err := os.ReadFile(filepath.Join(proj, "trace.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(strings.ReplaceAll(string(raw), "\r\n", "\n")); !reflect.DeepEqual(got, []string{"PRE", "MAIN", "POST"}) {
		t.Errorf("real run order = %v, want PRE MAIN POST (same as explain)", got)
	}

	// 授权后再次 explain：同一判定函数应给出"放行"，但仍未执行。
	var allowedOut bytes.Buffer
	if code := dispatch([]string{"run", "--explain", "build", "--dir=" + proj}, &allowedOut, &bytes.Buffer{}); code != 0 {
		t.Fatalf("explain (allow) exit=%d", code)
	}
	if !strings.Contains(allowedOut.String(), "当前策略会放行（未执行）") {
		t.Errorf("allow decision missing:\n%s", allowedOut.String())
	}
}

// writeV229Policy 直接写被测命令真实读取的那一份全局配置。
func writeV229Policy(t *testing.T, home, body string) {
	t.Helper()
	path := config.EffectiveGlobalPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// v229Snapshot 把一棵目录树拍成"相对路径 → 内容/目录标记"的只读快照。
func v229Snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			out[rel+"/"] = "<dir>"
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
