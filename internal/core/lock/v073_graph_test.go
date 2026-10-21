// v0.73：依赖图形进 lock（[ADR-028](../../docs/adr/adr-028-dependency-graph-in-lock.md)）。
//
// 这组判据守的是三件事：字段**恒在且归一**、**幽灵边**被拒、**图是否完整**有唯一判据。
// 三者任一失守，`ngm why` 都会少列一条路径——而"少列一条"在溯源问题里与说谎等价。

package lock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/resolve"
)

// TestRootRequirerMatchesResolve 钉住一处**跨包的同值约定**：
// lock 是格式层，不 import resolve，但两个标记必须同值，否则图会静默错边。
func TestRootRequirerMatchesResolve(t *testing.T) {
	if RootRequirer != resolve.RootMarker {
		t.Errorf("lock.RootRequirer=%q 与 resolve.RootMarker=%q 不同值——"+
			"lock 里的根标记会被 resolve 当成一个普通节点 Key", RootRequirer, resolve.RootMarker)
	}
}

// TestV73RequiredByIsNormalizedAndAlwaysPresent 序列化的三条纪律：
// 恒在、排序、去重——且永远不出现 `null`。
func TestV73RequiredByIsNormalizedAndAlwaysPresent(t *testing.T) {
	f := NewFile()
	f.Dependencies = []Dependency{
		{
			Name: "github:a/app", Ref: "v1", RefType: "tag",
			Commit:        strings.Repeat("a", 40),
			ArchiveDigest: "sha256:" + strings.Repeat("aa", 32),
			VendorPath:    "github.com/a/app",
			// 乱序 + 重复 + 带根标记：写出时必须归一
			RequiredBy: []string{"github:b/util", RootRequirer, "github:b/util", "github:a/other"},
		},
		{
			Name: "github:b/util", Ref: "v2", RefType: "tag",
			Commit:        strings.Repeat("b", 40),
			ArchiveDigest: "sha256:" + strings.Repeat("bb", 32),
			VendorPath:    "github.com/b/util",
			RequiredBy:    []string{RootRequirer},
		},
		{
			Name: "github:a/other", Ref: "v3", RefType: "tag",
			Commit:        strings.Repeat("c", 40),
			ArchiveDigest: "sha256:" + strings.Repeat("cc", 32),
			VendorPath:    "github.com/a/other",
			// nil：必须写成 `[]`，而不是 `null`
			RequiredBy: nil,
		},
	}
	f.Dependencies = append(f.Dependencies, Dependency{
		Name: "github:a/other#sub", Ref: "v3", RefType: "tag",
		Commit:       strings.Repeat("d", 40),
		ModuleDigest: "sha256:" + strings.Repeat("dd", 32),
		SubtreeOid:   "sha1:" + strings.Repeat("ee", 20),
		VendorPath:   "github.com/a/other", SubPath: "sub",
		RequiredBy: []string{"github:a/app"},
	})

	data, err := f.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	if strings.Contains(s, `"requiredBy": null`) {
		t.Error("序列化里不许出现 `null`——空与缺失必须是同一种写法（来源未知）")
	}
	if !strings.Contains(s, `"requiredBy": []`) {
		t.Error("nil 必须写成 `[]`（来源未知只有一种写法）")
	}

	// 第一个条目的 requiredBy：排序 + 去重后的顺序。
	want := []string{`"(root)"`, `"github:a/other"`, `"github:b/util"`}
	last := -1
	for _, w := range want {
		idx := strings.Index(s, w)
		if idx < 0 {
			t.Fatalf("缺少引用者 %s:\n%s", w, s)
		}
		if idx < last {
			t.Errorf("requiredBy 没有排序:\n%s", s)
		}
		last = idx
	}
	// 去重：b/util 只该出现两次（一次作为 name，一次作为引用者）。
	if got := strings.Count(s, `"github:b/util"`); got != 2 {
		t.Errorf("重复的引用者没有被去掉（出现 %d 次，应为 2）:\n%s", got, s)
	}
}

// TestV73GhostEdgesAreRejected 幽灵边必须在**读取时**就红——
// 而不是等到 `why` 少列一条路径时才发现。
func TestV73GhostEdgesAreRejected(t *testing.T) {
	f := NewFile()
	f.Dependencies = []Dependency{
		{
			Name: "github:a/app", Ref: "v1", RefType: "tag",
			Commit:        strings.Repeat("a", 40),
			ArchiveDigest: "sha256:" + strings.Repeat("aa", 32),
			VendorPath:    "github.com/a/app",
			RequiredBy:    []string{RootRequirer},
		},
		{
			Name: "github:b/util", Ref: "v2", RefType: "tag",
			Commit:        strings.Repeat("b", 40),
			ArchiveDigest: "sha256:" + strings.Repeat("bb", 32),
			VendorPath:    "github.com/b/util",
			RequiredBy:    []string{"github:ghost/gone"},
		},
	}
	err := f.Validate()
	if err == nil {
		t.Fatal("幽灵边必须被拒绝——lock 是唯一事实源，事实源里不许有悬空引用")
	}
	if !strings.Contains(err.Error(), "ghost edge") {
		t.Errorf("错误要说清这是幽灵边（指向不在 lock 里的条目）: %v", err)
	}
	if !strings.Contains(err.Error(), "github:ghost/gone") {
		t.Errorf("错误要点名那个不存在的引用者: %v", err)
	}
	if hint := errs.Hint(err); !strings.Contains(hint, "ngm install") {
		t.Errorf("必须给出下一步: %q", hint)
	}
}

// TestV73HasGraph 图是否可用只有一个判据：**每个**条目都有引用者。
// 少一条边 ⇒ 整体不可用（`why` 退回重新解析并说明来源），不做"部分图"。
func TestV73HasGraph(t *testing.T) {
	withGraph := NewFile()
	withGraph.Dependencies = []Dependency{
		{Name: "github:a/app", RequiredBy: []string{RootRequirer}},
		{Name: "github:b/util", RequiredBy: []string{"github:a/app"}},
	}
	if !withGraph.HasGraph() {
		t.Error("每个条目都有引用者 ⇒ 图可用")
	}

	partial := NewFile()
	partial.Dependencies = []Dependency{
		{Name: "github:a/app", RequiredBy: []string{RootRequirer}},
		{Name: "github:b/util"}, // 旧 lock 或手改：来源未知
	}
	if partial.HasGraph() {
		t.Error("有条目缺来源 ⇒ 图不完整，必须退回（不做「部分图」）")
	}

	// 没有 requiredBy 字段的 lock 解码后同样是"图未知"（v2 里仍允许）。
	old := `{"version":1,"lockfileVersion":"2.0.0","dependencies":[{"name":"github:a/app","ref":"v1","refType":"tag","commit":"` +
		strings.Repeat("a", 40) + `","archiveDigest":"sha256:` + strings.Repeat("aa", 32) +
		`","resolvedAt":"2026-09-29T10:00:00Z","vendorPath":"github.com/a/app"}]}`
	f, err := decodeForTest(old)
	if err != nil {
		t.Fatalf("没有 requiredBy 的 lock 必须仍然可读: %v", err)
	}
	if f.HasGraph() {
		t.Error("没有图的 lock —— `why` 应当退回重新解析")
	}
}

// TestV73OldLocksStillRead 格式契约的一条底线：同一 MAJOR 内新增可选字段不得让 lock 读不进来。
func TestV73OldLocksStillRead(t *testing.T) {
	f, err := decodeForTest(`{"version":1,"lockfileVersion":"2.0.0","dependencies":[]}`)
	if err != nil {
		t.Fatalf("同 MAJOR 的 lock 读不进来了: %v", err)
	}
	if len(f.Dependencies) != 0 {
		t.Errorf("dependencies = %d", len(f.Dependencies))
	}
}

// decodeForTest 直接解码一份 lock 文本（绕开 Read 的文件 IO），
// 用于"旧版 lock"的前向兼容检查。
func decodeForTest(s string) (*File, error) {
	var f File
	if err := json.Unmarshal([]byte(s), &f); err != nil {
		return nil, err
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}
