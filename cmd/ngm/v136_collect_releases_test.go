package main

import (
	"os"
	"strings"
	"testing"
)

// TestCollectReleasesUsesOneSpawn 钉住 v0.136：collectReleases 此前对每个匹配
// tag 起一次 git.RevParse（O(tags×modules)），与同文件 writeIndexBranch 把
// hash-object 从 N 次降到 1 次的纪律冲突。现在一次 for-each-ref 拿全。
func TestCollectReleasesUsesOneSpawn(t *testing.T) {
	raw, err := os.ReadFile("modules.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, "func collectReleases(")
	if start < 0 {
		t.Fatal("collectReleases not found")
	}
	end := strings.Index(src[start:], "\nfunc ")
	if end < 0 {
		t.Fatal("collectReleases end not found")
	}
	body := src[start : start+end]
	if !strings.Contains(body, `for-each-ref`) {
		t.Error("collectReleases must resolve all tags in one for-each-ref call")
	}
	if strings.Contains(body, "RevParse(") {
		t.Error("collectReleases must not spawn git per tag (no RevParse in its body)")
	}
}
