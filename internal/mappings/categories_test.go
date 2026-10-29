package mappings

import (
	"os"
	"path/filepath"
	"testing"
)

// v0.110：`EscapesVendor` 必须**只**标在"跑出树外"的那两类发现上。
//
// 依赖它的是 `ngm integrations`：它在写别名之前会拒绝这类条目，但**不能**
// 因为"任何 Fatal"就停下——"`to` 目录还不存在"在"先提交 mappings、后 install"
// 的顺序里是正常的，而"别名指向 vendor 之外"从来没有任何正常解释。
// 把这两件事混在一起，要么写出一条逃逸的别名，要么让 CI 里的配置生成永远失败。
func TestV110OnlyContainmentFindingsAreMarkedAsEscaping(t *testing.T) {
	proj := t.TempDir()

	// 一个正常的 vendor 目录（供"存在但越界"的用例落点）。
	okDir := filepath.Join(proj, "ngm.vendor", "github.com", "x", "ok")
	if err := os.MkdirAll(okDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(okDir, "index.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 项目根下一个真实存在的越界目标：从 ok 往上三级正好回到项目根。
	if err := os.WriteFile(filepath.Join(proj, "outside.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	f := &File{Version: 1, Mappings: []Mapping{
		// ① `to` 跑出 vendor 根。
		{From: "github:x/escape-to", To: "../../etc", Main: "index.js"},
		// ② `main` 跑出 `to`（目标文件**真实存在**——存在性检查会放行它）。
		{From: "github:x/escape-main", To: "./ngm.vendor/github.com/x/ok", Main: "../../../outside.js"},
		// ③ 正常条目，只是 `to` 还没落地（install 之前）。
		{From: "github:x/not-installed", To: "./ngm.vendor/github.com/x/missing", Main: "index.js"},
		// ④ 正常条目，`main` 不存在。
		{From: "github:x/no-main", To: "./ngm.vendor/github.com/x/ok", Main: "nope.js"},
	}}

	findings, err := Validate(f, ValidateEnvironment{
		ProjectDir: proj,
		LockNames: map[string]bool{
			"github:x/escape-to": true, "github:x/escape-main": true,
			"github:x/not-installed": true, "github:x/no-main": true,
		},
		VendorRoot: "./ngm.vendor",
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}

	escaping := map[string]bool{}
	fatal := map[string]bool{}
	for _, fi := range findings {
		if fi.EscapesVendor {
			escaping[fi.From] = true
			if !fi.Fatal {
				t.Errorf("an escaping finding must be fatal too, got %+v", fi)
			}
		}
		if fi.Fatal {
			fatal[fi.From] = true
		}
	}

	if !escaping["github:x/escape-to"] {
		t.Error("`to` outside the vendor root must be marked EscapesVendor")
	}
	if !escaping["github:x/escape-main"] {
		t.Error("`main` resolving outside `to` must be marked EscapesVendor " +
			"(its target exists, so the existence check alone would have let it through)")
	}
	if escaping["github:x/not-installed"] {
		t.Error("a `to` directory that is not installed yet must NOT be marked as escaping — " +
			"integrations has to keep working before `ngm install`")
	}
	if escaping["github:x/no-main"] {
		t.Error("a missing `main` file must not be marked as escaping")
	}
	// 反向确认：这三条仍然是 Fatal（只有分类不同，严重度没变）。
	for _, from := range []string{"github:x/escape-to", "github:x/escape-main", "github:x/no-main"} {
		if !fatal[from] {
			t.Errorf("%s must still be a fatal finding", from)
		}
	}
}
