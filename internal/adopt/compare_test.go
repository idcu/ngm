package adopt

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.109：一份证明如果没人再读，它和一个从不复核的审计结论是同一件事。
//
// `--check` 是这条路的后半段，而它的判据必须钉住三件事：**按 Path 比较**、
// **三种形态分得清**、**顺序确定**。前两条错了都不会崩——只会给出一个
// 看起来合理的错误答案，这正是本仓库一直在猎的那一族。

func proofOf(pkgs ...Package) *Proof {
	return &Proof{Version: ProofVersion, Tool: Tool{Name: "ngm", Version: "test"}, Packages: pkgs}
}

func pkg(path, name, digest string) Package {
	return Package{Path: path, Name: name, Version: "1.0.0", Digest: digest}
}

const (
	d1 = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	d2 = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestV109IdenticalTreesHaveNoDifferences(t *testing.T) {
	a := proofOf(pkg("node_modules/a", "a", d1), pkg("node_modules/b", "b", d2))
	b := proofOf(pkg("node_modules/a", "a", d1), pkg("node_modules/b", "b", d2))
	if got := Compare(a, b); len(got) != 0 {
		t.Fatalf("two scans of the same tree must differ in nothing, got %+v", got)
	}
}

func TestV109ThreeKindsAreDistinguishable(t *testing.T) {
	oldP := proofOf(
		pkg("node_modules/changed", "changed", d1),
		pkg("node_modules/gone", "gone", d1),
	)
	curP := proofOf(
		pkg("node_modules/changed", "changed", d2),
		pkg("node_modules/new", "new", d2),
	)

	changes := Compare(oldP, curP)
	if len(changes) != 3 {
		t.Fatalf("want 3 differences, got %d: %+v", len(changes), changes)
	}
	byPath := map[string]Change{}
	for _, c := range changes {
		byPath[c.Path] = c
	}

	if c := byPath["node_modules/changed"]; c.Kind != ChangeChanged || c.Want != d1 || c.Got != d2 {
		t.Errorf("a changed digest must carry both sides: %+v", c)
	}
	// 少了的那条**不能**给出 Got（它现在没有内容），多了的那条不能给出 Want。
	if c := byPath["node_modules/gone"]; c.Kind != ChangeMissing || c.Want != d1 || c.Got != "" {
		t.Errorf("missing entry: %+v", c)
	}
	if c := byPath["node_modules/new"]; c.Kind != ChangeExtra || c.Want != "" || c.Got != d2 {
		t.Errorf("extra entry: %+v", c)
	}
}

// 按 **Path** 而不是按 Name 索引。
//
// 同一个包名可以在两处出现（顶层 node_modules/b 与 node_modules/a/node_modules/b
// 可能是不同版本）。按 Name 比较会把它们合并，于是"嵌套依赖被换了"这种变化
// 会被静默吞掉——而嵌套依赖被换正是供应链攻击最常见的位置。
func TestV109SameNameAtTwoPathsIsNotMerged(t *testing.T) {
	oldP := proofOf(
		pkg("node_modules/a/node_modules/b", "b", d1),
		pkg("node_modules/b", "b", d1),
	)
	curP := proofOf(
		pkg("node_modules/a/node_modules/b", "b", d2), // 只有嵌套那个变了
		pkg("node_modules/b", "b", d1),
	)

	changes := Compare(oldP, curP)
	if len(changes) != 1 {
		t.Fatalf("want exactly one difference, got %d: %+v", len(changes), changes)
	}
	if changes[0].Path != "node_modules/a/node_modules/b" {
		t.Errorf("the difference must name the nested path, got %q", changes[0].Path)
	}
}

func TestV109OrderIsDeterministic(t *testing.T) {
	oldP := proofOf(
		pkg("node_modules/z", "z", d1),
		pkg("node_modules/a", "a", d1),
		pkg("node_modules/m", "m", d1),
	)
	curP := proofOf()

	first := Compare(oldP, curP)
	for i := 0; i < 5; i++ {
		again := Compare(oldP, curP)
		for j := range first {
			if first[j].Path != again[j].Path {
				t.Fatalf("two runs disagreed on order at %d: %q vs %q", j, first[j].Path, again[j].Path)
			}
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Path > first[i].Path {
			t.Fatalf("changes must be sorted by path: %q before %q", first[i-1].Path, first[i].Path)
		}
	}
}

// `--check` 后面很容易写错文件名。一个宽松的解析器会把它读成"零个包"，
// 于是比较结果是"全都是 extra"——那看起来像一次灾难，而原因只是指错了文件。
func TestV109ParseProofRefusesTheWrongFile(t *testing.T) {
	bad := []struct {
		name string
		in   string
	}{
		{"not JSON", `{"lockfileVersion":"2.0.0"`},
		{"a lock file", `{"version":1,"lockfileVersion":"2.0.0","dependencies":[]}`},
		{"an empty object", `{}`},
		{"a future schema", `{"version":99,"packages":[{"path":"node_modules/a"}]}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseProof([]byte(tc.in)); err == nil {
				t.Error("this file must be refused, not read as a proof with zero packages")
			}
		})
	}

	// 反向：自己写出来的东西必须能读回来（含 notes 与 coverage）。
	p := proofOf(pkg("node_modules/a", "a", d1))
	p.Notes = []string{"a note"}
	p.Coverage = Coverage{Packages: 1, WithCommit: 1}
	raw, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseProof(raw)
	if err != nil {
		t.Fatalf("a proof must survive a round trip: %v", err)
	}
	if len(back.Packages) != 1 || back.Packages[0].Digest != d1 || back.Coverage.WithCommit != 1 {
		t.Errorf("round trip lost data: %+v", back)
	}
	if len(Compare(back, p)) != 0 {
		t.Error("a round-tripped proof must compare equal to the original")
	}
}

func TestV109ParseProofErrorNamesTheFileToPointAt(t *testing.T) {
	_, err := ParseProof([]byte(`{}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	// 建议在 hint 上（`errs` 的 message 与 hint 是两个通道），所以断言 hint。
	if !strings.Contains(errs.Hint(err), FileName) {
		t.Errorf("the hint must name the file to point --check at (%s), got: %q", FileName, errs.Hint(err))
	}
}
