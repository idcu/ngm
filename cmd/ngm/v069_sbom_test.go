package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.69：`ngm sbom` 的端到端断言（评估第 3 层：合规场景的入场券）。

func TestV69SbomExportsFromTheLock(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	code, out, errOut := runSplit(t, "sbom", "--dir="+p)
	if code != 0 {
		t.Fatalf("sbom exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
	var doc struct {
		BomFormat  string `json:"bomFormat"`
		Components []struct {
			Name   string `json:"name"`
			Purl   string `json:"purl"`
			Hashes []struct {
				Alg     string `json:"alg"`
				Content string `json:"content"`
			} `json:"hashes"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n%s", err, out)
	}
	if doc.BomFormat != "CycloneDX" {
		t.Errorf("默认格式应为 CycloneDX，got %q", doc.BomFormat)
	}
	if len(doc.Components) != 1 || doc.Components[0].Name != "github:x/dep" {
		t.Fatalf("components = %+v", doc.Components)
	}
	if !strings.HasPrefix(doc.Components[0].Purl, "pkg:git/github.com/x/dep@") {
		t.Errorf("purl = %q", doc.Components[0].Purl)
	}
}

func TestV69SbomSPDXFormatAndOutFile(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	outFile := filepath.Join(t.TempDir(), "sbom.spdx.json")
	code, out, errOut := runSplit(t, "sbom", "--format=spdx", "--out="+outFile, "--dir="+p)
	if code != 0 {
		t.Fatalf("sbom --out exit=%d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "wrote ") {
		t.Errorf("--out 时 stdout 要确认写到哪了: %q", out)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("--out 没有写出文件: %v", err)
	}
	var doc struct {
		SPDXVersion string `json:"spdxVersion"`
		Packages    []struct {
			Name string `json:"name"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("写出的文件不是合法 JSON: %v", err)
	}
	if doc.SPDXVersion != "SPDX-2.3" || len(doc.Packages) != 1 {
		t.Errorf("SPDX 文档头不对: %+v", doc)
	}
	// stdout 上**不应**混入文档（它去了文件）。
	if strings.Contains(out, "spdxVersion") {
		t.Errorf("--out 时文档不该同时写 stdout")
	}
}

func TestV69SbomUnknownFormatIsAConfigError(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	code, _, errOut := runSplit(t, "sbom", "--format=sarif", "--dir="+p)
	if code != 3 {
		t.Fatalf("未知格式必须 exit 3，got %d:\n%s", code, errOut)
	}
	// 错误文本走 stderr（stdout 保持干净）——这是两条通道的分工。
	if !strings.Contains(errOut, "sarif") || !strings.Contains(errOut, "cyclonedx") {
		t.Errorf("错误要点名未知值并给出支持列表:\n%s", errOut)
	}
}

func TestV69SbomWithoutLockSaysWhatToDo(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t) // 有 ngm.json，没有 ngm.lock

	code, _, errOut := runSplit(t, "sbom", "--dir="+p)
	if code != 3 {
		t.Fatalf("无 lock 必须 exit 3，got %d:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "ngm install") {
		t.Errorf("错误要给出下一步（ngm install）:\n%s", errOut)
	}
}

// 同一份 lock 导两次：除时间戳外逐字节相同（时间戳是唯一被声明的非确定字段）。
func TestV69SbomIsDeterministicAcrossRuns(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	strip := func(s string) string {
		var doc map[string]any
		if err := json.Unmarshal([]byte(s), &doc); err != nil {
			t.Fatalf("不是合法 JSON: %v", err)
		}
		if md, ok := doc["metadata"].(map[string]any); ok {
			delete(md, "timestamp")
		}
		out, _ := json.Marshal(doc)
		return string(out)
	}

	_, a, _ := runSplit(t, "sbom", "--dir="+p)
	_, b, _ := runSplit(t, "sbom", "--dir="+p)
	if strip(a) != strip(b) {
		t.Error("两次导出在时间戳之外还有差异——diff/快照会看到假改动")
	}
}
