package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.106：要被签名的证据只能读一次。
//
// 缺陷形态（`ngm attest`）：先 `env.ReadManifest()`（open + decode），再
// `os.ReadFile(env.ManifestPath())`（又 open 一次）——语句**描述**的那份清单
// 与**被哈希进语句**的那份字节可能不是同一份文件。窗口极小，但这份语句是要被
// 签名的：签完之后没有任何东西能发现它（v0.82 对 lock 做过同样的修）。
//
// 为什么用源码判据：读两次的窗口在运行时要靠并发改写才撞得到，
// 而"只读一次"是一个**结构性**性质——它只能靠枚举调用点给出。
func TestV106AttestReadsTheManifestOnlyOnce(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "cmd", "ngm", "attest.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)

	// 1) 必须恰好有一次 `os.ReadFile(env.ManifestPath())`。
	reads := strings.Count(body, "os.ReadFile(env.ManifestPath())")
	if reads != 1 {
		t.Errorf("attest.go reads ngm.json %d time(s) via os.ReadFile; want exactly 1 — "+
			"the bytes that get hashed and the bytes that get parsed must be the same read", reads)
	}

	// 2) 不许再经过 env.ReadManifest()：那是**另一次** open。
	//    （它此前是第一处读，修完之后这条路径上不该再出现。）
	if strings.Contains(body, "env.ReadManifest()") {
		t.Error("attest.go still calls env.ReadManifest(), which opens the file a second time " +
			"and re-introduces the window this judgment exists to close")
	}

	// 3) 解析必须走**同一份字节**的入口。
	if !strings.Contains(body, "config.ParseProjectFile(manifestBytes") {
		t.Error("attest.go must parse the manifest from the bytes it just read " +
			"(config.ParseProjectFile(manifestBytes, ...)), not from the path")
	}
}
