package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/resolve"
)

// ReadProjectFile 读取并校验单个 ngm.json（不做三级合并）。
//
// 与 Load 的区别：Load 返回合并后的 Resolved；本函数用于需要"原始项目声明"的
// 写入型操作（ngm add / remove / update），它们必须保留文件里未涉及的字段。
func ReadProjectFile(path string) (*ProjectFile, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errs.Wrap(errs.CodeConfigInvalid,
				"ngm.json not found at "+path,
				"run `ngm init <name>` first to create a project", err)
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid, "open "+path, "", err)
	}
	defer f.Close()
	return decodeProject(f, path)
}

// WriteProjectFile 以确定格式写回 ngm.json。
//
// 格式纪律（与 lock / mappings 一致，见 development/README.md 全局注意事项）：
//   - 字段顺序固定（由 struct 定义顺序决定）
//   - 2 空格缩进
//   - LF 行尾 + 文件末尾单个换行
//   - dependencies 为空时输出 `[]`（而非 null）
//
// 写入是原子的：先写同目录临时文件，再 rename，避免中断时留下半个文件。
func WriteProjectFile(path string, p *ProjectFile) error {
	out, err := MarshalProjectFile(p)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, out)
}

// MarshalProjectFile 返回确定格式的 ngm.json 字节。
//
// 供 `ngm add --dry-run`、golden 测试以及任何需要"先算字节再落盘"的场景使用。
func MarshalProjectFile(p *ProjectFile) ([]byte, error) {
	if p == nil {
		return nil, errs.New(errs.CodeConfigInvalid, "cannot marshal nil project file", "")
	}
	if p.Dependencies == nil {
		p.Dependencies = []Dependency{}
	}
	if p.SchemaVersion == 0 {
		p.SchemaVersion = SchemaVersion
	}
	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "marshal ngm.json", "", err)
	}
	return append(out, '\n'), nil
}

// writeFileAtomic 原子写文件（同目录临时文件 + rename）。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".ngm-*.tmp")
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create temp file in "+dir, "check directory permissions", err)
	}
	tmpName := tmp.Name()
	defer func() {
		// 成功路径已 rename；失败路径清理临时文件
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "write temp file", "", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return errs.Wrap(errs.CodeConfigInvalid, "sync temp file", "", err)
	}
	if err := tmp.Close(); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "close temp file", "", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "replace "+path, "close any editor holding the file", err)
	}
	return nil
}

// UpsertDependency 添加或更新一个依赖条目。
//
// 匹配规则：对既有条目的 name 与新增条目的 name 都做归一化（resolve.ParseSlug），
// 归一化后指向同一仓库且 path 相同者视为同一条目并被覆盖。
// 这避免了 `github:org/repo` 与 `github.com:org/repo` 被视为两个依赖。
//
// 返回 (added, error)：added=true 表示新增（而非覆盖）。
func (p *ProjectFile) UpsertDependency(dep Dependency) (bool, error) {
	if p == nil {
		return false, errs.New(errs.CodeConfigInvalid, "nil project file", "")
	}
	if err := dep.Validate(); err != nil {
		return false, errs.Wrap(errs.CodeConfigInvalid, "invalid dependency", "", err)
	}
	newCanon, err := resolve.ParseSlug(dep.Name)
	if err != nil {
		return false, err
	}

	for i := range p.Dependencies {
		existing, perr := resolve.ParseSlug(p.Dependencies[i].Name)
		if perr != nil {
			// 既有条目非法：不参与匹配（用户可自行修）
			continue
		}
		if existing.Equal(newCanon) && normalizeSubPath(p.Dependencies[i].Path) == normalizeSubPath(dep.Path) {
			p.Dependencies[i] = dep
			p.sortDependencies()
			return false, nil
		}
	}

	p.Dependencies = append(p.Dependencies, dep)
	p.sortDependencies()
	return true, nil
}

// RemoveDependency 按 name（归一化后）移除依赖；返回是否存在并删除。
func (p *ProjectFile) RemoveDependency(name string) (bool, error) {
	target, err := resolve.ParseSlug(name)
	if err != nil {
		return false, err
	}
	out := p.Dependencies[:0]
	removed := false
	for _, d := range p.Dependencies {
		c, cerr := resolve.ParseSlug(d.Name)
		if cerr == nil && c.Equal(target) {
			removed = true
			continue
		}
		out = append(out, d)
	}
	p.Dependencies = out
	if removed {
		p.sortDependencies()
	}
	return removed, nil
}

// sortDependencies 按 name 的 UTF-8 字节序稳定排序（确定性输出）。
func (p *ProjectFile) sortDependencies() {
	sort.SliceStable(p.Dependencies, func(i, j int) bool {
		if p.Dependencies[i].Name != p.Dependencies[j].Name {
			return p.Dependencies[i].Name < p.Dependencies[j].Name
		}
		return p.Dependencies[i].Path < p.Dependencies[j].Path
	})
}

// normalizeSubPath 让子路径比较跨平台一致（`a/b` 与 `a\b` 视为同一路径）。
func normalizeSubPath(p string) string {
	return strings.Trim(filepath.ToSlash(p), "/")
}
