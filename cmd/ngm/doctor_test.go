package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// V214 钉住"脚本审批预演"：doctor 只列会被默认策略拒绝的声明，
// 给出可粘贴的策略片段，且**不执行任何脚本**（ADR-054）。

func v214Project(t *testing.T) (string, string) {
	t.Helper()
	isolateUserEnv(t)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ngm.json"),
		`{"schemaVersion":3,"name":"github.com:acme/app","version":"0.1.0","runtime":"node"}`)
	return dir, filepath.Join(dir, "ngm.vendor")
}

func v214Entry(rep scriptApprovalReport, dependency, kind string) (scriptApprovalEntry, bool) {
	for _, e := range rep.Entries {
		if e.Dependency == dependency && e.Kind == kind {
			return e, true
		}
	}
	return scriptApprovalEntry{}, false
}

func TestV214DoctorListsDeclaredInstallScripts(t *testing.T) {
	dir, vendor := v214Project(t)
	// 两个 vendor 依赖 + 一个 package-lock 声明；刻意用不同来源与种类。
	writeFile(t, filepath.Join(vendor, "github.com", "acme", "zeta", "package.json"),
		`{"name":"github.com:acme/zeta","scripts":{"postinstall":"node setup.js"}}`)
	writeFile(t, filepath.Join(vendor, "github.com", "acme", "alpha", "package.json"),
		`{"name":"alpha","scripts":{"install":"make install"}}`)
	// 这个钩子若被执行会留下 marker；报告之后必须确认它没有被跑。
	hook := filepath.Join(vendor, "github.com", "acme", "alpha", PostInstallHookName)
	writeFile(t, hook, "Deno.writeTextFileSync('marker', 'ran')\n")
	marker := filepath.Join(vendor, "github.com", "acme", "alpha", "marker")
	writeFile(t, filepath.Join(dir, "package-lock.json"),
		`{"lockfileVersion":3,"packages":{`+
			`"node_modules/declared-only":{"version":"1.0.0","hasInstallScript":true}}}`)

	rep := scanScriptApprovals(dir, vendor, "deny")
	if rep.Status != "pending" || rep.Policy != "deny" {
		t.Fatalf("status=%q policy=%q, want pending/deny", rep.Status, rep.Policy)
	}
	if len(rep.Entries) < 4 {
		t.Fatalf("entries = %+v, want vendor postinstall/install/hook + lock declaration", rep.Entries)
	}
	if e, ok := v214Entry(rep, "github.com:acme/zeta", "package.json:scripts.postinstall"); !ok || !strings.Contains(e.Detail, "node setup.js") {
		t.Errorf("postinstall declaration missing: %+v", rep.Entries)
	}
	if _, ok := v214Entry(rep, "alpha", "package.json:scripts.install"); !ok {
		t.Errorf("install declaration missing: %+v", rep.Entries)
	}
	if _, ok := v214Entry(rep, "github.com/acme/alpha", PostInstallHookName); !ok {
		t.Errorf("postinstall.js declaration missing: %+v", rep.Entries)
	}
	if e, ok := v214Entry(rep, "declared-only", "package-lock.json:hasInstallScript"); !ok || e.Source != "package-lock.json" {
		t.Errorf("package-lock declaration missing: %+v", rep.Entries)
	}
	if !strings.Contains(rep.Allow, `"postInstallPolicy": "allow"`) || !strings.Contains(rep.Deny, `"postInstallPolicy": "deny"`) {
		t.Errorf("pasteable snippets missing: %+v", rep)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("doctor executed a declared script (marker exists): %v", err)
	}

	// 接线检查：人读与 --json 都必须能拿到这一节。
	var human, errb bytes.Buffer
	if code := dispatch([]string{"doctor", "--dir=" + dir}, &human, &errb); code != 0 {
		t.Fatalf("doctor exit=%d\n%s", code, errb.String())
	}
	for _, want := range []string{"script approval preview", "would be denied", "github.com:acme/zeta", "allow (sandbox only)"} {
		if !strings.Contains(human.String(), want) {
			t.Errorf("human report missing %q:\n%s", want, human.String())
		}
	}
	var jsonOut, jsonErr bytes.Buffer
	if code := dispatch([]string{"doctor", "--json", "--dir=" + dir}, &jsonOut, &jsonErr); code != 0 {
		t.Fatalf("doctor --json exit=%d\n%s", code, jsonErr.String())
	}
	var doc doctorReport
	if err := json.Unmarshal(jsonOut.Bytes(), &doc); err != nil {
		t.Fatalf("doctor --json is not one document: %v\n%s", err, jsonOut.String())
	}
	if doc.Scripts == nil || doc.Scripts.Status != "pending" || len(doc.Scripts.Entries) < 4 {
		t.Fatalf("scripts section missing from --json: %+v", doc.Scripts)
	}
	var check bool
	for _, c := range doc.Checks {
		if c.Name == "scripts" && c.Status == "warn" {
			check = true
		}
	}
	if !check {
		t.Errorf("no warning `scripts` check: %+v", doc.Checks)
	}
}

func TestV214DoctorSaysNoApprovalNeeded(t *testing.T) {
	dir, vendor := v214Project(t)
	rep := scanScriptApprovals(dir, vendor, "deny")
	if rep.Status != "none" || len(rep.Entries) != 0 || rep.Allow != "" {
		t.Fatalf("no scripts: %+v", rep)
	}
	var buf bytes.Buffer
	writeScriptApprovals(&buf, &rep)
	for _, want := range []string{"no approval needed", "无需审批"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q: %s", want, buf.String())
		}
	}
	var human, errb bytes.Buffer
	if code := dispatch([]string{"doctor", "--dir=" + dir}, &human, &errb); code != 0 {
		t.Fatalf("doctor exit=%d\n%s", code, errb.String())
	}
	if !strings.Contains(human.String(), "no approval needed") {
		t.Errorf("doctor did not print the no-approval line:\n%s", human.String())
	}
}

func TestV214DoctorScriptReportIsDeterministic(t *testing.T) {
	dir, vendor := v214Project(t)
	for _, name := range []string{"z-last", "a-first", "m-middle"} {
		writeFile(t, filepath.Join(vendor, name, "package.json"),
			`{"name":"`+name+`","scripts":{"postinstall":"echo `+name+`"}}`)
	}
	writeFile(t, filepath.Join(dir, "package-lock.json"),
		`{"lockfileVersion":3,"packages":{`+
			`"node_modules/zzz":{"hasInstallScript":true},"node_modules/aaa":{"hasInstallScript":true}}}`)

	a := scanScriptApprovals(dir, vendor, "deny")
	b := scanScriptApprovals(dir, vendor, "deny")
	first, _ := json.Marshal(a)
	second, _ := json.Marshal(b)
	if !bytes.Equal(first, second) {
		t.Fatalf("two scans differ:\n%s\n%s", first, second)
	}
	less := func(i int) bool {
		x, y := a.Entries[i-1], a.Entries[i]
		if x.Source != y.Source {
			return x.Source < y.Source
		}
		if x.Dependency != y.Dependency {
			return x.Dependency < y.Dependency
		}
		if x.Kind != y.Kind {
			return x.Kind < y.Kind
		}
		return x.Detail <= y.Detail
	}
	for i := 1; i < len(a.Entries); i++ {
		if !less(i) {
			t.Fatalf("entries not sorted at %d: %+v", i, a.Entries)
		}
	}
}
