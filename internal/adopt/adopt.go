// Package adopt 为一个**不使用 ngm 安装**的项目产出内容证明。
//
// 为什么要有这个包（v0.105，[ADR-040]）：ngm 的全部资产是"内容 ↔ commit"的
// 可证明性，而它此前只能对自己装出来的树说话。真实项目的依赖是 pnpm/npm 装的，
// 要求"先换安装器再谈证明"是采用率的唯一硬门槛——本包把门槛降成一条命令：
// 读现有的 `node_modules/`，按 **ngm 自己的清单格式**（ADR-008）算出每个包的
// 内容摘要，并把 lock 文件里能追溯到的 Git 来源一并记下来。
//
// 它**不**做三件事，且每一件都在输出里明说：
//
//   - 不接管安装（不写 node_modules，不动 lock）
//   - 不判"安全"（内容摘要只回答"这份内容的字节是什么"，不回答"它有害吗"）
//   - 不假装覆盖完整：没 lock、没来源、跳过的东西都写进 Notes 与 Coverage
//
// 摘要格式与 `ngm verify` 的 archiveDigest **逐字节同构**（同一函数算出来的），
// 因此 `ngm adopt` 的产物与 ngm 自己装出来的条目可以直接比较。
package adopt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
)

// ProofVersion 是证明文件的 schema 版本。
const ProofVersion = 1

// FileName 是默认输出文件名。
//
// 刻意**不叫** `ngm.lock`：那不是锁，它没有钉住任何东西，也不该被 `install` 读到。
// 叫 `ngm.proof.json` 的名称把它的身份写在脸上（与 `ngm.mappings.json` 同一族）。
const FileName = "ngm.proof.json"

// 来源种类。
const (
	// KindGit：lock 里记录了 Git URL 与 commit——这一条是可追溯的。
	KindGit = "git"
	// KindRegistry：来自 registry，只有 integrity 摘要（tgz 的哈希）。
	KindRegistry = "registry"
	// KindUnknown：lock 里没有这一条，或 lock 不存在。
	KindUnknown = "unknown"
)

// Provenance 是一个依赖的来源描述。
type Provenance struct {
	Kind string `json:"kind"`
	// URL / Commit 只在 KindGit 时出现。
	URL    string `json:"url,omitempty"`
	Commit string `json:"commit,omitempty"`
	// Ref 是 Git 来源的 fragment 不是 40 位 hash 时记下的声明（如 `v1.2.0`）。
	// 它**不可追溯**，因此单独成字段而不是塞进 Commit。
	Ref string `json:"ref,omitempty"`
	// Resolved / Integrity 是 registry 来源的原样记录（不解释、不重算）。
	Resolved  string `json:"resolved,omitempty"`
	Integrity string `json:"integrity,omitempty"`
}

// Package 是一个已安装依赖的内容证据。
type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Path 是项目根相对路径（POSIX 分隔），如 `node_modules/foo`。
	Path string `json:"path"`
	// Digest 与 `ngm verify` 的 archiveDigest 同构：sha256 + 规范化清单哈希。
	Digest string `json:"digest"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
	// Nested 是该包自带 node_modules 的子包数（它们各自成条目）。
	Nested    int        `json:"nested,omitempty"`
	Provenance Provenance `json:"provenance"`
}

// Coverage 是"这份证明覆盖了什么"的读数。
//
// 它是本文件最重要的字段：**没有它，一份只有 3 个包有来源的证明看起来与
// 全部都有来源的证明一模一样。**
type Coverage struct {
	Packages int `json:"packages"`
	// WithCommit 是来源可追溯到某个 commit 的包数。
	WithCommit int `json:"withCommit"`
	// WithIntegrity 是只有 registry integrity 的包数。
	WithIntegrity int `json:"withIntegrity"`
	// Unknown 是 lock 里查不到的包数（没有 lock 时等于 Packages）。
	Unknown int `json:"unknown"`
	// Skipped 是被跳过的文件系统条目数（socket / device / 读取失败）。
	Skipped int `json:"skipped,omitempty"`
}

// Source 描述证据的来源。
type Source struct {
	// Manager 是本包**识别到**的安装器：npm / pnpm / yarn / unknown。
	Manager string `json:"manager"`
	// Lockfile 是读到的 lock 路径（相对项目根）；空表示没有。
	Lockfile string `json:"lockfile,omitempty"`
	// LockfileDigest 是那份 lock 的字节哈希——证据要能指向它当时的样子。
	LockfileDigest string `json:"lockfileDigest,omitempty"`
}

// Tool 记录产出这份证据的 ngm 版本。
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Proof 是 `ngm adopt` 的完整产物。
type Proof struct {
	Version  int      `json:"version"`
	Tool     Tool     `json:"tool"`
	Source   Source   `json:"source"`
	Coverage Coverage `json:"coverage"`
	Packages []Package `json:"packages"`
	// Notes 是**必须说出来**的边界：跳过了什么、为什么某类来源查不到。
	Notes []string `json:"notes,omitempty"`
}

// Options 控制一次扫描。
type Options struct {
	// Dir 是项目根（含 node_modules 的目录）。
	Dir string
	// ToolVersion 由调用方注入（本包不读版本）。
	ToolVersion string
}

// Run 扫描 node_modules 并产出证明。
func Run(opts Options) (*Proof, error) {
	root := opts.Dir
	if root == "" {
		root = "."
	}
	nmRoot := filepath.Join(root, "node_modules")
	st, err := os.Lstat(nmRoot)
	if err != nil || !st.IsDir() {
		// 消息里用**原样路径**（不加 ToSlash）：路径是给人读的，而外部判据
		// 要把它与 `--dir` 的取值逐字对上才能归一化（v0.51／v0.65 那一族）。
		// ToSlash 只用于 `packages[].path`——那是数据，不是消息。
		return nil, errs.New(errs.CodeConfigInvalid,
			"no node_modules directory at "+nmRoot,
			"run this in a project that has its dependencies installed "+
				"(ngm adopt describes what is on disk; it never installs)")
	}

	lock, lockNotes, lockSource := readLock(root)

	sc := &scanner{root: root, lock: lock}
	if err := sc.discover(nmRoot); err != nil {
		return nil, err
	}
	if len(sc.pkgs) == 0 {
		return nil, errs.New(errs.CodeConfigInvalid,
			"node_modules exists but contains no packages at "+nmRoot,
			"the directory may be empty or only contain `.bin`; re-run your installer first")
	}

	sort.Slice(sc.pkgs, func(i, j int) bool {
		if sc.pkgs[i].Path != sc.pkgs[j].Path {
			return sc.pkgs[i].Path < sc.pkgs[j].Path
		}
		return sc.pkgs[i].Name < sc.pkgs[j].Name
	})

	proof := &Proof{
		Version:  ProofVersion,
		Tool:     Tool{Name: "ngm", Version: opts.ToolVersion},
		Source:   lockSource,
		Packages: sc.pkgs,
	}
	proof.Coverage.Packages = len(sc.pkgs)
	proof.Coverage.Skipped = sc.skipped
	for i := range sc.pkgs {
		switch sc.pkgs[i].Provenance.Kind {
		case KindGit:
			if sc.pkgs[i].Provenance.Commit != "" {
				proof.Coverage.WithCommit++
			} else {
				proof.Coverage.Unknown++
			}
		case KindRegistry:
			proof.Coverage.WithIntegrity++
		default:
			proof.Coverage.Unknown++
		}
	}
	proof.Notes = append(proof.Notes, lockNotes...)
	if sc.skipped > 0 {
		proof.Notes = append(proof.Notes, fmt.Sprintf(
			"%d filesystem entr(ies) were skipped (not a regular file or symlink, or unreadable)",
			sc.skipped))
	}
	proof.Notes = append(proof.Notes, ""+
		"a digest here covers the bytes that are installed, not who produced them: "+
		"it cannot tell you whether the code is malicious, only whether it changed")
	return proof, nil
}

// Marshal 以确定性格式编码（固定字段序、两空格缩进、LF、尾随换行）。
func (p *Proof) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "encode the proof", "report this as a bug", err)
	}
	return append(data, '\n'), nil
}

// ---------------------------------------------------------------------------
// 扫描
// ---------------------------------------------------------------------------

type scanner struct {
	root    string
	lock    map[string]lockEntry // key = 项目根相对路径（POSIX）
	pkgs    []Package
	skipped int
	seen    map[string]bool // 已展开的 node_modules 绝对路径（防重复）
}

// discover 递归展开一个 node_modules 目录下的包。
//
// 为什么递归：npm 的非提升依赖住在 `node_modules/a/node_modules/b`。
// 只看第一层会让"证明"漏掉它们，而漏掉的形态与"没有这些包"完全一样。
func (s *scanner) discover(nmDir string) error {
	abs, err := filepath.Abs(nmDir)
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "resolve "+nmDir,
			"pass a --dir that resolves on this machine (a path with a drive letter, "+
				"or one relative to the current directory)", err)
	}
	if s.seen == nil {
		s.seen = map[string]bool{}
	}
	if s.seen[abs] {
		return nil
	}
	s.seen[abs] = true

	entries, err := os.ReadDir(nmDir)
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "read "+filepath.ToSlash(nmDir),
			"check directory permissions", err)
	}
	for _, e := range entries {
		name := e.Name()
		// `.bin` / `.cache` / `.modules.yaml` / `.package-lock.json` 都是安装器的
		// 内部条目，不是包。点前缀统一跳过。
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(nmDir, name)
		if name[0] == '@' {
			// scope 目录：里面的每一项才是包。
			subs, serr := os.ReadDir(full)
			if serr != nil {
				s.skipped++
				continue
			}
			for _, sub := range subs {
				if strings.HasPrefix(sub.Name(), ".") {
					continue
				}
				if err := s.addPackage(filepath.Join(full, sub.Name())); err != nil {
					return err
				}
			}
			continue
		}
		if err := s.addPackage(full); err != nil {
			return err
		}
	}
	return nil
}

func (s *scanner) addPackage(dir string) error {
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		// 符号链接（如 workspace 链接）不跟随：跟随会把"链接到项目自己的源码"
		// 算成一份依赖，而那份内容不在 lock 里也不在 registry 上。
		s.skipped++
		return nil
	}

	records := make([]digest.Record, 0, 64)
	var bytes int64
	walkErr := filepath.WalkDir(dir, func(p string, d os.DirEntry, werr error) error {
		if werr != nil {
			s.skipped++
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// 嵌套的 node_modules 单独展开，不并入本包的清单：
			// 否则父包的 digest 会随后代的内容变化而变化，而那不是它的字节。
			if p != dir && d.Name() == "node_modules" {
				if err := s.discover(p); err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			s.skipped++
			return nil
		}
		rel = filepath.ToSlash(rel)

		info, ierr := d.Info()
		if ierr != nil {
			s.skipped++
			return nil
		}
		switch {
		case info.Mode().IsRegular():
			sum, size, herr := hashFile(p)
			if herr != nil {
				s.skipped++
				return nil
			}
			mode := digest.ModeRegular
			if info.Mode().Perm()&0o111 != 0 {
				mode = digest.ModeExecutable
			}
			records = append(records, digest.Record{Path: rel, Mode: mode, BlobSHA256: sum})
			bytes += size
		case info.Mode()&os.ModeSymlink != 0:
			target, terr := os.Readlink(p)
			if terr != nil {
				s.skipped++
				return nil
			}
			records = append(records, digest.Record{
				Path: rel, Mode: digest.ModeSymlink,
				BlobSHA256: digest.HashBytes([]byte(target)),
			})
		default:
			// socket / device / fifo：没有"字节"可证明，如实计入 skipped。
			s.skipped++
		}
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	relDir, rerr := filepath.Rel(s.root, dir)
	if rerr != nil {
		relDir = dir
	}
	relDir = filepath.ToSlash(relDir)

	p := Package{
		Name:     readPackageName(dir, filepath.Base(dir)),
		Version:  readPackageVersion(dir),
		Path:     relDir,
		Digest:   digest.Digest(digest.BuildManifest(records)),
		Files:    len(records),
		Bytes:    bytes,
		Provenance: s.provenanceFor(relDir),
	}
	s.pkgs = append(s.pkgs, p)
	return nil
}

func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// ---------------------------------------------------------------------------
// 来源（lock 文件）
// ---------------------------------------------------------------------------

type lockEntry struct {
	Version   string
	Resolved  string
	Integrity string
}

// readLock 识别安装器并读取**能读懂的那一种** lock。
//
// 只支持 npm 的 `package-lock.json`（JSON，标准库即可）。pnpm 的是 YAML，
// 而本项目不引入第三方依赖（ADR-003）：读不了就**说出读不了**，
// 绝不退化成"假装没有来源"。
func readLock(root string) (map[string]lockEntry, []string, Source) {
	var notes []string
	src := Source{Manager: "unknown"}

	candidates := []struct{ file, manager string }{
		{"package-lock.json", "npm"},
		{"npm-shrinkwrap.json", "npm"},
	}
	for _, c := range candidates {
		path := filepath.Join(root, c.file)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entries, perr := parseNPMLock(raw)
		src.Manager = c.manager
		src.Lockfile = c.file
		src.LockfileDigest = "sha256:" + digest.HashBytes(raw)
		if perr != nil {
			notes = append(notes, fmt.Sprintf(
				"%s was found but could not be parsed (%v); provenance is unknown for every package",
				c.file, perr))
			return nil, notes, src
		}
		return entries, notes, src
	}

	for _, c := range []struct{ file, manager string }{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
	} {
		path := filepath.Join(root, c.file)
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		src.Manager = c.manager
		// Lockfile **必须记下**：它是"我们看到了但没读懂"的那个文件。
		// 此前这里只设 Manager，于是人读那行打印 "no lock file"——
		// 而眼睛看到的是"项目里有 pnpm-lock.yaml"，两句话互相矛盾。
		src.Lockfile = c.file
		if raw, rerr := os.ReadFile(path); rerr == nil {
			src.LockfileDigest = "sha256:" + digest.HashBytes(raw)
		}
		notes = append(notes, fmt.Sprintf(
			"%s is present but ngm does not parse it, and will not: registry provenance "+
				"is your installer's integrity field, not ngm's axis (ngm proves `content <-> commit`). "+
				"Every package below is reported with provenance=unknown; the content digests are still exact. "+
				"To put a *git* dependency under ngm, declare it with `ngm add` — ngm.vendor/ and "+
				"node_modules/ coexist", c.file))
		return nil, notes, src
	}

	notes = append(notes, "no lock file was found; every package below is reported with "+
		"provenance=unknown. The content digests are still exact")
	return nil, notes, src
}

// npmLock 只取 v2/v3 的 `packages` 段（v1 没有它，解析结果为空——如实降级）。
type npmLock struct {
	LockfileVersion int `json:"lockfileVersion"`
	Packages        map[string]struct {
		Version   string `json:"version"`
		Resolved  string `json:"resolved"`
		Integrity string `json:"integrity"`
	} `json:"packages"`
}

func parseNPMLock(raw []byte) (map[string]lockEntry, error) {
	var l npmLock
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}
	out := make(map[string]lockEntry, len(l.Packages))
	for path, e := range l.Packages {
		// 只收依赖条目（`node_modules/...`）；根条目与 workspace 条目没有 resolved。
		if !strings.Contains(path, "node_modules/") && !strings.HasPrefix(path, "node_modules/") {
			continue
		}
		out[path] = lockEntry{Version: e.Version, Resolved: e.Resolved, Integrity: e.Integrity}
	}
	return out, nil
}

func (s *scanner) provenanceFor(relPath string) Provenance {
	e, ok := s.lock[relPath]
	if !ok {
		return Provenance{Kind: KindUnknown}
	}
	if u, commit, ref, isGit := parseGitResolved(e.Resolved); isGit {
		return Provenance{Kind: KindGit, URL: u, Commit: commit, Ref: ref}
	}
	if e.Integrity != "" || e.Resolved != "" {
		return Provenance{Kind: KindRegistry, Resolved: e.Resolved, Integrity: e.Integrity}
	}
	return Provenance{Kind: KindUnknown}
}

// parseGitResolved 解析 npm 写的 Git 来源。
//
// npm 的形态（实测）：
//
//	git+ssh://git@github.com/org/repo.git#<40hex>
//	git+https://github.com/org/repo.git#<40hex>
//	git+file:///path/to/repo#<40hex>
//
// fragment 不是 40 位 hash 时（手写的 `#v1.2.0`）记为 Ref 而**不是** Commit：
// 那个字符串不可追溯，把它当成 commit 是这类工具最常见的一种谎。
func parseGitResolved(resolved string) (url, commit, ref string, ok bool) {
	const p = "git+"
	if !strings.HasPrefix(resolved, p) && !strings.HasPrefix(resolved, "git:") {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(resolved, p)
	u, frag := rest, ""
	if i := strings.LastIndex(rest, "#"); i >= 0 {
		u, frag = rest[:i], rest[i+1:]
	}
	if isFullHex(frag) {
		return u, frag, "", true
	}
	return u, "", frag, true
}

func isFullHex(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// package.json 读取（只读两个字段，失败即降级——不猜）
// ---------------------------------------------------------------------------

type pkgJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func readPackageName(dir, fallback string) string {
	if p, ok := readPkgJSON(dir); ok && p.Name != "" {
		return p.Name
	}
	return fallback
}

func readPackageVersion(dir string) string {
	if p, ok := readPkgJSON(dir); ok {
		return p.Version
	}
	return ""
}

func readPkgJSON(dir string) (pkgJSON, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return pkgJSON{}, false
	}
	var p pkgJSON
	if err := json.Unmarshal(raw, &p); err != nil {
		return pkgJSON{}, false
	}
	return p, true
}
