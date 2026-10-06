package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.49：**config 包的校验错误必须自带建议**（静态普查）。
//
// 起点是 v0.48 的读数：`internal/config/validate.go` 的三十处校验全是裸错误
// （`fmt.Errorf` / `errors.New`），没有一处带 hint——而包顶的注释写着
// "返回的错误是给用户看的：包含字段路径与可操作 hint"。**注释比实现说得多。**
//
// 那一版只修了 path 一处（因为它在用户眼前犯事），这一版把整族补齐，
// 并留一条**静态**判据钉住它——因为这族共 30 处，靠"每处都造一个夹具"覆盖
// 不现实（v0.46 的教训：覆盖集要么从源码派生，要么迟早漏）。
//
// 规则（只看 `validate.go` 这个校验文件）：
//
//	① `errors.New(` 只允许出现在**例外表**里（键 = 去掉空白的整行，值 = 为什么）；
//	② `fmt.Errorf(` 只允许出现在**带 `%w` 的包装**里——包装不造新信息，
//	   它把内层的建议原样带上来（`inheritedHint` 沿链找）；
//	③ 例外表的键必须**仍然存在**于文件里——删掉那行代码却留着登记，等于登记看着空气。
//
// 判据只读源码、不跑命令：它是"这一族新错误必须带建议"的**结构性**下限，
// 与 v0.30（空 hint 棘轮）、v0.46（覆盖集从哪来）同族。
func TestV49ConfigValidationAlwaysCarriesAHint(t *testing.T) {
	path := filepath.Join(repoRoot(t), "internal", "config", "validate.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")

	// 例外表：**每一条都要写清为什么**（没有理由的例外就是漏网）。
	exempt := map[string]string{
		`return errors.New("project file is nil")`: "调用方的编程错误，用户写不出这个状态",
	}

	bare, wrappers, exemptSeen := 0, 0, map[string]bool{}
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.Contains(line, "errors.New("):
			if _, ok := exempt[line]; !ok {
				bare++
				t.Errorf("validate.go:%d 造了一个**没有建议**的错误：%s\n"+
					"这一族的新错误必须带上'该怎么改'——用 fieldErr(msg, hint)，"+
					"或把这个状态登记进例外表（并写清为什么）", i+1, line)
			} else {
				exemptSeen[line] = true
			}
		case strings.Contains(line, "fmt.Errorf("):
			if !strings.Contains(line, "%w") {
				bare++
				t.Errorf("validate.go:%d 造了一个**没有建议**的错误：%s\n"+
					"（带 %%w 的包装不算：它把内层的建议原样带上来）", i+1, line)
			} else {
				wrappers++
			}
		}
	}

	// 例外表的键必须仍然存在——否则那条登记**什么也没看着**。
	for k := range exempt {
		if !exemptSeen[k] {
			t.Errorf("例外表里的这一条在 validate.go 里已经不存在了：%s\n"+
				"删掉代码却留着登记，等于登记看着空气", k)
		}
	}

	// 可达性：这条判据必须**真的看到了东西**（否则文件被改名/清空时它会静默通过）。
	if wrappers == 0 && bare == 0 {
		t.Fatalf("no error construction was seen at all in validate.go — " +
			"either the file moved or this census no longer looks at the right thing")
	}
	t.Logf("config validation: %d bare error(s) (exemptions %d, all present), %d %%w wrapper(s)",
		bare, len(exempt), wrappers)
}
