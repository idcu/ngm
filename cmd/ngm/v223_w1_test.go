package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/receipt"
	"github.com/idcu/ngm/internal/testutils"
)

// W1 的公共夹具：一个带假引擎、假 lock 的离线项目。
//
// 用 Go 编译的假引擎而不是 shell 脚本，保证 Windows 上也能跑；
// 它不联网、不依赖本机装了 esbuild，产物由 FAKE_OUT 固定，便于逐字节比对。
func w1Fixture(t *testing.T, tool string) (proj string) {
	t.Helper()
	proj = t.TempDir()
	testutils.WriteFile(t, proj, "ngm.json", `{
  "schemaVersion": 3,
  "name": "github.com/w1-test/app",
  "version": "0.1.0",
  "runtime": "node",
  "main": "src/index.ts",
  "dependencies": [],
  "engines": {"bundle": "fake"}
}
`)
	testutils.WriteFile(t, proj, "src/index.ts", "export const x = 1\n")
	// 假 lock：只用来证明"回执写入不碰它"，不参与构建。
	testutils.WriteFile(t, proj, "ngm.lock", "{\"lockfileVersion\":\"3.0.0\",\"dependencies\":[]}\n")
	w1WriteCatalog(t, proj, tool)
	return proj
}

func w1WriteCatalog(t *testing.T, proj, tool string) {
	t.Helper()
	cat := struct {
		Version int `json:"version"`
		Engines []struct {
			Name    string `json:"name"`
			Kind    string `json:"kind"`
			Adapter string `json:"adapter"`
			Command string `json:"command"`
			Version string `json:"version"`
		} `json:"engines"`
	}{Version: 1}
	// 路径可能带空格（Windows 用户名 / 临时目录），splitCommand 只认双引号切分。
	cat.Engines = append(cat.Engines, struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Adapter string `json:"adapter"`
		Command string `json:"command"`
		Version string `json:"version"`
	}{Name: "fake", Kind: "bundle", Adapter: "subprocess",
		Command: `"` + tool + `"`, Version: "9.9.9-w1"})
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	testutils.WriteFile(t, proj, "ngm.engines.json", string(data)+"\n")
}

func w1ReadReceipt(t *testing.T, proj string) ([]byte, receipt.Receipt) {
	t.Helper()
	path := filepath.Join(proj, receipt.FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var r receipt.Receipt
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", receipt.FileName, err, raw)
	}
	return raw, r
}

// TestV223BuildReceiptIsDeterministicAndSidecar 钉住 E4 的五件事：
// 同输入两次字节一致、无时间戳/绝对路径、lock 字节不变、
// engine.digest 是工具文件摘要而非路径、缺引擎 exit 5 且 hint 点名工具。
func TestV223BuildReceiptIsDeterministicAndSidecar(t *testing.T) {
	testutils.AllowEngines(t, "fake", "fake-engine")
	isolateUserEnv(t)
	fake := testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")
	proj := w1Fixture(t, fake)
	t.Setenv("FAKE_OUT", "/* v223 deterministic bundle */\n")

	lockPath := filepath.Join(proj, "ngm.lock")
	lockBefore, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	args := []string{"build", "--outfile=dist/app.js", "--dir=" + proj, "--provenance"}
	for i := 1; i <= 2; i++ {
		code, out := runCaptureCode(t, args...)
		if code != 0 {
			t.Fatalf("build #%d exit=%d:\n%s", i, code, out)
		}
	}
	raw1, r1 := w1ReadReceipt(t, proj)
	raw2, _ := w1ReadReceipt(t, proj)
	if string(raw1) != string(raw2) {
		t.Fatalf("two identical builds produced different receipt bytes:\n%s\n---\n%s", raw1, raw2)
	}

	lockAfter, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(lockBefore) != string(lockAfter) {
		t.Fatalf("ngm.lock changed by build --provenance:\nbefore=%s\nafter=%s", lockBefore, lockAfter)
	}

	lower := strings.ToLower(string(raw1))
	for _, banned := range []string{"timestamp", "generatedat", "startedon", "finishedon", "time"} {
		if strings.Contains(lower, banned) {
			t.Errorf("receipt contains a time-ish key %q: %s", banned, raw1)
		}
	}
	if strings.Contains(string(raw1), filepath.ToSlash(proj)) ||
		strings.Contains(string(raw1), proj) {
		t.Errorf("receipt leaked an absolute project path:\n%s", raw1)
	}

	if r1.ReceiptVersion != 1 {
		t.Errorf("receiptVersion=%d, want 1", r1.ReceiptVersion)
	}
	if len(r1.Inputs) != 1 || r1.Inputs[0].Path != "src/index.ts" ||
		!strings.HasPrefix(r1.Inputs[0].Digest, "sha256:") {
		t.Errorf("inputs must record the relative entry and its digest: %+v", r1.Inputs)
	}
	if r1.Engine.Name != "fake" || r1.Engine.Kind != "bundle" ||
		!strings.HasPrefix(r1.Engine.Version, "9.9.9") {
		t.Errorf("engine identity missing: %+v", r1.Engine)
	}
	if r1.MappingsDigest == "" || r1.Output.Path != "dist/app.js" ||
		!strings.HasPrefix(r1.Output.Digest, "sha256:") {
		t.Errorf("mappings/output fields missing: %+v", r1)
	}
	if len(r1.Argv) == 0 {
		t.Error("argv is empty — the receipt does not say what was passed to the engine")
	}
	for i := 1; i < len(r1.Argv); i++ {
		if r1.Argv[i-1] > r1.Argv[i] {
			t.Fatalf("argv is not sorted: %q", r1.Argv)
		}
	}

	// engine.digest 是文件字节摘要，不是路径，也不等于清单里的命令。
	bin, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bin)
	wantDigest := "sha256:" + hex.EncodeToString(sum[:])
	if r1.Engine.Digest != wantDigest {
		t.Errorf("engine.digest=%s, want sha256 of tool bytes %s", r1.Engine.Digest, wantDigest)
	}
	if strings.Contains(r1.Engine.Digest, fake) || r1.Engine.Digest == fake {
		t.Errorf("engine.digest must not be a path: %q", r1.Engine.Digest)
	}

	// 缺引擎：exit 5，hint 点名到底缺哪个工具，且不写回执。
	missingProj := w1Fixture(t, "ngm-w1-missing-engine-tool")
	code, out := runCaptureCode(t,
		"build", "--outfile=dist/app.js", "--dir="+missingProj, "--provenance")
	if code != 5 {
		t.Fatalf("missing engine should exit 5, got %d:\n%s", code, out)
	}
	if !strings.Contains(out, "ngm-w1-missing-engine-tool") {
		t.Errorf("hint must name the missing tool:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(missingProj, receipt.FileName)); !os.IsNotExist(err) {
		t.Errorf("failed build must not write a receipt (stat err=%v)", err)
	}
}
