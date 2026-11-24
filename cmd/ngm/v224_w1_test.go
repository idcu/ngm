package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

func w1FileSet(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func w1SameFiles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestV224ReceiptVerifySeparatesIntegrityFromMissingTool 钉住 E5：
// 篡改产物 → 2；删引擎 → 5；两者同时发生按 ADR-039 完整性压过资源缺失；
// 命令只读、不产生新文件、不启动引擎（连 --version 探测也不做）。
func TestV224ReceiptVerifySeparatesIntegrityFromMissingTool(t *testing.T) {
	testutils.AllowEngines(t, "fake", "fake-engine")
	isolateUserEnv(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := w1Fixture(t, fake)
	t.Setenv("FAKE_OUT", "/* v224 bundle */\n")

	code, out := runCaptureCode(t,
		"build", "--outfile=dist/app.js", "--dir="+proj, "--provenance")
	if code != 0 {
		t.Fatalf("fixture build exit=%d:\n%s", code, out)
	}
	artifact := filepath.Join(proj, "dist", "app.js")
	good, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}

	// 基线：回执与产物一致 → 0。
	code, out = runCaptureCode(t, "verify", "--receipt", "--dir="+proj)
	if code != 0 {
		t.Fatalf("baseline receipt verify exit=%d:\n%s", code, out)
	}
	if !strings.Contains(out, "artifact matches receipt") {
		t.Errorf("human output should state the conclusion:\n%s", out)
	}

	// 只读性：verify 前后文件集合完全一致；且假引擎的 --version 探测文件不存在。
	marker := filepath.Join(t.TempDir(), "probed")
	t.Setenv("FAKE_VERSION_MARKER", marker)
	before := w1FileSet(t, proj)
	code, _, out = runVerifyJSON(t, "--receipt", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("baseline --json exit=%d:\n%s", code, out)
	}
	if after := w1FileSet(t, proj); !w1SameFiles(before, after) {
		t.Fatalf("verify --receipt created/removed files:\nbefore=%v\nafter=%v", before, after)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("verify --receipt probed the engine (--version marker exists): %v", err)
	}

	// ① 产物不符 → 2；JSON 里 output 层 fail、engine 层 pass。
	if err := os.WriteFile(artifact, []byte("tampered artifact\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, rep, out := runVerifyJSON(t, "--receipt", "--json", "--dir="+proj)
	if code != 2 {
		t.Fatalf("tampered artifact must exit 2, got %d:\n%s", code, out)
	}
	if rep.Summary.ExitCode != 2 || len(rep.Gates) != 2 {
		t.Fatalf("report should carry exitCode=2 and two gates: %+v", rep)
	}
	if rep.Gates[0].Name != "receipt-output" || rep.Gates[0].ExitCode != 2 ||
		rep.Gates[1].Name != "receipt-engine" || rep.Gates[1].ExitCode != 0 {
		t.Errorf("gates must separate integrity from engine: %+v", rep.Gates)
	}

	// ② 产物恢复、引擎缺失 → 5（资源缺失单独报）。
	if err := os.WriteFile(artifact, good, 0o644); err != nil {
		t.Fatal(err)
	}
	w1WriteCatalog(t, proj, "ngm-w1-missing-engine-tool")
	code, rep, out = runVerifyJSON(t, "--receipt", "--json", "--dir="+proj)
	if code != 5 {
		t.Fatalf("missing engine must exit 5, got %d:\n%s", code, out)
	}
	if rep.Gates[0].ExitCode != 0 || rep.Gates[1].ExitCode != 5 {
		t.Errorf("engine gate should be the only failure: %+v", rep.Gates)
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "ngm-w1-missing-engine-tool") {
		t.Errorf("notes must name the missing tool: %+v", rep.Notes)
	}

	// ③ 两者同时发生：2 压过 5（ADR-039），且仍不产生文件。
	before = w1FileSet(t, proj)
	if err := os.WriteFile(artifact, []byte("tampered again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, rep, out = runVerifyJSON(t, "--receipt", "--json", "--dir="+proj)
	if code != 2 {
		t.Fatalf("integrity must win over missing tool (ADR-039), got %d:\n%s", code, out)
	}
	if rep.Gates[0].ExitCode != 2 || rep.Gates[1].ExitCode != 5 {
		t.Errorf("both gates should report their own code: %+v", rep.Gates)
	}
	if after := w1FileSet(t, proj); !w1SameFiles(before, after) {
		t.Fatalf("failing verify --receipt wrote files:\nbefore=%v\nafter=%v", before, after)
	}
}
