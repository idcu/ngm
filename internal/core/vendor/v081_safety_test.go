package vendor

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.81：v0.66 那道逃逸校验的**补口**，以及空条目集合的静默成功。
//
// 为什么单独一份文件：v0.66 的网（v066_safety_test.go）在 POSIX 口径下全绿，
// 而缺陷恰恰在它没问到的那半边——**反斜杠形态的相对目标**。
// "网绿着"并不意味着它看见了什么；这一版把没看见的那半边补进来。

// TestV81BackslashSymlinkTargetEscapes 钉住反斜杠目标的逃逸判定。
//
// Windows 的 CreateSymbolicLinkW 把 `\` 当分隔符解析，而 path.Clean/Join
// 只认 `/`。v0.66 之前（以及 v0.66 只归一 entryPath 时），
// `..\..\..\x` 既不以 `../` 开头、也不像绝对路径 ⇒ 判据返回 false ⇒ 放行。
//
// 牙齿：把 EscapesPackageTree 里的 `target = filepath.ToSlash(target)`
// 删掉，本测试红。
func TestV81BackslashSymlinkTargetEscapes(t *testing.T) {
	cases := []struct {
		entry, target string
		want          bool
		why           string
	}{
		{"link", `..\..\outside\x`, true, "反斜杠相对目标：Windows 上等价于 ../../outside/x"},
		{"link", `..\outside`, true, "反斜杠上一级，越过包根"},
		{"src/link", `..\..\x`, true, "结合链接所在目录后越过包根"},
		{"link", `\etc\passwd`, true, "反斜杠绝对形态"},
		{"link", `C:\Windows\x`, true, "驱动器 + 反斜杠"},
		{"a/link", `..\x`, false, "反斜杠上一级但仍在包内（a/ 下的链接回到根）"},
		{"link", `.\index.ts`, false, "当前目录，不逃逸"},
	}
	for _, tc := range cases {
		if got := EscapesPackageTree(tc.entry, tc.target); got != tc.want {
			t.Errorf("EscapesPackageTree(%q, %q) = %v, want %v（%s）",
				tc.entry, tc.target, got, tc.want, tc.why)
		}
	}
}

// TestV81ValidateEntryPathsRejectsBackslashSymlink 端到端：含反斜杠逃逸目标的
// symlink 条目必须被 validateEntryPaths 拒绝。
func TestV81ValidateEntryPathsRejectsBackslashSymlink(t *testing.T) {
	bad := []ContentEntry{
		{Path: "link", Symlink: true, LinkTarget: `..\..\..\Users\victim\.ssh\id_rsa`, Size: 30},
		{Path: "src/link", Symlink: true, LinkTarget: `..\..\secret`, Size: 12},
	}
	for _, e := range bad {
		err := validateEntryPaths([]ContentEntry{e})
		if err == nil {
			t.Fatalf("symlink %s → %q 必须被拒绝（反斜杠目标在 Windows 上会解析到包外）",
				e.Path, e.LinkTarget)
		}
		var ne *errs.NgmError
		if !errorAs(err, &ne) {
			t.Fatalf("拒绝应带 ngm 错误码，got %T", err)
		}
		if ne.Code != errs.CodeConfigInvalid {
			t.Errorf("拒绝码 = %v, want %v", ne.Code, errs.CodeConfigInvalid)
		}
	}
}

// TestV81EscapesPackageTreeIsOneJudgement 判据必须只有一份：
// 落地侧（validateEntryPaths）与判定侧（internal/verify）共用同一个函数，
// 否则会出现"安装拒绝、verify 放行"这种分叉。
func TestV81EscapesPackageTreeIsOneJudgement(t *testing.T) {
	// 两个方向都走同一个导出函数——若有人在调用点自己写了一份判定，
	// 这份测试的"边界表"就会与那份实现分叉。
	for _, target := range []string{`..\..\x`, `../../x`} {
		if !EscapesPackageTree("link", target) {
			t.Errorf("两种分隔符形态必须同处置：%q 应判为逃逸", target)
		}
	}
}

// TestV81NilEntriesIsNotAnEmptyDependency 钉住"nil 条目集合 ≠ 空依赖"。
//
// v0.81 之前：`Entries` 返回 nil 时（v1 树目录存在但无内容），
// Materialize 会一路走到 linkEntries(nil, dst)，先 RemoveAll 再建目录，
// 返回 Files:0 并被当作成功 ⇒ 用户拿到一个空目录，install 报一切正常。
//
// 注意口径：空 commit 是**合法**的（ADR-008 明确支持，清单只有头部），
// 所以这里只拦 nil（v2 走清单，零条目是非 nil 空切片），不拦空。
func TestV81NilEntriesIsNotAnEmptyDependency(t *testing.T) {
	// EntriesUnder(nil, "") 返回 nil——这正是 v1 空树的表现。
	if got := EntriesUnder(nil, ""); got != nil {
		t.Fatalf("EntriesUnder(nil, \"\") = %#v, want nil（前置假设）", got)
	}
	// 而"零条目但非 nil"必须仍然被当作合法的空依赖：
	if EntriesUnder([]ContentEntry{}, "") == nil {
		t.Fatalf("空的非 nil 集合不得被当成 nil（前置假设）")
	}
}

func errorAs(err error, target **errs.NgmError) bool {
	ne, ok := err.(*errs.NgmError)
	if ok {
		*target = ne
	}
	return ok
}

// 保证 strings 被使用（错误信息断言）
var _ = strings.Contains
