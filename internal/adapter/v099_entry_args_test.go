package adapter

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// v0.99：清单里的固定前缀参数（`Entry.Args`）必须**只前置一次**，两个引擎形态一致。
//
// 起因：v0.99 读代码时发现 `wasmEngine.entryArgs` 也做了一次前置，而它调用的
// `buildInvocation` 已经前置过 ⇒ wasm 形态会传两遍、subprocess 形态传一遍。
// 这正是那条注释（"两者必须一致"）承诺不会发生的事，而**现有测试看不见**：
// `wasm_test.go` 比的是两个 **builder** 的输出（`buildInvocation(wasm, req)` 与
// `buildInvocation(sub, req)`）——比在**错误的层级**上（前置发生在它之后）。
//
// 修法不是"两处各修对"，而是**让它只剩一处**（v0.82 的老办法）：现在
// `buildInvocation` 是唯一前置点，wasm 侧直接用 `inv.Args`。
//
// 这条判据因此分两层：
//   - 行为层：带 `Args` 的条目走 `buildInvocation` 后，前缀**恰好出现一次**；
//   - 结构层：`internal/adapter` 的非测试源码里，`entry.Args` 只允许出现在**登记过的那几行**，
//     且两个 `--version` 探针必须**逐字相同**（它们是刻意的一对镜像，也是"两者必须一致"
//     这句话里唯一还需要人工维持的部分——所以这里比字符串）。
//
// 结构层为什么必要：行为层只观测得到"合并点那一次"，观测不到"是不是还有第二次"——
// 而这次的 bug 恰恰长在第二次上。**改这里要动登记表，这就是它的用处。**
func TestV99EntryArgsArePrependedExactlyOnce(t *testing.T) {
	t.Run("行为层：前缀恰好一次", func(t *testing.T) {
		entry := Entry{
			Name: "my-tool", Kind: KindBundle, Adapter: AdapterSubprocess,
			Command: "my-tool --fixed-flag",
		}
		entry.Program, entry.Args = splitCommand(entry.Command)
		if len(entry.Args) != 1 {
			t.Fatalf("夹具坏了吗：splitCommand(%q) 给 %v", entry.Command, entry.Args)
		}

		inv, err := buildInvocation(entry, buildRequest{
			Options:   BundleOptions{Outfile: "out/index.js"},
			EntryFile: "src/x.ts",
		})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(inv.Args, " ")
		if n := strings.Count(joined, "--fixed-flag"); n != 1 {
			t.Errorf("固定前缀出现 %d 次（必须恰好 1 次）：%v", n, inv.Args)
		}
		if len(inv.Args) == 0 || inv.Args[0] != "--fixed-flag" {
			t.Errorf("固定前缀必须在最前面：%v", inv.Args)
		}
	})

	t.Run("结构层：entry.Args 只允许出现在登记过的行", func(t *testing.T) {
		// 每一行都写清楚它为什么存在。多一行、少一行、或改了字面 ⇒ 红。
		allowed := map[string]int{
			`args := append(append([]string{}, e.entry.Args...), "--version")`: 2, // subprocess.go 与 wasm.go 各一处：--version 探针（刻意镜像）
			`if len(entry.Args) > 0 {`: 1, // buildInvocation 的守卫
			`inv.Args = append(append([]string{}, entry.Args...), inv.Args...)`: 1, // **唯一**前置点
		}

		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			t.Fatal("runtime.Caller 失败——这条判据读不到源码就没法守纪律")
		}
		dir := filepath.Dir(thisFile)
		if filepath.Base(dir) != "adapter" {
			t.Fatalf("这条判据应当在 internal/adapter 里（实际 %s）", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		probeFiles := map[string]bool{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strings.Split(string(raw), "\n") {
				trimmed := strings.TrimSpace(line)
				// 只看**代码行**：注释里提到 `entry.Args` 不会前置任何东西
				//（第一版忘了这一步，于是判据抓到的是它自己的说明文字）。
				if strings.HasPrefix(trimmed, "//") {
					continue
				}
				if !strings.Contains(trimmed, "entry.Args") {
					continue
				}
				got[trimmed]++
				if strings.Contains(trimmed, `"--version"`) {
					probeFiles[e.Name()] = true
				}
			}
		}

		for line, want := range allowed {
			if got[line] != want {
				t.Errorf("`%s`\n  出现 %d 次，登记的是 %d 次 —— 改了这一行就必须改登记表（这就是它的用处）", line, got[line], want)
			}
		}
		for line, n := range got {
			if _, ok := allowed[line]; !ok {
				t.Errorf("多了一处 `entry.Args`：`%s`（%d 次）—— 前置只能有一处，否则两个形态会分家（v0.99 的 bug 就是这么来的）", line, n)
			}
		}
		if len(probeFiles) != 2 {
			t.Errorf("`--version` 探针应当**恰好**在两个引擎文件里各一份（它们是刻意镜像的一对），实际在：%v", probeFiles)
		}
	})
}
