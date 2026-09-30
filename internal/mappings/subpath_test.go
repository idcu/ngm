package mappings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerate_RecordsSubPath 固定 v0.3 E 组的协议扩展：
// monorepo 子路径依赖在 mappings 里要有 `path`，消费方据此拼导入标识符。
func TestGenerate_RecordsSubPath(t *testing.T) {
	// VendorRelPath 由调用方给出，monorepo 时它**已包含**子路径
	// （env.go 用 vendor.VendorPathFor(MirrorRelPath, SubPath) 拼出来）。
	// Generate 只负责把 SubPath 如实写进 `path`；两者是否自洽由 Validate 校验。
	f, _ := Generate([]GenerateInput{
		{From: "github:org/repo", VendorRelPath: "github.com/org/repo/packages/core", SubPath: "packages/core"},
		{From: "github:org/plain", VendorRelPath: "github.com/org/plain"},
	})

	if len(f.Mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(f.Mappings))
	}
	byFrom := map[string]Mapping{}
	for _, m := range f.Mappings {
		byFrom[m.From] = m
	}

	core := byFrom["github:org/repo"]
	if core.Path != "packages/core" {
		t.Errorf("`path` = %q, want packages/core", core.Path)
	}
	if !strings.HasSuffix(core.To, "/packages/core") {
		t.Errorf("`to` should point into the subpath, got %q", core.To)
	}

	// 无子路径的依赖不带 `path`：缺省即"依赖根"，这是 v1 文件的语义
	if got := byFrom["github:org/plain"].Path; got != "" {
		t.Errorf("a non-monorepo dependency must not carry `path`, got %q", got)
	}
}

// `path` 序列化时省略空值，否则 v1 消费者会读到一堆无意义的空字段。
func TestMapping_PathIsOmittedWhenEmpty(t *testing.T) {
	f := NewFile()
	f.Mappings = []Mapping{{From: "github:org/a", To: "./ngm.vendor/github.com/org/a"}}
	data, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"path"`) {
		t.Errorf("empty `path` must be omitted:\n%s", data)
	}
}

// TestMapping_PathShapeIsRejected `path` 最终会参与拼出文件系统路径，
// 因此协议层就拦掉绝对路径与 `..`——比让每个消费方各自小心可靠。
func TestMapping_PathShapeIsRejected(t *testing.T) {
	for _, bad := range []string{"../outside", "/abs/path", `packages\core`, " packages/core", "packages/../x"} {
		f := NewFile()
		f.Mappings = []Mapping{{From: "github:org/a", To: "./ngm.vendor/github.com/org/a", Path: bad}}
		if err := f.Validate(); err == nil {
			t.Errorf("`path` %q should be rejected", bad)
		}
	}

	f := NewFile()
	f.Mappings = []Mapping{{From: "github:org/a", To: "./ngm.vendor/github.com/org/a", Path: "packages/core"}}
	if err := f.Validate(); err != nil {
		t.Errorf("a well-formed `path` must pass: %v", err)
	}
}

// TestValidate_SubPathConsistency 校验 `path` 与 lock / `to` 三者自洽。
//
// 这三者不一致时，集成脚手架会生成一条指向别处的别名，
// 而症状在运行时表现为"模块找不到"——很难回溯到 mappings。
func TestValidate_SubPathConsistency(t *testing.T) {
	proj := t.TempDir()
	writeVendorTree(t, proj, map[string]string{
		"ngm.vendor/github.com/org/mono/packages/core/index.js": "x",
		"ngm.vendor/github.com/org/mono/other/index.js":         "x",
		"ngm.vendor/github.com/org/mono/index.js":               "x",
	})

	env := func(subPaths map[string][]string) ValidateEnvironment {
		return ValidateEnvironment{
			ProjectDir: proj,
			LockNames:  map[string]bool{"github:org/mono": true},
			SubPaths:   subPaths,
		}
	}

	t.Run("a locked subpath is accepted", func(t *testing.T) {
		f := NewFile()
		f.Mappings = []Mapping{{
			From: "github:org/mono", Path: "packages/core",
			To: "./ngm.vendor/github.com/org/mono/packages/core", Main: "./index.js",
		}}
		findings, err := Validate(f, env(map[string][]string{"github:org/mono": {"packages/core"}}))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("expected no findings, got %v", findings)
		}
	})

	t.Run("an unlocked subpath is fatal", func(t *testing.T) {
		f := NewFile()
		f.Mappings = []Mapping{{
			From: "github:org/mono", Path: "packages/typo",
			To: "./ngm.vendor/github.com/org/mono/packages/typo",
		}}
		findings, err := Validate(f, env(map[string][]string{"github:org/mono": {"packages/core"}}))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) == 0 || !findings[0].Fatal {
			t.Fatalf("an unlocked `path` must be fatal, got %v", findings)
		}
		// 失败信息要能直接指出"lock 里其实是什么"
		if !strings.Contains(findings[0].Message, "packages/core") {
			t.Errorf("the finding should name the locked subpath: %v", findings[0])
		}
	})

	t.Run("path and to must agree", func(t *testing.T) {
		f := NewFile()
		f.Mappings = []Mapping{{
			From: "github:org/mono", Path: "packages/core",
			To: "./ngm.vendor/github.com/org/mono/other",
		}}
		findings, err := Validate(f, env(map[string][]string{"github:org/mono": {"packages/core"}}))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, fd := range findings {
			if fd.Fatal && strings.Contains(fd.Message, "does not end with the declared `path`") {
				found = true
			}
		}
		if !found {
			t.Errorf("`to` disagreeing with `path` must be fatal, got %v", findings)
		}
	})

	// v1 文件（无 `path`）在 lock 记着子路径时不该被判错——它是**旧**，不是**错**。
	// 给警告并指明怎么修，是"向后兼容读取旧文件"的具体含义。
	t.Run("a v1 file without path is a warning, not an error", func(t *testing.T) {
		f := NewFile()
		f.Mappings = []Mapping{{
			From: "github:org/mono",
			To:   "./ngm.vendor/github.com/org/mono/packages/core", Main: "./index.js",
		}}
		findings, err := Validate(f, env(map[string][]string{"github:org/mono": {"packages/core"}}))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 1 {
			t.Fatalf("expected exactly one finding, got %v", findings)
		}
		if findings[0].Fatal {
			t.Errorf("a missing `path` on a v1 file must not be fatal: %v", findings[0])
		}
		if !strings.Contains(findings[0].Message, "ngm install") {
			t.Errorf("the warning should say how to fix it: %v", findings[0])
		}
	})

	// 没有子路径的普通依赖：`path` 为空是正常状态，不该有任何发现
	t.Run("a non-monorepo dependency is unaffected", func(t *testing.T) {
		f := NewFile()
		f.Mappings = []Mapping{{
			From: "github:org/mono",
			To:   "./ngm.vendor/github.com/org/mono", Main: "./index.js",
		}}
		findings, err := Validate(f, env(nil))
		if err != nil {
			t.Fatal(err)
		}
		if len(findings) != 0 {
			t.Errorf("expected no findings, got %v", findings)
		}
	})
}

// TestRead_V1FileWithoutPathStaysReadable 是"向后兼容 v1"的字面含义：
// 旧文件（没有 `path`）必须仍能被读入并通过文件级校验。
func TestRead_V1FileWithoutPathStaysReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	v1 := `{
  "version": 1,
  "mappings": [
    {"from": "github:org/a", "to": "./ngm.vendor/github.com/org/a", "main": "./index.js"}
  ]
}
`
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := Read(path)
	if err != nil {
		t.Fatalf("a v1 file must remain readable: %v", err)
	}
	if len(f.Mappings) != 1 || f.Mappings[0].Path != "" {
		t.Fatalf("unexpected parse result: %+v", f.Mappings)
	}
}

// 反过来也要成立：v1 **读者**看到带 `path` 的文件不会崩——
// 按版本策略"旧工具应忽略未知字段继续工作"，新字段不改变既有语义。
func TestFile_UnknownFieldTolerance(t *testing.T) {
	var f File
	body := `{"version":1,"mappings":[{"from":"github:org/a","to":"./ngm.vendor/github.com/org/a","futureField":123}]}`
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("unknown fields must be tolerated: %v", err)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("a file with an unknown field must still validate: %v", err)
	}
}
