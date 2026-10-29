package vendor

import (
	"testing"
)

// --- v0.105：大小写折叠文件系统上的落地必须 fail-closed ---

// 缺陷形态（v0.105 在 Windows 上实测）：仓库里同时有 `Foo.txt` 与 `foo.txt` 时，
// 落地第二个条目会因为 EEXIST 降级为 copy，再以 O_TRUNC 覆盖同一个文件 ——
// `ngm install` 打印 "installed 1 dependencies"，vendor 里只剩一个文件，
// 而紧接的 `ngm verify` 报 critical exit 2。也就是说：**install 成功，
// 但它落地出来的树必然验不过**。
func TestV105CaseCollisionIsDetected(t *testing.T) {
	cases := []struct {
		name    string
		entries []ContentEntry
		want    bool
	}{
		{"仅大小写不同", []ContentEntry{{Path: "Foo.txt"}, {Path: "foo.txt"}}, true},
		{"大小写不同的目录", []ContentEntry{{Path: "Src/A.ts"}, {Path: "src/a.ts"}}, true},
		{"完全无关", []ContentEntry{{Path: "a.txt"}, {Path: "b.txt"}}, false},
		// 同一个路径出现两次是另一个缺陷（由 digest / lock 层看着）。
		// 这里**不**把它算成大小写冲突：判据的每条断言都该只对一件事负责。
		{"完全相同的路径不算大小写冲突", []ContentEntry{{Path: "a.txt"}, {Path: "a.txt"}}, false},
		{"空集合", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, got := caseCollision(tc.entries)
			if got != tc.want {
				t.Errorf("caseCollision = %v, want %v", got, tc.want)
			}
		})
	}
}

// 组合层：判据跟随**磁盘**而不是 GOOS。
//
// 在大小写敏感的文件系统上，同一份清单是可以忠实落地的（两个文件真的并存），
// 因此这里必须放行——否则会把 Linux 上的合法仓库误拒。断言按探测结果分支，
// 与实现读同一个事实源（`isCaseInsensitiveDir`），而不是猜平台。
func TestV105CaseCollisionGuardFollowsTheFilesystem(t *testing.T) {
	dir := t.TempDir()
	colliding := []ContentEntry{{Path: "Foo.txt"}, {Path: "foo.txt"}}
	benign := []ContentEntry{{Path: "a.txt"}, {Path: "b.txt"}}

	if err := ensureNoCaseCollisions(benign, dir); err != nil {
		t.Errorf("non-colliding entries must always be allowed: %v", err)
	}

	if !isCaseInsensitiveDir(dir) {
		if err := ensureNoCaseCollisions(colliding, dir); err != nil {
			t.Errorf("this filesystem is case-sensitive: the collision is representable, so it must not be refused: %v", err)
		}
		t.Skip("case-sensitive volume: the fail-closed half of this judgment is not exercisable here")
	}

	if err := ensureNoCaseCollisions(colliding, dir); err == nil {
		t.Error("on a case-folding filesystem a collision must be refused *before* the vendor is cleaned " +
			"(otherwise install reports success and the next verify reports critical)")
	}
}
