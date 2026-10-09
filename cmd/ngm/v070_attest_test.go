package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.70：`ngm attest` 的端到端断言（评估第 3 层：SLSA 谓词 / 内容侧的 L2）。

func TestV70AttestEmitsStatementFromTheLock(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	code, out, errOut := runSplit(t, "attest", "--dir="+p)
	if code != 0 {
		t.Fatalf("attest exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}

	var doc struct {
		Type          string `json:"_type"`
		PredicateType string `json:"predicateType"`
		Subject       []struct {
			Name        string            `json:"name"`
			Digest      map[string]string `json:"digest"`
			Annotations map[string]string `json:"annotations"`
		} `json:"subject"`
		Predicate struct {
			BuildDefinition struct {
				ExternalParameters struct {
					Manifest struct {
						URI    string            `json:"uri"`
						Digest map[string]string `json:"digest"`
					} `json:"manifest"`
					Lock struct {
						URI    string            `json:"uri"`
						Digest map[string]string `json:"digest"`
					} `json:"lock"`
				} `json:"externalParameters"`
				ResolvedDependencies []struct {
					URI    string            `json:"uri"`
					Digest map[string]string `json:"digest"`
				} `json:"resolvedDependencies"`
			} `json:"buildDefinition"`
			RunDetails struct {
				Builder struct {
					ID string `json:"id"`
				} `json:"builder"`
			} `json:"runDetails"`
		} `json:"predicate"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout 不是合法 JSON: %v\n%s", err, out)
	}

	if doc.Type != "https://in-toto.io/Statement/v1" {
		t.Errorf("_type = %q", doc.Type)
	}
	if doc.PredicateType != "https://slsa.dev/provenance/v1" {
		t.Errorf("predicateType = %q", doc.PredicateType)
	}
	if doc.Predicate.RunDetails.Builder.ID == "" {
		t.Error("builder.id 不能为空——那是「谁产出了这份语句」")
	}

	// 语句里的事实必须与 lock 一致（逐字段核对，不做"看起来差不多"的断言）。
	lockRaw, err := os.ReadFile(filepath.Join(p, "ngm.lock"))
	if err != nil {
		t.Fatal(err)
	}
	var lf struct {
		Dependencies []struct {
			Name          string `json:"name"`
			Commit        string `json:"commit"`
			ArchiveDigest string `json:"archiveDigest"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(lockRaw, &lf); err != nil {
		t.Fatal(err)
	}
	if len(lf.Dependencies) != 1 || len(doc.Subject) != 1 {
		t.Fatalf("lock=%d subject=%d, want 1/1", len(lf.Dependencies), len(doc.Subject))
	}
	dep := lf.Dependencies[0]
	want := strings.TrimPrefix(dep.ArchiveDigest, "sha256:")

	if doc.Subject[0].Digest["sha256"] != want {
		t.Errorf("subject 哈希与 lock 不一致: %q != %q", doc.Subject[0].Digest["sha256"], want)
	}
	if !strings.HasPrefix(doc.Subject[0].Name, "pkg:git/github.com/x/dep@") {
		t.Errorf("subject 名 = %q", doc.Subject[0].Name)
	}
	if len(doc.Predicate.BuildDefinition.ResolvedDependencies) != 1 ||
		doc.Predicate.BuildDefinition.ResolvedDependencies[0].Digest["gitCommit"] != dep.Commit {
		t.Errorf("material 的 gitCommit 与 lock 不一致: %+v", doc.Predicate.BuildDefinition.ResolvedDependencies)
	}

	// 输入哈希 = 提交进 Git 的那份文件字节。
	manifestRaw, err := os.ReadFile(filepath.Join(p, "ngm.json"))
	if err != nil {
		t.Fatal(err)
	}
	hexOf := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	ep := doc.Predicate.BuildDefinition.ExternalParameters
	if ep.Lock.URI != "ngm.lock" || ep.Lock.Digest["sha256"] != hexOf(lockRaw) {
		t.Errorf("lock 外部参数不对: %+v", ep.Lock)
	}
	if ep.Manifest.URI != "ngm.json" || ep.Manifest.Digest["sha256"] != hexOf(manifestRaw) {
		t.Errorf("manifest 外部参数不对: %+v", ep.Manifest)
	}

	// 确定性纪律：语句里不许有时间字段。
	for _, key := range []string{`"timestamp"`, `"startedOn"`, `"finishedOn"`} {
		if strings.Contains(out, key) {
			t.Errorf("语句里不许有 %s（同一份 lock 必须导出同一份语句）", key)
		}
	}
}

func TestV70AttestOutFileKeepsStdoutClean(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	outFile := filepath.Join(t.TempDir(), "ngm.intoto.json")
	code, out, errOut := runSplit(t, "attest", "--out="+outFile, "--dir="+p)
	if code != 0 {
		t.Fatalf("attest --out exit=%d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "wrote ") {
		t.Errorf("--out 时 stdout 要确认写到哪了: %q", out)
	}
	if strings.Contains(out, "predicateType") {
		t.Error("--out 时文档不该同时写 stdout（两条通道的分工）")
	}
	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("--out 没有写出文件: %v", err)
	}
	if !strings.Contains(string(data), `"predicateType": "https://slsa.dev/provenance/v1"`) {
		t.Errorf("写出的文件不是语句: %s", string(data)[:min(200, len(data))])
	}
}

func TestV70AttestWithoutLockSaysWhatToDo(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t) // 有 ngm.json，没有 ngm.lock

	code, _, errOut := runSplit(t, "attest", "--dir="+p)
	if code != 3 {
		t.Fatalf("无 lock 必须 exit 3，got %d:\n%s", code, errOut)
	}
	if !strings.Contains(errOut, "ngm install") {
		t.Errorf("错误要给出下一步（ngm install）:\n%s", errOut)
	}
}

// 与 sbom 的差别在这一条上：语句**没有**任何非确定字段——
// 两次运行的字节必须完全相同（不需要剔除时间戳）。
func TestV70AttestIsDeterministicAcrossRuns(t *testing.T) {
	isolateUserEnv(t)
	testutils.MustHaveGit(t)

	p := newProject(t)
	seedDep(t, p)

	_, a, _ := runSplit(t, "attest", "--dir="+p)
	_, b, _ := runSplit(t, "attest", "--dir="+p)
	if a != b {
		t.Error("两次导出的字节不同——语句里混进了非确定字段（时间戳/随机 id）")
	}
}
