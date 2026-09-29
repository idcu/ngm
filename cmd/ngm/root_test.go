package main

import (
	"flag"
	"reflect"
	"testing"
)

// TestNormalizeArgs 锁定参数重排的正确性。
//
// 背景：Go 标准库 flag 在遇到第一个 positional 后即停止解析，
// 而用户自然地写 `ngm init <name> --dir <path>`。normalizeArgs 负责把 flag
// （含 `--key value` 的**两个相邻 token**）提到前面，同时不吞掉真正的 positional。
func TestNormalizeArgs(t *testing.T) {
	specs := []flagSpec{
		{Name: "dir"},
		{Name: "runtime"},
		{Name: "force", Bool: true},
	}
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "positional before flags",
			in:   []string{"my-app", "--runtime=node", "--dir=x"},
			want: []string{"--runtime=node", "--dir=x", "my-app"},
		},
		{
			name: "flag with separate value",
			in:   []string{"my-app", "--dir", "/tmp/x"},
			want: []string{"--dir", "/tmp/x", "my-app"},
		},
		{
			name: "bool flag does not swallow positional",
			in:   []string{"--force", "my-app"},
			want: []string{"--force", "my-app"},
		},
		{
			name: "bool flag before positional before valued flag",
			in:   []string{"--force", "my-app", "--dir", "/p"},
			want: []string{"--force", "--dir", "/p", "my-app"},
		},
		{
			name: "value flag followed by flag (no value to pair)",
			in:   []string{"--dir", "--force", "app"},
			want: []string{"--dir", "--force", "app"},
		},
		{
			name: "double dash terminates flag scanning",
			in:   []string{"--dir=x", "--", "--not-a-flag"},
			want: []string{"--dir=x", "--not-a-flag"},
		},
		{
			name: "no flags at all",
			in:   []string{"a", "b"},
			want: []string{"a", "b"},
		},
		{
			name: "short flag with equals",
			in:   []string{"app", "-dir=y"},
			want: []string{"-dir=y", "app"},
		},
		{
			name: "unknown long flag treated as valued",
			in:   []string{"app", "--unknown", "v"},
			want: []string{"--unknown", "v", "app"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeArgs(tc.in, specs)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("normalizeArgs(%v)\n got %v\nwant %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormalizeArgs_ParsesAsExpected 用真实的 FlagSet 端到端验证：
// 重排后 `ngm init <name> --dir <path>` 能被正确解析。
func TestNormalizeArgs_ParsesAsExpected(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	dir := fs.String("dir", ".", "")
	runtime := fs.String("runtime", "node", "")
	force := fs.Bool("force", false, "")

	args := []string{"github.com:my-org/app", "--dir", "/tmp/project", "--runtime=deno", "--force"}
	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "runtime"}, {Name: "force", Bool: true},
	})); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if *dir != "/tmp/project" {
		t.Errorf("dir=%q", *dir)
	}
	if *runtime != "deno" {
		t.Errorf("runtime=%q", *runtime)
	}
	if !*force {
		t.Errorf("force should be true")
	}
	if fs.NArg() != 1 || fs.Arg(0) != "github.com:my-org/app" {
		t.Errorf("positional args=%v", fs.Args())
	}
}

// TestNormalizeArgs_BoolDoesNotEatPositional 是回归防线：
// `ngm update --all github:o/r` 必须保留 `github:o/r` 为 positional，
// 这样 runUpdate 才能报告"--all 与显式依赖不能同时使用"。
func TestNormalizeArgs_BoolDoesNotEatPositional(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	all := fs.Bool("all", false, "")
	_ = fs.Parse(normalizeArgs([]string{"--all", "github:o/r"}, []flagSpec{
		{Name: "all", Bool: true},
	}))
	if !*all {
		t.Errorf("--all not set")
	}
	if fs.NArg() != 1 || fs.Arg(0) != "github:o/r" {
		t.Errorf("positional lost: %v", fs.Args())
	}
}
