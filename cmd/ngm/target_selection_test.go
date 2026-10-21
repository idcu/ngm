package main

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/lock"
)

// v0.101：一个仓库多个模块后，`update` / `verify` 的目标必须能区分 subPath。
// 只给仓库名且存在多个候选时，必须报歧义并列出候选——静默全选会让
// "我只更新/只验证了一个模块"变成假象。
func TestSelectTargetsDistinguishesSubPaths(t *testing.T) {
	pf := &config.ProjectFile{
		Dependencies: []config.Dependency{
			{Name: "github:acme/modules", Ref: "packages/a/v1", RefType: config.RefTypeTag, Path: "packages/a"},
			{Name: "github:acme/modules", Ref: "packages/b/v1", RefType: config.RefTypeTag, Path: "packages/b"},
			{Name: "github:acme/lib", Ref: "v1", RefType: config.RefTypeTag},
		},
	}

	t.Run("full target selects one", func(t *testing.T) {
		got, err := selectTargets(pf, []string{"github:acme/modules#path=packages/a"}, false)
		if err != nil {
			t.Fatalf("selectTargets: %v", err)
		}
		if len(got) != 1 || got[0].Path != "packages/a" {
			t.Fatalf("got %+v, want packages/a", got)
		}
	})

	t.Run("bare repo with multiple sub-paths is ambiguous", func(t *testing.T) {
		_, err := selectTargets(pf, []string{"github:acme/modules"}, false)
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "ambiguous") ||
			!strings.Contains(msg, "packages/a") ||
			!strings.Contains(msg, "packages/b") {
			t.Errorf("ambiguity must list candidates, got %q", msg)
		}
	})

	t.Run("bare repo with one module is fine", func(t *testing.T) {
		got, err := selectTargets(pf, []string{"github:acme/lib"}, false)
		if err != nil {
			t.Fatalf("selectTargets: %v", err)
		}
		if len(got) != 1 || got[0].Name != "github:acme/lib" {
			t.Fatalf("got %+v", got)
		}
	})
}

func TestFilterLockDistinguishesSubPaths(t *testing.T) {
	lf := lock.NewFile()
	lf.Dependencies = []lock.Dependency{
		{Name: "github:acme/modules", SubPath: "packages/a", Commit: "a"},
		{Name: "github:acme/modules", SubPath: "packages/b", Commit: "b"},
		{Name: "github:acme/lib", Commit: "c"},
	}

	t.Run("full target selects one", func(t *testing.T) {
		got, err := filterLock(lf, []string{"github:acme/modules#path=packages/a"})
		if err != nil {
			t.Fatalf("filterLock: %v", err)
		}
		if len(got.Dependencies) != 1 || got.Dependencies[0].SubPath != "packages/a" {
			t.Fatalf("got %+v", got.Dependencies)
		}
	})

	t.Run("bare repo with multiple sub-paths is ambiguous", func(t *testing.T) {
		_, err := filterLock(lf, []string{"github:acme/modules"})
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "ambiguous") ||
			!strings.Contains(msg, "packages/a") ||
			!strings.Contains(msg, "packages/b") {
			t.Errorf("ambiguity must list candidates, got %q", msg)
		}
	})

	t.Run("bare repo with one module is fine", func(t *testing.T) {
		got, err := filterLock(lf, []string{"github:acme/lib"})
		if err != nil {
			t.Fatalf("filterLock: %v", err)
		}
		if len(got.Dependencies) != 1 || got.Dependencies[0].Commit != "c" {
			t.Fatalf("got %+v", got.Dependencies)
		}
	})
}
