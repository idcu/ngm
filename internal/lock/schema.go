// Package lock 实现 ngm.lock 的 schema、确定性序列化与读写。
//
// 规范唯一事实源：architecture/locking.md。
//
// 格式契约（一旦发布不可随意变更）：
//
//	{
//	  "version": 1,
//	  "lockfileVersion": "1.0.0",
//	  "dependencies": [
//	    {
//	      "name": "github:org/utils",
//	      "ref": "v1.2.3",
//	      "refType": "tag",
//	      "commit": "abc123...",
//	      "archiveDigest": "sha256:...",
//	      "resolvedAt": "2026-09-29T10:00:00Z",
//	      "vendorPath": "github.com/org/utils"
//	    }
//	  ]
//	}
//
// 可复现性定义（locking.md §可复现性的定义）：
//
//	确定性字段  结构、字段顺序、commit、archiveDigest、vendorPath 等
//	非确定性字段 resolvedAt（解析时刻）
//	序列化规范  UTF-8、2 空格缩进、字段顺序固定、末尾换行
package lock

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/digest"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/resolve"
)

const (
	// SchemaVersion 是 lock schema 版本（locking.md 的 `version` 字段）。
	SchemaVersion = 1

	// FileVersion 是 lockfileVersion，控制兼容策略。
	//
	// 规则（locking.md §lockfileVersion 演进）：
	//   - 同 MAJOR 内只允许新增可选字段
	//   - MAJOR 变更需**同时**提供迁移命令（`ngm lock migrate`）——至今未发生 MAJOR 变更，
	//     因此该命令**不存在**，错误提示里也不得引用它（v0.5 复核修正：提示曾引用一个
	//     不存在的命令，用户照做只会得到"未知命令"）
	//   - archiveDigest 的清单规范版本与之绑定
	FileVersion = "1.0.0"

	// FileName 是 lock 文件名（项目根目录，必须提交到 Git）。
	FileName = "ngm.lock"
)

// File 是 ngm.lock 的顶层结构。
//
// 字段顺序即序列化顺序（encoding/json 按 struct 声明顺序输出），
// 因此**调整字段顺序等同于破坏格式契约**。
type File struct {
	Version         int          `json:"version"`
	LockfileVersion string       `json:"lockfileVersion"`
	Dependencies    []Dependency `json:"dependencies"`
}

// Dependency 是 lock 中的一个依赖条目。
//
// 字段顺序固定为：name / ref / refType / commit / archiveDigest / resolvedAt / vendorPath。
// 注意 resolvedAt **在依赖条目内**——lock 顶层不含时间戳（locking.md §字段纪律）。
type Dependency struct {
	// Name 是依赖标识（slug 形式，如 github:org/repo）。来源：ngm.json。
	Name string `json:"name"`
	// Ref 是人类的 ref 声明（tag 名 / 分支名 / commit hash）。来源：ngm.json。
	Ref string `json:"ref"`
	// RefType 是声明意图（tag / branch / commit）。来源：ngm.json。
	RefType string `json:"refType"`
	// Commit 是解析结果，不可变锚点。
	Commit string `json:"commit"`
	// ArchiveDigest 是规范化内容清单哈希（ADR-008），形如 `sha256:<hex>`。
	ArchiveDigest string `json:"archiveDigest"`
	// ResolvedAt 是解析时间，**唯一非确定性字段**，仅用于审计。
	//
	// 明确语义（locking.md §resolvedAt 的语义）：
	//   - 记录"你什么时候信了它"
	//   - 不参与 minimumReleaseAge（后者的时间源是 commit 的 committer date）
	//   - 不是 commit 的 author date，也不是上游发布时间
	ResolvedAt string `json:"resolvedAt"`
	// VendorPath 是 vendor 层落地路径（canonical 形式，不含 .git），
	// 如 `github.com/org/utils`。来源：归一化规则。
	VendorPath string `json:"vendorPath"`
	// SubPath 是 monorepo 子路径（可选；空表示整仓）。
	//
	// 注意：该字段在 locking.md 的 v1.0.0 示例中未出现。按 §lockfileVersion 演进
	// 的"同 MAJOR 内只允许新增可选字段"规则，增加可选字段是兼容的；
	// omitempty 保证不含子路径时的字节与 v1.0.0 示例一致。
	SubPath string `json:"subPath,omitempty"`
	// RequiredBy 是**直接引用者**的节点 Key（`name` 或 `name#subPath`；
	// 项目本身直接声明时为 `(root)`，见 RootRequirer）。
	//
	// v0.73（[ADR-028](../docs/adr/adr-028-dependency-graph-in-lock.md)）：图写进 lock，
	// 让 `ngm why` 不必重新解析（离线也可答）、并让 SBOM 有真正的依赖关系段。
	//
	// 三条纪律：
	//  1. **只存直接边**——全路径是指数的（ADR-024），直接边可重构全部路径；
	//  2. 字段**恒在**（没有 omitempty）：`[]` 表示"来源未知"（旧版 lock 或手改），
	//     空数组与 nil 解码后同义，消费端一律按"图未知"处理；
	//  3. 序列化时排序 + 去重（见 Marshal），同一份解析结果 ⇒ 同一份字节。
	RequiredBy []string `json:"requiredBy"`
}

// RootRequirer 是 RequiredBy 里代表"项目本身直接声明"的标记。
//
// 刻意在本包**重新声明**而不是 import resolve：lock 是格式层，不依赖解析层
// （与 lockKey 同一条理由）。它与 `resolve.RootMarker` 必须**同值**——
// 这条一致性由 `TestRootRequirerMatchesResolve` 机械钉住。
const RootRequirer = "(root)"

// NewFile 创建一个空的 lock（含正确的版本字段）。
func NewFile() *File {
	return &File{
		Version:         SchemaVersion,
		LockfileVersion: FileVersion,
		Dependencies:    []Dependency{},
	}
}

// FormatResolvedAt 把时间格式化为 lock 使用的 RFC3339 UTC 形式。
//
// 统一到 UTC 且秒级精度，避免时区/亚秒差异让"同一时刻"写出不同字节。
func FormatResolvedAt(t time.Time) string {
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// Validate 校验 lock 的结构与版本兼容性。
func (f *File) Validate() error {
	if f == nil {
		return errs.New(errs.CodeConfigInvalid, "lock file is nil", "")
	}
	if f.Version != SchemaVersion {
		return errs.New(
			errs.CodeConfigInvalid,
			"unsupported lock `version`: "+strconv.Itoa(f.Version),
			"this build supports version "+strconv.Itoa(SchemaVersion)+
				"; a newer ngm wrote this file — regenerate the lock")
	}
	if f.LockfileVersion == "" {
		return errs.New(errs.CodeConfigInvalid, "lock is missing `lockfileVersion`",
			"delete ngm.lock and re-run `ngm install` to regenerate it")
	}
	if majorVersion(f.LockfileVersion) != majorVersion(FileVersion) {
		return errs.New(
			errs.CodeConfigInvalid,
			"incompatible lockfileVersion "+f.LockfileVersion+
				" (this build writes "+FileVersion+")",
			"delete ngm.lock and re-run `ngm install` to regenerate it — "+
				"no MAJOR bump has happened yet, so no migration tool exists")
	}
	for i, d := range f.Dependencies {
		if err := d.validate(); err != nil {
			// v0.81：把内层**点名字段**的消息带出来。
			//
			// 只写 "dependencies[N] invalid" 时，用户拿到的是一条**正确**的建议
			// （hint 沿 cause 链继承下来了）却不知道是哪个字段不对——
			// 于是只能盲试。"建议对、但不知道修哪儿"与"没有建议"相差无几。
			// 取**裸 message** 而不是 err.Error()：后者带 `ConfigInvalid:` 前缀，
			// 拼进来会变成"同一个码名在同一条错误上出现两次"（v0.48 修过的形态）。
			msg := err.Error()
			if e, ok := err.(*errs.NgmError); ok {
				msg = e.Message
			}
			return errs.Wrap(errs.CodeConfigInvalid,
				"lock dependencies["+strconv.Itoa(i)+"] invalid: "+msg, "", err)
		}
	}
	if err := f.validateEdges(); err != nil {
		return err
	}
	return nil
}

// validateEdges 拒绝**幽灵边**（v0.73，ADR-028）。
//
// lock 是依赖状态的唯一事实源，事实源里不许有悬空引用：requiredBy 指向一个
// 不在本 lock 里的条目，只会让 `why` 少列一条路径——而"少列一条"在溯源问题里
// 与说谎等价。缺失（空数组）则**允许**：那是"图未知"的诚实表示，由消费端降级。
func (f *File) validateEdges() error {
	known := f.Index()
	for i, d := range f.Dependencies {
		for _, from := range d.RequiredBy {
			if from == RootRequirer {
				continue
			}
			if _, ok := known[from]; ok {
				continue
			}
			return errs.Wrap(errs.CodeConfigInvalid,
				"lock dependencies["+strconv.Itoa(i)+"] invalid", "",
				errs.New(errs.CodeConfigInvalid,
					"`requiredBy` references "+from+", which is not in this lock (a ghost edge)",
					"delete ngm.lock and re-run `ngm install` to regenerate the graph"))
		}
	}
	return nil
}

// HasGraph 报告这份 lock 是否带有**完整**的依赖图（ADR-028）。
//
// 判据是**每个**条目都至少有一个引用者：图的边只有完整才可用——少一条边就会让
// `why` 少一条路径。缺失（旧版 lock 或手改）⇒ false，消费端据此优雅降级，
// 并且**必须**在输出里说明自己用的是哪条路径（绝不静默回退）。
func (f *File) HasGraph() bool {
	if f == nil {
		return false
	}
	for i := range f.Dependencies {
		if len(f.Dependencies[i].RequiredBy) == 0 {
			return false
		}
	}
	return true
}

func (d *Dependency) validate() error {
	if d.Name == "" {
		return errs.New(errs.CodeConfigInvalid, "missing `name`", "")
	}
	if _, err := resolve.ParseSlug(d.Name); err != nil {
		return err
	}
	if d.Ref == "" {
		return errs.New(errs.CodeConfigInvalid, "missing `ref` for "+d.Name, "")
	}
	if !resolve.RefType(d.RefType).IsValid() {
		return errs.New(errs.CodeConfigInvalid,
			"invalid `refType` "+d.RefType+" for "+d.Name, "")
	}
	if !isFullCommit(d.Commit) {
		return errs.New(errs.CodeConfigInvalid,
			"`commit` for "+d.Name+" must be a full 40-char hex hash (got "+d.Commit+")",
			"a partial hash is ambiguous; re-run `ngm install`")
	}
	// 只校验算法前缀，**不校验长度/字符集**——见下方 isArchiveDigest 的说明。
	if !strings.HasPrefix(d.ArchiveDigest, digest.Algorithm+":") {
		return errs.New(errs.CodeConfigInvalid,
			"`archiveDigest` for "+d.Name+" must be "+digest.Algorithm+":<hex> (got "+d.ArchiveDigest+")",
			"re-run `ngm install` to regenerate the digest")
	}
	if d.VendorPath == "" {
		return errs.New(errs.CodeConfigInvalid, "missing `vendorPath` for "+d.Name, "")
	}
	return nil
}

// majorVersion 取语义化版本的 MAJOR 段；无法解析时返回 -1（调用方按不兼容处理）。
func majorVersion(v string) int {
	if i := strings.IndexByte(v, '.'); i > 0 {
		v = v[:i]
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}

// isFullCommit 报告 s 是否为 40 位十六进制（小写或大写）。
//
// lock 中的 commit 必须是完整 hash：短 hash 在不同时间可能指向不同对象
// （对象库里新增同前缀对象时），那会破坏"不可变锚点"的语义。
// IsArchiveDigest 校验 `sha256:<64 hex>` 的**完整形态**（导出给消费方）。
//
// 为什么 `Validate` 自己只校验前缀（v0.81 的决定，勿"顺手收紧"）：
// lock 里一个**形状不对**的 digest 有两种成因——写坏了，或**被篡改**。
// 而"被篡改"必须是**完整性失败（exit 2）**，不是配置错误（exit 3）：
// `TestM3Acceptance/install_rejects_a_tampered_lock_digest` 钉的就是这条
// （它把 digest 改成一个 128 位的串，要求 install 退 2 并说明 mismatch）。
// 若在读取时按形状拒绝，同一件事就会变成 exit 3——**把一次完整性事件
// 降级成配置问题**，那比放行更糟。
//
// 于是形状校验放在**消费方**：真正要把 digest 写进产物的地方
// （`internal/sbom.FromLock`，SBOM 与 attestation 共用）必须拒绝空哈希，
// 因为一份 `content: ""` 的 SBOM 是**结构合法、内容为空**的假证据。
func IsArchiveDigest(s string) bool {
	rest := strings.TrimPrefix(s, digest.Algorithm+":")
	if len(rest) != 64 || len(s) != len(digest.Algorithm)+1+64 {
		return false
	}
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

func isFullCommit(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

// Sort 按 name（字节序）稳定排序依赖，保证输出确定性。
//
// 排序键为 name + subPath：同一仓库的 monorepo 子路径是不同条目。
func (f *File) Sort() {
	sort.SliceStable(f.Dependencies, func(i, j int) bool {
		if f.Dependencies[i].Name != f.Dependencies[j].Name {
			return f.Dependencies[i].Name < f.Dependencies[j].Name
		}
		return f.Dependencies[i].SubPath < f.Dependencies[j].SubPath
	})
}

// Find 按 name + subPath 查找条目。
func (f *File) Find(name, subPath string) (*Dependency, bool) {
	if f == nil {
		return nil, false
	}
	for i := range f.Dependencies {
		d := &f.Dependencies[i]
		if d.Name == name && d.SubPath == subPath {
			return d, true
		}
	}
	return nil, false
}

// Index 返回 name+subPath → 条目的索引。
func (f *File) Index() map[string]*Dependency {
	out := make(map[string]*Dependency, len(f.Dependencies))
	if f == nil {
		return out
	}
	for i := range f.Dependencies {
		d := &f.Dependencies[i]
		out[lockKey(d.Name, d.SubPath)] = d
	}
	return out
}

// Key 返回该条目的身份（name 或 name#subPath），与 resolve 的节点 Key 同口径。
//
// v0.73：lock 里的边（requiredBy）引用的就是这个身份，因此它必须有**唯一定义处**。
func (d *Dependency) Key() string { return lockKey(d.Name, d.SubPath) }

// lockKey 与 resolve.DepSpec.Key 保持同一口径（name[#subPath]）。
//
// 刻意不 import resolve 的 Key 方法：lock 是格式层，不应依赖解析层的行为。
// 两处的一致性由 lock 测试与 install 的集成测试共同保证。
func lockKey(name, subPath string) string {
	if subPath == "" {
		return name
	}
	return name + "#" + subPath
}

// Marshal 序列化为规范字节流。
//
// 规范：2 空格缩进、LF 行尾、文件末尾单个换行、字段顺序由 struct 声明固定。
// Marshal **不排序**——调用方应先 Sort（或由 Install 流程保证顺序），
// 以便"顺序变化"成为显式行为而非隐式副作用。
func (f *File) Marshal() ([]byte, error) {
	if f == nil {
		return nil, errs.New(errs.CodeConfigInvalid, "cannot marshal nil lock", "")
	}
	out := *f
	if out.Dependencies == nil {
		out.Dependencies = []Dependency{}
	} else {
		// v0.73：requiredBy 在**序列化这一刻**归一（排序 + 去重），
		// 于是"同一份解析结果 ⇒ 同一份字节"不依赖调用方是否记得排序
		// （调用方甚至可以直接把解析器给的顺序写进来）。
		// 顺带把 nil 变成 `[]`：输出里不许出现 `"requiredBy": null`——
		// 那会让"空"有两种写法，而格式层只有一种"来源未知"。
		norm := make([]Dependency, len(out.Dependencies))
		copy(norm, out.Dependencies)
		for i := range norm {
			norm[i].RequiredBy = normalizeRequiredBy(norm[i].RequiredBy)
		}
		out.Dependencies = norm
	}
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "marshal ngm.lock", "", err)
	}
	return append(data, '\n'), nil
}

// normalizeRequiredBy 排序 + 去重，并保证返回非 nil（空 → `[]`）。
func normalizeRequiredBy(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
