package mappings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.106：`main` / `types` 不能逃出被 vendor 的目录。
//
// 缺陷形态：`checkEntry` 用 `filepath.Join(toAbs, p)` 拼路径，而 Join 会老实
// 地把 `../../../x.js` 解析到 vendor 之外。那份文件若真的存在，
// **存在性检查反而放行**。而这个值会被 integrations 拼进 tsconfig 的 paths /
// alias，于是别名指向项目里任意位置——运行时的表现是"模块找到了，但不是你以为的
// 那一个"，比"找不到"难查得多。
func TestV106EntryMustNotEscapeTheVendoredDirectory(t *testing.T) {
	proj := t.TempDir()

	// vendor 里的依赖目录（`to` 的目标）。
	depDir := filepath.Join(proj, "ngm.vendor", "github.com", "x", "dep")
	if err := os.MkdirAll(depDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 一个**真实存在**的越界目标：从 depDir 往上三级正好回到项目根。
	if err := os.WriteFile(filepath.Join(proj, "secret.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &File{Version: 1, Mappings: []Mapping{{
		From: "github:x/dep",
		To:   "./ngm.vendor/github.com/x/dep",
		Main: "../../../secret.js",
	}}}

	findings, err := Validate(f, ValidateEnvironment{
		ProjectDir: proj,
		LockNames:  map[string]bool{"github:x/dep": true},
		VendorRoot: "./ngm.vendor",
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	var escaped bool
	for _, fi := range findings {
		if strings.Contains(fi.Message, "escapes the vendored directory") && fi.Fatal {
			escaped = true
		}
	}
	if !escaped {
		t.Fatalf("an entry that resolves outside `to` must be a fatal finding; got %+v", findings)
	}

	// 反向：目录内的入口不许被误伤（`./index.js` 与 `index.js` 都要过）。
	for _, good := range []string{"./index.js", "index.js"} {
		ok := &File{Version: 1, Mappings: []Mapping{{
			From: "github:x/dep", To: "./ngm.vendor/github.com/x/dep", Main: good,
		}}}
		if err := os.WriteFile(filepath.Join(depDir, "index.js"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		fs, ferr := Validate(ok, ValidateEnvironment{
			ProjectDir: proj,
			LockNames:  map[string]bool{"github:x/dep": true},
			VendorRoot: "./ngm.vendor",
		})
		if ferr != nil {
			t.Fatal(ferr)
		}
		for _, fi := range fs {
			if strings.Contains(fi.Message, "escapes") {
				t.Errorf("`%s` is inside the vendored directory and must not be flagged: %+v", good, fi)
			}
		}
	}
}
