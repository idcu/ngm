package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// v0.108：每一次 OSV 查询都必须带上网关门禁。
//
// 缺陷形态（上一轮审计）：`OSVConfig.CheckNet` 为 nil 时**没有门禁**——
// 而"忘了传"在调用点看起来与"策略放行"完全一样。这与项目里其它门禁
// （git 的 `run:git`、引擎的 `run:<engine>`、沙箱的 `net:<host>`）相反：
// 同一个错误在别处是拒绝，在这里是放行。
//
// 为什么**不**把 nil 改成内部错误：那是本包已文档化的语义
// （`OSVConfig` 的字段注释 + `nil CheckNet keeps the previous behaviour` 那条用例），
// 而且测试替身本来就没有策略。与 v0.105 修 `git.Options.Policy` 同一个判断：
// 施力点是**调用方必须传**，用一条源码级判据钉住，而不是改库的语义。
func TestV108EveryOSVQueryCarriesTheGate(t *testing.T) {
	root := repoRoot(t)

	var sites int
	var missing []string

	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "testdata", "ngm.vendor", "node_modules", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if !strings.Contains(line, "OSVConfig{") {
				continue
			}
			sites++
			// 结构体字面量很短：从这一行往下看 12 行足够覆盖全部字段。
			window := lines[i:]
			if len(window) > 12 {
				window = window[:12]
			}
			if !strings.Contains(strings.Join(window, "\n"), "CheckNet:") {
				missing = append(missing, rel+":"+strconv.Itoa(i+1))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}

	for _, m := range missing {
		t.Errorf("an OSVConfig literal without `CheckNet:` at %s — "+
			"a nil gate means the query leaves the process without asking `net:<host>`", m)
	}
	if sites < 2 {
		t.Fatalf("only %d OSVConfig literal(s) scanned: the net's scope shrank to nothing", sites)
	}
}
