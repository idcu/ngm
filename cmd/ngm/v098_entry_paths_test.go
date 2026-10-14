package main

import (
	"encoding/json"
	"testing"

	"github.com/idcu/ngm/internal/mappings"
)

// v0.98：**入口路径必须平台无关**，且与自检器的 `rel_path` 同口径。
//
// 为什么这一版要单独一条判据（对齐网已经覆盖了一部分）：
//
//   - **反斜杠**那一格在本机**无法用行为分辨**——产品旧实现靠 `filepath.ToSlash`，
//     而它在这个平台上正好会把 `\` 转成 `/`。也就是说：这一格在 Windows 上"碰巧"是对的，
//     到 Linux 就变成 `./src\x.js`（而自检器一律转）。口径：package.json 里的路径按 `/`
//     语义理解——npm 自己就是这么规定的。它的价值在 **Linux CI**，那里它会红。
//   - **多个前导斜杠**那一格两边都分辨得出来：旧实现只剥一个（`//src/x.js` ⇒
//     `.//src/x.js`），自检器剥全部（`./src/x.js`）。指向同一个文件、**打印出来不一样**，
//     而"报告里写出来的就是产品会用的那一个"是这条链路的全部意义（v0.88 / v0.97）。
//
// 这里走**公开入口** `mappings.InferEntry`（登记表只认 `cmd/ngm` 里的判据；
// 内部单测那一条已被这条取代）——测的是"产品最终会写进 mappings 的那个路径"。
func TestV98EntryPathsAreNormalised(t *testing.T) {
	cases := []struct{ in, want string }{
		{`src\entry.js`, "./src/entry.js"},
		{`.\src\entry.js`, "./src/entry.js"},
		{"//src/x.js", "./src/x.js"},
		{"///src/x.js", "./src/x.js"},
		{"/abs.js", "./abs.js"},
		{"m.js", "./m.js"},
		{"./m.js", "./m.js"},
		{"../m.js", "../m.js"},
		{"   ./m.js   ", "./m.js"},
		{"  src\\x.js  ", "./src/x.js"},
		{"/", "./"}, // 剥到一个不剩 ⇒ "./"（自检器同样如此）
	}

	for _, c := range cases {
		// 用 json.Marshal 造 package.json：反斜杠的转义交给它，不手搓字符串。
		pkg := map[string]any{
			"name": "a", "version": "1.0.0", "type": "module", "license": "MIT",
			"main": c.in,
		}
		raw, err := json.Marshal(pkg)
		if err != nil {
			t.Fatal(err)
		}
		entry, warns := mappings.InferEntry(mapReader(map[string]string{"package.json": string(raw)}))
		if len(warns) > 0 {
			t.Errorf("main=%q 时不该有告警：%v", c.in, warns)
		}
		if entry.Main != c.want {
			t.Errorf("main=%q ⇒ 入口 %q，want %q", c.in, entry.Main, c.want)
		}
	}
}

// mapReader 把内存里的一组文件包成 mappings.FileReader（只读，够入口推断用）。
func mapReader(files map[string]string) mappings.FileReader {
	return func(rel string) ([]byte, bool, error) {
		if s, ok := files[rel]; ok {
			return []byte(s), true, nil
		}
		return nil, false, nil
	}
}
