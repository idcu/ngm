package adopt

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/idcu/ngm/internal/core/errs"
)

// 变化的三种形态。
//
// 刻意只有三种：一份证明描述的是"路径 → 内容摘要"，而摘要不含时间、不含下载来源，
// 所以两次扫描之间的差异只可能是"内容变了""少了""多了"。
// 版本号变化**不单列**——它必然改变 package.json 的字节，因此落在 `changed` 里；
// 把它单列会让同一个事实出现两条记录。
const (
	// ChangeChanged 表示同一路径的摘要变了。
	ChangeChanged = "changed"
	// ChangeMissing 表示旧证明里有、现在没了。
	ChangeMissing = "missing"
	// ChangeExtra 表示现在有、旧证明里没有。
	ChangeExtra = "extra"
)

// Change 是一条差异。
type Change struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Kind string `json:"kind"`
	// Want 是旧证明里的摘要（`extra` 时为空）。
	Want string `json:"want,omitempty"`
	// Got 是当前摘要（`missing` 时为空）。
	Got string `json:"got,omitempty"`
}

// Compare 比较一份**已存下的**证明与刚扫出来的结果。
//
// 按 Path 索引而不是按 Name：同一个仓库可以在两处出现不同的包名
// （`node_modules/a/node_modules/b` 与顶层 `node_modules/b` 可能是不同版本），
// 而 Path 是唯一标识"这一份字节住在哪"。按 Name 比较会把它们合并，
// 于是"嵌套依赖被换了"这种变化会被静默吞掉。
//
// 返回值按 Path 排序：两次运行必须给出同一个顺序，否则 diff 是噪声。
func Compare(old, cur *Proof) []Change {
	oldByPath := make(map[string]Package, len(old.Packages))
	for _, p := range old.Packages {
		oldByPath[p.Path] = p
	}
	curByPath := make(map[string]Package, len(cur.Packages))
	for _, p := range cur.Packages {
		curByPath[p.Path] = p
	}

	out := make([]Change, 0, len(old.Packages)+len(cur.Packages))
	for path, o := range oldByPath {
		c, ok := curByPath[path]
		if !ok {
			out = append(out, Change{Name: o.Name, Path: path, Kind: ChangeMissing, Want: o.Digest})
			continue
		}
		if c.Digest != o.Digest {
			out = append(out, Change{Name: c.Name, Path: path, Kind: ChangeChanged, Want: o.Digest, Got: c.Digest})
		}
	}
	for path, c := range curByPath {
		if _, ok := oldByPath[path]; !ok {
			out = append(out, Change{Name: c.Name, Path: path, Kind: ChangeExtra, Got: c.Digest})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

// ParseProof 解析一份存下来的证明。
//
// 存在的意义是**拒绝**比接受更重要：`--check` 后面很容易写错文件名
// （`ngm.lock`、`package-lock.json`、上一版的 proof）。一个宽松的解析器
// 会把它们读成"零个包"，于是比较结果是"全都是 extra"——那看起来像一次
// 灾难，而真实原因是"你指错了文件"。
func ParseProof(data []byte) (*Proof, error) {
	var p Proof
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"that file is not a proof (it does not parse as JSON)",
			"point --check at the file `ngm adopt` wrote (default: "+FileName+")", err)
	}
	// 判别"这是一份证明"要靠**证明独有**的字段，不能只看 `version`：
	// `ngm.lock` 的顶层也是 `version: 1`，只是没有 `tool` 与 `packages`。
	// 若只比 version，`--check=ngm.lock` 会被读成"零个包的证明"，
	// 于是每个已装包都成了 extra——一次看起来像灾难、实际只是指错文件的结果。
	if p.Tool.Name != "ngm" || p.Packages == nil {
		return nil, errs.New(errs.CodeConfigInvalid,
			"that file is not a proof: it has no `tool` object naming ngm and no `packages` array",
			"point --check at the file `ngm adopt` wrote (default: "+FileName+")")
	}
	if p.Version != ProofVersion {
		return nil, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("that proof has schema version %d, this build writes %d", p.Version, ProofVersion),
			"re-create it with `ngm adopt` using this version of ngm")
	}
	if len(p.Packages) == 0 {
		// `ngm adopt` 从不写空证明（空 node_modules 会直接失败），
		// 所以"零个包"只可能来自一份被手工改过或截断的文件。
		return nil, errs.New(errs.CodeConfigInvalid,
			"that proof lists no packages",
			"re-create it with `ngm adopt`; an empty proof would make every installed package look new")
	}
	return &p, nil
}
