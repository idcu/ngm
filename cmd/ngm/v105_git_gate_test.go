package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- v0.105：`run:git` 门禁必须覆盖**每一条**生产调用路径 ---

// optsWithoutPolicy 登记"确实不需要权限对象"的构造点。
//
// 判据的方向是 fail-closed：新写一处 `git.Options{...}` 而忘了带 `Policy:`，
// 必须红。因此这张表是**例外表**，每一条都要写清为什么——没有 why 的例外
// 与"我忘了它"在源码里长得一模一样（v0.52 立下的纪律）。
var optsWithoutPolicy = map[string]string{
	"cmd/ngm/store.go": "只做文件系统遍历（Mirror.Usage / ModuleCache.Usage / PruneResidue），" +
		"这条路径不启动 git 子进程；哪天它需要起 git，这条登记必须先删掉",
	"cmd/ngm/modules.go": "文件里唯一的字面量是 `return git.Options{}, err`——错误返回，" +
		"不是一份可用的选项",
}

// 缺陷形态（v0.105 修）：`cmd/ngm/modules.go` 有六处 `git.Options{Dir: dir}`，
// 而 `Policy` 为 nil 的语义是"不施加权限"（core/git/exec.go）。于是
// `ngm modules changed|plan|index` 成了唯一能绕过 `run:git` 的 git 路径——
// 而那条路径会 `hash-object` / `commit-tree` / `update-ref`，是**写操作**。
//
// 为什么用源码判据而不是行为判据：`spawn_gate_test.go` 已经证明"拒了就不起进程"，
// 但它证明不了"每个调用点都带上了 Policy"——那是个全称命题，而全称命题
// 只能靠枚举源码给出（v0.37「枚举，而不是列举」）。
func TestV105EveryGitCallCarriesTheGate(t *testing.T) {
	root := repoRoot(t)

	const (
		// 结构性守卫：实测值。少于它说明扫描本身坏了（比如 Walk 提前返回、
		// 或路径过滤把整个 cmd/ 跳过了）——那时"全绿"是假绿。
		minSites = 9
	)

	var scanned int
	var missing []string
	usedExceptions := map[string]bool{}

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
		for i, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			// 注释里会**引用**这个字面量（解释缺陷时就是这么写的），
			// 而注释不是调用点——v0.57 的教训：按名字扫会扫到说名字的那句话。
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			if !strings.Contains(line, "git.Options{") {
				continue
			}
			scanned++
			if strings.Contains(line, "Policy:") {
				continue
			}
			if _, ok := optsWithoutPolicy[rel]; ok {
				usedExceptions[rel] = true
				continue
			}
			missing = append(missing, fmt.Sprintf("%s:%d: %s", rel, i+1, trimmed))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk source tree: %v", walkErr)
	}

	for _, m := range missing {
		t.Errorf("git call without the gate:\n  %s\n"+
			"Policy==nil means \"no permission is applied\" — `deny: [\"run:git\"]` would not stop this path", m)
	}
	if scanned < minSites {
		t.Fatalf("only %d git.Options literal(s) were scanned (floor %d): the scan itself is broken, "+
			"so a green result would mean nothing", scanned, minSites)
	}
	// 反向：登记了却再也命中 ⇒ 那条 why 已经过期，它现在在守护空气。
	for file, why := range optsWithoutPolicy {
		if !usedExceptions[file] {
			t.Errorf("exception registered for %s but never matched — it is guarding nothing now "+
				"(registered reason: %s)", file, why)
		}
		if strings.TrimSpace(why) == "" {
			t.Errorf("exception for %s has no reason: an exception without a why is "+
				"indistinguishable from having forgotten it", file)
		}
	}
}
