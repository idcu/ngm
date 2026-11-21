// Package runid 把"一个将要执行的程序"归一成权限判定用的身份。
//
// 它只回答两个问题：这个声明最终指向哪个文件，以及这个文件在项目内还是项目外。
// **不做策略裁决**——allow/deny/默认档位都在 internal/security。
//
// 单独成包的理由是 adapter（探测与执行）和 security（判定）必须共用同一套归一化
// 规则：两边各写一份，迟早会退化成"判定看的是 A、实际执行的是 B"。
package runid

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Identity 是一次 `run:` 判定要看的身份。
type Identity struct {
	// Name 是权限名（如 git / esbuild / engine.wasm），已去掉 Windows 可执行后缀。
	Name string
	// Program 是清单或调用点里**原始声明**的值（./git、../x/tool、C:\tools\x.exe、esbuild）。
	Program string
	// Path 是解析后的绝对路径（尚未展开符号链接）；空表示未解析或找不到。
	Path string
	// ProjectDir 是项目根；为空时不做"项目内"判定。
	ProjectDir string
}

// PathForm 报告声明是否写成路径形式，而不是交给 PATH 解析的裸名字。
//
// `./git`、`../x/tool`、`cmd/foo.exe`、`/usr/bin/git`、`C:\tools\git.exe` 都是
// 路径形式；`git`、`esbuild` 不是。Windows 的盘符相对写法 `C:foo` 也算路径形式，
// 因为它同样绕开了 PATH 与"裸名字"的默认档位语义。
func (id Identity) PathForm() bool {
	p := strings.TrimSpace(id.Program)
	if p == "" {
		return false
	}
	if p == "." || p == ".." {
		return true
	}
	if strings.HasPrefix(p, "."+string(filepath.Separator)) || strings.HasPrefix(p, "./") || strings.HasPrefix(p, `.\`) {
		return true
	}
	if filepath.IsAbs(p) {
		return true
	}
	// 盘符相对路径（C:foo）与含分隔符的相对路径。
	if len(p) >= 2 && p[1] == ':' && runtime.GOOS == "windows" {
		return true
	}
	return strings.ContainsAny(p, `/\`)
}

// LocalRel 报告解析后的 Path 是否落在 ProjectDir 内，并返回 slash 分隔的相对路径
// （不带前导 "./"）。比较前两侧都展开符号链接：项目里指向外面的软链不算项目内，
// 指向项目内的软链算项目内——判定看的是**将要执行的实体**，不是链接本身。
func (id Identity) LocalRel() (string, bool) {
	if id.Path == "" || id.ProjectDir == "" {
		return "", false
	}
	root := realPath(id.ProjectDir)
	target := realPath(id.Path)
	rel, ok := relWithin(root, target)
	if !ok && runtime.GOOS == "windows" {
		// Windows 路径大小写不敏感；EvalSymlinks 可能保留调用方的大小写。
		rel, ok = relWithin(strings.ToLower(root), strings.ToLower(target))
	}
	if !ok {
		return "", false
	}
	return rel, true
}

// IsLocal 报告 Path 是否解析到项目内。
func (id Identity) IsLocal() bool {
	_, ok := id.LocalRel()
	return ok
}

// DisplayTarget 是提示信息与 `engines list` 展示的权限目标。
//
// 项目内的可执行文件给路径形式（`./tools/git`），因为裸名字授权不覆盖它们；
// 其余沿用名字，保持 `run:esbuild` / `run:git` 的既有表述。
func (id Identity) DisplayTarget() string {
	if rel, ok := id.LocalRel(); ok {
		return "./" + rel
	}
	if name := strings.TrimSpace(id.Name); name != "" {
		return name
	}
	return ""
}

// Targets 返回可用于匹配**路径授权**的归一化目标（已去重）。
//
// 同时给出解析后的实体路径、按声明词法解析出的路径、以及原始声明形式，
// 使 `run:./git`、`run:git`（经 Clean 后同为 git）与绝对路径写法都能命中。
// 裸名字不会进入这个集合——那是名字授权的匹配空间，两者必须分开。
func (id Identity) Targets() []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		n := Normalize(s)
		if n == "" {
			return
		}
		for _, existing := range out {
			if existing == n {
				return
			}
		}
		out = append(out, n)
	}

	if rel, ok := id.LocalRel(); ok {
		add("./" + rel)
	}
	if id.Path != "" {
		add(filepath.ToSlash(realPath(id.Path)))
	}
	if id.PathForm() {
		p := strings.TrimSpace(id.Program)
		add(p)
		if !filepath.IsAbs(p) && id.ProjectDir != "" {
			add(filepath.Join(id.ProjectDir, filepath.FromSlash(p)))
		}
	}
	return out
}

// Normalize 把权限目标归一成可比较的形式：统一分隔符、Clean、Windows 下折叠大小写。
//
// 例："./tools/git" 与 "tools/git" 都归一为 "tools/git"；`C:\Tools\X.EXE` 在
// Windows 下归一为 "c:/tools/x.exe"。
func Normalize(target string) string {
	s := strings.TrimSpace(target)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, `\`, "/")
	s = path.Clean(s)
	if runtime.GOOS == "windows" {
		s = strings.ToLower(s)
	}
	return s
}

func realPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return abs
}

func relWithin(root, target string) (string, bool) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
