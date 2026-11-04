// Package runscript 实现 `ngm run` 的脚本发现与执行。
//
// 它刻意**只**做编排：发现脚本、拼 shell 命令、注入 `node_modules/.bin`、
// 设最小环境变量、原样透传退出码。它不安装依赖、不模拟 npm 配置、
// 不执行依赖的 lifecycle 脚本——那些属于 install 的安全边界（ADR-009），
// 与这里的“用户显式要求执行自己的脚本”是两件事。
//
// 为什么不复用 `internal/adapter` 的引擎执行：引擎的契约是“适配器 + 结构化
// 输入输出 + 退出码语义”，而项目脚本的契约是“平台 shell 原样执行”。把两者
// 揉在一起会让脚本继承一堆它不需要的引擎语义，也会让引擎不敢改自己的参数。
package runscript

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/errs"
)

// Source 是脚本定义的来源。
type Source string

const (
	// SourcePackageJSON 是权威来源。
	SourcePackageJSON Source = "package.json"
	// SourceNgmJSON 是回退来源（package.json 没有同名脚本时）。
	SourceNgmJSON Source = "ngm.json"
)

// Script 是一条可执行脚本。
type Script struct {
	Name    string
	Command string
	Source  Source
	// Pre / Post 是按 npm 约定存在的前后置脚本名（`pre<name>` / `post<name>`）。
	//
	// 空串表示不存在。它们只是**发现结果**，不是额外配置：用户不需要声明它们。
	Pre  string
	Post string
}

// Set 是某个项目的脚本集合。
type Set struct {
	Project        string
	PackageName    string
	PackageVersion string
	// Scripts 按 Name 排序，便于稳定输出。
	Scripts []Script
}

// Lookup 按名字查找脚本。
func (s *Set) Lookup(name string) (Script, bool) {
	if s == nil {
		return Script{}, false
	}
	for _, sc := range s.Scripts {
		if sc.Name == name {
			return sc, true
		}
	}
	return Script{}, false
}

// Names 返回全部脚本名（已排序）。
func (s *Set) Names() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.Scripts))
	for _, sc := range s.Scripts {
		out = append(out, sc.Name)
	}
	return out
}

type pkgJSON struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Scripts map[string]string `json:"scripts"`
}

// Load 读取项目脚本。
//
// 顺序：package.json 权威 → ngm.json 回退。同名脚本同时出现在两处是
// 配置错误（exit 3），错误里点名两处来源。**禁止静默覆盖**：静默选一边会让
// “我明明改了脚本”变成不可解释的行为。
func Load(dir string) (*Set, error) {
	out := &Set{Project: dir}

	pkg, hasPkg, err := readPackageJSON(dir)
	if err != nil {
		return nil, err
	}
	merged := map[string]string{}
	origin := map[string]Source{}
	if hasPkg {
		out.PackageName = pkg.Name
		out.PackageVersion = pkg.Version
		for name, cmd := range pkg.Scripts {
			merged[name] = cmd
			origin[name] = SourcePackageJSON
		}
	}

	ngmScripts, err := readNgmScripts(dir, out)
	if err != nil {
		return nil, err
	}
	for name, cmd := range ngmScripts {
		if prev, dup := merged[name]; dup {
			return nil, errs.New(errs.CodeConfigInvalid,
				"script "+quote(name)+" is defined twice: package.json.scripts and ngm.json.scripts",
				"keep it in exactly one place (package.json is authoritative); "+
					"the two definitions are "+quote(prev)+" and "+quote(cmd)+" — "+
					"ngm refuses to pick one silently")
		}
		merged[name] = cmd
		origin[name] = SourceNgmJSON
	}

	if len(merged) == 0 {
		return nil, errs.New(errs.CodeConfigInvalid,
			"no scripts defined in "+dir,
			"add a `scripts` object to package.json (preferred) or ngm.json, "+
				"e.g. {\"scripts\":{\"build\":\"tsc -p .\"}}")
	}

	for name := range merged {
		out.Scripts = append(out.Scripts, Script{
			Name:    name,
			Command: merged[name],
			Source:  origin[name],
			Pre:     prePostName(merged, "pre", name),
			Post:    prePostName(merged, "post", name),
		})
	}
	sort.Slice(out.Scripts, func(i, j int) bool { return out.Scripts[i].Name < out.Scripts[j].Name })
	return out, nil
}

// prePostName 返回 npm 约定的前后置脚本名：`pre` + name / `post` + name。
//
// npm 的约定是**前缀**（`prebuild` / `postbuild`），不是后缀。用户文档里
// 也按这个真实约定书写——写成 `<name>pre` 会让 npm 用户按习惯写的脚本
// 一条都不生效，那才是最难查的分歧。
func prePostName(all map[string]string, prefix, name string) string {
	candidate := prefix + name
	if _, ok := all[candidate]; ok {
		return candidate
	}
	return ""
}

// readPackageJSON 读取 package.json；文件不存在返回 has=false（不是错误）。
func readPackageJSON(dir string) (pkgJSON, bool, error) {
	path := filepath.Join(dir, "package.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return pkgJSON{}, false, nil
		}
		return pkgJSON{}, false, errs.Wrap(errs.CodeConfigInvalid,
			"read "+path, "check that package.json is a readable regular file", err)
	}
	var p pkgJSON
	// 允许 UTF-8 BOM：Windows 上以“UTF-8 带 BOM”保存的 package.json 很常见，
	// 而 encoding/json 会把它当成非法首字节。npm 侧同样容忍它。
	raw = bytes.TrimPrefix(raw, []byte("\ufeff"))
	if err := json.Unmarshal(raw, &p); err != nil {
		return pkgJSON{}, false, errs.Wrap(errs.CodeConfigInvalid,
			"invalid package.json at "+path,
			"fix the reported JSON error; `ngm run` only needs \"name\", \"version\" and \"scripts\"", err)
	}
	return p, true, nil
}

// readNgmScripts 读取 ngm.json.scripts（文件不存在返回空表）。
//
// 用 config.ReadProjectFile 而不是自己解码：ngm.json 的未知字段、必填字段
// 与类型错误必须由**同一条**既有路径判定，否则同一个坏文件在 `ngm run` 与
// `ngm config validate` 上会给出两种结论。
func readNgmScripts(dir string, out *Set) (map[string]string, error) {
	path := filepath.Join(dir, config.FileName)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+path,
			"check that ngm.json is a readable regular file", err)
	}
	pf, err := config.ReadProjectFile(path)
	if err != nil {
		return nil, err
	}
	if out.PackageName == "" {
		out.PackageName = pf.Name
	}
	if out.PackageVersion == "" {
		out.PackageVersion = pf.Version
	}
	return pf.Scripts, nil
}

// BinDirs 返回从项目目录向上逐级找到的 `node_modules/.bin`，**近者在前**。
//
// npm 的查找习惯是沿祖先目录向上；近者优先，因为嵌套项目的本地工具应当
// 覆盖外层同名工具。找不到时返回空表——这不是错误：项目可能还没装任何
// 带 bin 的依赖，脚本也可能只用全局 PATH 上的命令。
func BinDirs(dir string) []string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	var out []string
	for cur := abs; ; {
		bin := filepath.Join(cur, "node_modules", ".bin")
		if st, err := os.Stat(bin); err == nil && st.IsDir() {
			out = append(out, bin)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return out
}

func quote(s string) string { return "\"" + s + "\"" }
