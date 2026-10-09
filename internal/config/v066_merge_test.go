package config

import "testing"

// v0.66：`mergeProject` 必须**返回副本**（评估报告 P2#13）。
//
// 修复前它原地修改入参 ⇒ `Resolved.Effective` 与 `Resolved.Project` 是同一指针，
// 与 `Resolved` 的文档注释（"来源"与"合并视图"是两个视图）矛盾：
// 调用方改有效视图会悄悄改掉来源。

func TestV66MergeProjectReturnsACopy(t *testing.T) {
	builtin := DefaultBuiltin()

	project := &ProjectFile{
		Name: "app", Version: "1.0.0",
		Vendor: &VendorConfig{Mode: "local"},
	}
	eff := mergeProject(builtin, project)

	if eff == project {
		t.Fatal("Effective 与 Project 不得是同一指针")
	}

	// 改有效视图不得影响来源（Vendor 是指针字段，单独钉住深拷贝）。
	eff.Runtime = RuntimeDeno
	eff.Vendor.LinkMode = "copy"

	if project.Runtime != "" {
		t.Errorf("改 Effective 改动了 Project.Runtime: %q", project.Runtime)
	}
	if project.Vendor.LinkMode != "" {
		t.Error("Vendor 指针被共享：改 Effective 改动了 Project.Vendor.LinkMode")
	}
}

func TestV66MergeProjectDoesNotWriteBackFallbacks(t *testing.T) {
	builtin := DefaultBuiltin()

	// Vendor 为 nil 的来源：回退值只能出现在返回的副本里。
	src := &ProjectFile{Name: "b", Version: "1.0.0"}
	eff := mergeProject(builtin, src)

	if eff.Vendor == nil {
		t.Fatal("Effective 必须带上 Vendor 的回退值")
	}
	if src.Vendor != nil {
		t.Error("mergeProject 不得把回退的 Vendor 写回来源对象")
	}
	if src.Runtime != "" {
		t.Error("mergeProject 不得把回退的 runtime 写回来源对象")
	}
}
