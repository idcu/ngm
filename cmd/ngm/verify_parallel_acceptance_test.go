package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/idcu/ngm/internal/lock"
)

// TestV02VerifyReportOrderIsStable 固定并发改造后唯一的对外不变量：
// **报告顺序必须与 lock 顺序一致**。
//
// 并发执行后结果按调度完成、先后不定，如果直接 append 就会让同一份 lock
// 产生不同的 JSON——那会让 diff、快照测试与"报告可比"全部失效。
// 因此实现里写的是 results[i] 而不是 append，本用例就是它的防线。
func TestV02VerifyReportOrderIsStable(t *testing.T) {
	isolateUserEnv(t)

	// 6 个独立依赖（各自一个 mirror）：足以让依赖级并发真正跑起来
	proj := newProject(t)
	for i := 0; i < 6; i++ {
		slug := fmt.Sprintf("github:v/dep%02d", i)
		scUpstream(t, slug, fmt.Sprintf("export const v%d = 1\n", i), "")
		if code, out := runCaptureCode(t, "add", slug+"@v1", "--ref-type=tag", "--dir="+proj); code != 0 {
			t.Fatalf("add %s: %s", slug, out)
		}
	}
	if code, out := runCaptureCode(t, "install", "--dir="+proj); code != 0 {
		t.Fatalf("install: %s", out)
	}

	lf, err := lock.Read(lock.Find(proj))
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Dependencies) != 6 {
		t.Fatalf("lock has %d dependencies, want 6", len(lf.Dependencies))
	}

	code, out := runCaptureCode(t, "verify", "--offline", "--json", "--dir="+proj)
	if code != 0 {
		t.Fatalf("verify exit=%d:\n%s", code, out)
	}
	var rep struct {
		Dependencies []struct {
			Name string `json:"name"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("verify --json is not valid JSON: %v\n%s", err, out)
	}
	if len(rep.Dependencies) != len(lf.Dependencies) {
		t.Fatalf("report has %d entries, lock has %d", len(rep.Dependencies), len(lf.Dependencies))
	}
	for i := range rep.Dependencies {
		if rep.Dependencies[i].Name != lf.Dependencies[i].Name {
			t.Errorf("report order must follow the lock: entry %d is %q, lock says %q",
				i, rep.Dependencies[i].Name, lf.Dependencies[i].Name)
		}
	}
}
