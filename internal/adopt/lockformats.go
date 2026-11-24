package adopt

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// 本文件提供 pnpm / yarn lock 的**窄形状**解析（2026-11-18 D1）。
//
// 决策（见 docs/specs/assessment-2026-11-18-followup/spec.md §3）：
//   - 只提取 **Git 来源**（url + commit/ref）进 provenance；registry 条目一律
//     保持 unknown——registry 的完整性是安装器的 integrity 字段，不是 ngm 的轴。
//   - 不引入 YAML 依赖：两种 lock 都只按行读，能被安全识别的形状就认，
//     认不出的条目如实留 unknown，并在 notes 里说明覆盖范围。
//
// 判据是"不猜"：一个解析不出名字或来源的条目绝不编造 provenance。
func lockEntryFromGit(spec, commit, ref, version string) (lockEntry, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return lockEntry{}, false
	}
	// yarn 的 resolved 用 `git+` 前缀；pnpm 的 resolution.git 是裸 URL。
	// 两者都是明确的 Git 来源声明（不是从普通 URL 猜的）。裸 URL 必须有
	// commit 才算 Git 来源——否则可能把 registry 的 tarball URL 误判成 Git。
	explicit := strings.HasPrefix(spec, "git+") || strings.HasPrefix(spec, "git:")
	url := strings.TrimPrefix(spec, "git+")
	// `git://` 是完整 scheme，第二个 TrimPrefix 会把它剥成 `//host/repo.git`。
	// 与 adopt.go 的 parseGitResolved（I-04 修复）保持同一口径。
	if !strings.HasPrefix(url, "git://") {
		url = strings.TrimPrefix(url, "git:")
	}
	if url == "" || (!explicit && commit == "" && ref == "") {
		return lockEntry{}, false
	}
	out := lockEntry{Version: version}
	if commit != "" && isFullHex(commit) {
		out.Resolved = "git+" + url + "#" + commit
	} else if ref != "" {
		out.Resolved = "git+" + url + "#" + ref
	} else {
		out.Resolved = "git+" + url
	}
	return out, true
}

// parseYarnLock 读 yarn.lock v1（经典块格式）与 v2+ 的 git resolution 形状。
//
// v1:
//
//	"github.com/foo/bar#v1.0.0":
//	  version "1.0.0"
//	  resolved "git+https://github.com/foo/bar.git#<40hex>"
//	  integrity sha512-...
//
// v2+（berry）的 key 形如 `"github.com/foo/bar@npm:1.0.0"`，resolution 行是
// `resolution: "github.com/foo/bar@commit=<sha>"`；同样只取 Git 形状。
func parseYarnLock(raw []byte) (map[string]lockEntry, []SkippedEntry, error) {
	out := map[string]lockEntry{}
	var skipped []SkippedEntry
	lines := strings.Split(string(raw), "\n")
	var key string
	var keyName, version, resolved, commit string

	flush := func() {
		if key == "" {
			return
		}
		name := keyName
		if name == "" {
			name = yarnNameFromKey(key)
		}
		// scoped 名（@scope/pkg）保持原样，与 node_modules/@scope/pkg 对齐。
		if name == "" {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "cannot derive a package name from the descriptor key"})
		} else if resolved == "" && commit == "" {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "no Git declaration/anchor (registry or metadata entry); " +
					"provenance stays unknown by design (ADR-040)"})
		} else {
			u, parsedCommit, parsedRef, isGit := parseGitResolved(resolved)
			// v1 把锚点写在 resolved 的 fragment 里；berry 另有 `commit:` 行。
			anchorCommit, anchorRef := commit, parsedRef
			if anchorCommit == "" {
				anchorCommit = parsedCommit
			}
			switch {
			case !isGit && commit == "":
				skipped = append(skipped, SkippedEntry{Key: key,
					Reason: "resolved value is not a Git declaration and no commit was given"})
			case anchorCommit == "" && anchorRef == "":
				skipped = append(skipped, SkippedEntry{Key: key,
					Reason: "Git declaration " + u + " has no commit/ref anchor; " +
						"a bare URL cannot be mapped to a commit"})
			default:
				if entry, ok := lockEntryFromGit(resolved, anchorCommit, anchorRef, version); ok {
					out["node_modules/"+name] = entry
				} else {
					skipped = append(skipped, SkippedEntry{Key: key,
						Reason: "declaration could not be mapped to a Git source"})
				}
			}
		}
		key, keyName, version, resolved, commit = "", "", "", "", ""
	}

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		// 顶层 key（列 0）开始一个新块；缩进行属于当前块。
		if line[0] != ' ' && line[0] != '\t' && strings.HasSuffix(strings.TrimSpace(line), ":") {
			flush()
			key = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), ":"))
			// 多 key 形如 "a@^1", "b@^1"；取第一个，其余是别名。
			if i := strings.Index(key, ", "); i >= 0 {
				key = key[:i]
			}
			key = strings.Trim(key, "\"'")
			if key == "__metadata" {
				key = ""
			}
			continue
		}
		if key == "" {
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "version "):
			version = unquoteYarn(strings.TrimPrefix(t, "version "))
		case strings.HasPrefix(t, "resolved "):
			resolved = unquoteYarn(strings.TrimPrefix(t, "resolved "))
		case strings.HasPrefix(t, "commit "):
			commit = unquoteYarn(strings.TrimPrefix(t, "commit "))
		case strings.HasPrefix(t, "resolution:"):
			// berry：`resolution: "github.com/foo/bar@commit=<sha>"`
			r := strings.TrimSpace(strings.TrimPrefix(t, "resolution:"))
			r = strings.Trim(r, "\"'")
			if i := strings.LastIndex(r, "@commit="); i >= 0 && isFullHex(r[i+len("@commit="):]) {
				commit = r[i+len("@commit="):]
				resolved = "git+" + r[:i]
			}
		}
	}
	flush()
	return out, skipped, nil
}

func unquoteYarn(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// yarnNameFromKey 从 yarn 的 descriptor key 里取包名：
//
//	"left-pad@1.3.0"             → "left-pad"
//	"@scope/pkg@^1.0.0"          → "@scope/pkg"
//	"@scope/pkg#v1.0.0"          → "@scope/pkg"   （v0.136 修）
//
// 注意：Git URL 形态（含 `/`）不在这里拆全路径——调用方有 keyName 时用
// 它；这里只处理 yarn descriptor 的包名部分。
func yarnNameFromKey(key string) string {
	key = strings.Trim(key, "\"'")
	// v0.136：scoped 包 + `#ref`（`"@scope/pkg#v1.0.0"`）此前落进 scoped 分支
	// 只找第二个 `@`、找不到就 `return key`，把版本后缀留在名字里——
	// provenance 用 node_modules/@scope/pkg 查不到该条，Git 来源被误降为 unknown。
	// `IndexAny("@#")` 从 i=0 起不匹配（scoped 的 @ 在首位），因此把 `#` 一并
	// 放进 scoped 分支的查找集。
	if strings.HasPrefix(key, "@") {
		if i := strings.IndexAny(key[1:], "@#"); i >= 0 {
			return key[:i+1]
		}
		return key
	}
	if i := strings.IndexAny(key, "@#"); i > 0 {
		return key[:i]
	}
	return key
}

// parsePNPMLock 读 pnpm-lock.yaml 的 packages 段，只取 Git resolution。
//
// 支持两形状（v6/v9 通用）：
//
//	github.com/foo/bar/abc123...:
//	  name: bar
//	  version: 1.0.0
//	  resolution: {git: https://github.com/foo/bar.git, commit: <sha>}
//
//	/bar@1.0.0:
//	  resolution: {integrity: sha512-...}   ← registry，跳过
func parsePNPMLock(raw []byte) (map[string]lockEntry, []SkippedEntry, error) {
	out := map[string]lockEntry{}
	var skipped []SkippedEntry
	lines := strings.Split(string(raw), "\n")
	inPackages := false
	var key string
	var name, version, gitURL, commit string

	flush := func() {
		if key == "" {
			return
		}
		n := name
		if n == "" {
			n = pnpmNameFromKey(key)
		}
		if n == "" {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "cannot derive a package name from the lock key"})
		} else if gitURL == "" {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "no Git declaration/anchor (registry/integrity entry); " +
					"provenance stays unknown by design (ADR-040)"})
		} else if commit == "" {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "Git declaration " + gitURL + " has no commit anchor; " +
					"tag/ref-only pnpm entries are outside the conservative subset"})
		} else if entry, ok := lockEntryFromGit(gitURL, commit, "", version); ok {
			out["node_modules/"+n] = entry
		} else {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "declaration could not be mapped to a Git source"})
		}
		key, name, version, gitURL, commit = "", "", "", "", ""
	}

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			flush()
			inPackages = t == "packages:" || strings.HasPrefix(t, "packages:")
			if t != "packages:" {
				// snapshots: / dependencies: 等段不再作为包条目处理。
			}
			continue
		}
		if !inPackages {
			continue
		}
		if indent == 2 && strings.HasSuffix(t, ":") {
			flush()
			key = strings.Trim(strings.TrimSuffix(t, ":"), "\"'")
			continue
		}
		if key == "" {
			continue
		}
		switch {
		case strings.HasPrefix(t, "name:"):
			name = strings.Trim(strings.TrimPrefix(t, "name:"), "\"' ")
		case strings.HasPrefix(t, "version:"):
			version = strings.Trim(strings.TrimPrefix(t, "version:"), "\"' ")
		case strings.HasPrefix(t, "resolution:"):
			r := strings.TrimPrefix(t, "resolution:")
			gitURL, commit = parsePNPMResolution(r)
		}
	}
	flush()
	return out, skipped, nil
}

func parsePNPMResolution(r string) (gitURL, commit string) {
	r = strings.Trim(strings.TrimSpace(r), "{}")
	for _, field := range strings.Split(r, ",") {
		field = strings.TrimSpace(field)
		// 按 ": "（冒号+空格）拆：值里的 URL 自带冒号（https://），
		// 用第一个 ":" 会把 git 的值截成 " https"。
		i := strings.Index(field, ": ")
		if i < 0 {
			continue
		}
		k := strings.Trim(strings.TrimSpace(field[:i]), "\"'")
		v := strings.Trim(strings.TrimSpace(field[i+2:]), "\"'")
		switch k {
		case "git":
			gitURL = v
		case "commit":
			commit = v
		}
	}
	return gitURL, commit
}

func pnpmNameFromKey(key string) string {
	key = strings.TrimPrefix(key, "/")
	parts := strings.Split(key, "/")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	return last
}

// ---------------------------------------------------------------------------
// bun.lock（文本）与 deno.lock（JSON）—— 同样是"窄形状 + skipped 说清边界"
// ---------------------------------------------------------------------------

// parseBunLock 读 bun.lock 的保守子集：`packages` 段里**单行**的
//
//	"<name>": ["<name>@<spec>", "", {...}, "<integrity>"]
//
// 只认 spec 里**显式声明**的 Git scheme 加 commit/ref 锚点（与 npm 的
// `git+<url>#<anchor>` 同形）；名字必须来自 key 且与 spec 前缀一致。
// 多行数组、其它写法、registry 条目一律 skipped 并给出原因——不猜包名，
// 也不把没有锚点的 Git URL 写成 commit。老的二进制 bun.lockb 不读。
func parseBunLock(raw []byte) (map[string]lockEntry, []SkippedEntry, error) {
	out := map[string]lockEntry{}
	var skipped []SkippedEntry
	inPackages := false
	sectionIndent := -1
	for _, rawLine := range strings.Split(string(raw), "\n") {
		line := strings.TrimRight(rawLine, "\r")
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		isEntry := inPackages && indent > sectionIndent
		if strings.HasPrefix(t, "{") && strings.Contains(t, `"packages":`) {
			// 紧凑写法：`{"lockfileVersion":0,"packages":{...}}`。
			inPackages, sectionIndent = true, 0
			continue
		}
		if !isEntry && strings.HasPrefix(t, `"`) {
			// 根对象的段头（"packages": / "workspaces": / ...）：只切换段落，
			// 不当作包条目。bun.lock 的 packages 段通常缩进 2，条目缩进 4。
			k, rest, ok := cutQuoted(t)
			if ok && strings.HasPrefix(strings.TrimSpace(rest), ":") {
				inPackages, sectionIndent = k == "packages", indent
				continue
			}
		}
		if !inPackages || !isEntry {
			continue
		}
		if strings.HasPrefix(t, "}") || strings.HasPrefix(t, "]") || strings.HasPrefix(t, ",") {
			continue
		}
		if !strings.HasPrefix(t, `"`) {
			skipped = append(skipped, SkippedEntry{Key: t,
				Reason: "unsupported entry shape; expected a quoted package key"})
			continue
		}
		key, rest, ok := cutQuoted(t)
		if !ok || !strings.HasPrefix(strings.TrimSpace(rest), ":") {
			skipped = append(skipped, SkippedEntry{Key: t,
				Reason: "cannot read the quoted package key of this entry"})
			continue
		}
		after := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ":"))
		if !strings.HasPrefix(after, "[") {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "entry is not the supported single-line array shape"})
			continue
		}
		spec, ok := firstQuoted(strings.TrimPrefix(after, "["))
		if !ok {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "first array element is not a quoted spec string; " +
					"multi-line entries are outside the conservative subset"})
			continue
		}
		prefix := key + "@"
		if !strings.HasPrefix(spec, prefix) {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "spec " + spec + " does not declare the same package name as the key"})
			continue
		}
		gitSpec := strings.TrimPrefix(spec, prefix)
		u, commit, ref, isGit := parseGitResolved(gitSpec)
		if !isGit {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "spec has no Git declaration/anchor (registry/integrity entry); " +
					"provenance stays unknown by design (ADR-040)"})
			continue
		}
		if commit == "" && ref == "" {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "Git declaration " + u + " has no commit/ref anchor; a bare URL " +
					"cannot be mapped to a commit"})
			continue
		}
		if entry, ok := lockEntryFromGit(gitSpec, commit, ref, ""); ok {
			out["node_modules/"+key] = entry
		} else {
			skipped = append(skipped, SkippedEntry{Key: key,
				Reason: "declaration could not be mapped to a Git source"})
		}
	}
	return out, skipped, nil
}

// cutQuoted 取 s 开头的双引号字符串，返回内容与剩余部分。
func cutQuoted(s string) (string, string, bool) {
	if len(s) < 2 || s[0] != '"' {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '"' {
			return s[1:i], s[i+1:], true
		}
	}
	return "", s, false
}

func firstQuoted(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '"' {
			v, _, ok := cutQuoted(s[i:])
			return v, ok
		}
	}
	return "", false
}

// parseDenoLock 读 deno.lock 的 JSON 形式（version 4/5）的 `specifiers` 段。
//
// 只认显式 Git specifier（`git+…` / `git://…`）且带锚点（`#<anchor>` 或
// `?rev=`/`?tag=`）的条目。包名由**声明的 URL 路径末段**推出，仅用作
// `node_modules/<name>` 的查表键；若同一名字有多个不同锚点则整组 skipped，
// 不可映射就 skipped。npm/jsr/remote 段只有 integrity，逐条 skipped 说明原因。
func parseDenoLock(raw []byte) (map[string]lockEntry, []SkippedEntry, error) {
	var l struct {
		Version    json.RawMessage            `json:"version"`
		Specifiers map[string]string          `json:"specifiers"`
		NPM        map[string]json.RawMessage `json:"npm"`
		JSR        map[string]json.RawMessage `json:"jsr"`
		Remote     map[string]string          `json:"remote"`
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, nil, err
	}
	var skipped []SkippedEntry
	version := denoVersion(l.Version)
	if version != "4" && version != "5" {
		skipped = append(skipped, SkippedEntry{Key: "version",
			Reason: "unsupported deno.lock version " + strconv.Quote(version) +
				"; only JSON version 4/5 is in the conservative subset"})
		return nil, skipped, nil
	}

	type candidate struct {
		spec, url, commit, ref string
	}
	byName := map[string][]candidate{}
	keys := make([]string, 0, len(l.Specifiers))
	for k := range l.Specifiers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, spec := range keys {
		u, commit, ref, ok := denoGitSpecifier(spec)
		if !ok {
			skipped = append(skipped, SkippedEntry{Key: spec,
				Reason: "specifier has no Git declaration (registry/jsr/remote); " +
					"provenance stays unknown by design (ADR-040)"})
			continue
		}
		if commit == "" && ref == "" {
			skipped = append(skipped, SkippedEntry{Key: spec,
				Reason: "Git declaration " + u + " has no commit/ref anchor"})
			continue
		}
		name := denoNameFromURL(u)
		if name == "" {
			skipped = append(skipped, SkippedEntry{Key: spec,
				Reason: "cannot map specifier to a package name without guessing"})
			continue
		}
		byName[name] = append(byName[name], candidate{spec: spec, url: u, commit: commit, ref: ref})
	}

	out := map[string]lockEntry{}
	for name, cands := range byName {
		if len(cands) > 1 {
			for _, c := range cands {
				skipped = append(skipped, SkippedEntry{Key: c.spec,
					Reason: "ambiguous: " + strconv.Itoa(len(cands)) +
						" different Git anchors claim package " + name})
			}
			continue
		}
		c := cands[0]
		// lockEntryFromGit 自己按 commit/ref 拼 `#anchor`；spec 不能带锚点，
		// 否则 provenance 会读成两段 fragment。
		if entry, ok := lockEntryFromGit("git+"+c.url, c.commit, c.ref, ""); ok {
			out["node_modules/"+name] = entry
		} else {
			skipped = append(skipped, SkippedEntry{Key: c.spec,
				Reason: "declaration could not be mapped to a Git source"})
		}
	}
	for k := range l.NPM {
		skipped = append(skipped, SkippedEntry{Key: "npm:" + k,
			Reason: "npm integrity entry: no Git declaration/anchor in deno.lock"})
	}
	for k := range l.JSR {
		skipped = append(skipped, SkippedEntry{Key: "jsr:" + k,
			Reason: "JSR integrity entry: no Git declaration/anchor in deno.lock"})
	}
	for k := range l.Remote {
		skipped = append(skipped, SkippedEntry{Key: "remote:" + k,
			Reason: "remote hash entry: no Git declaration/anchor in deno.lock"})
	}
	return out, skipped, nil
}

func denoVersion(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return strconv.Itoa(n)
	}
	return strings.TrimSpace(string(raw))
}

// denoGitSpecifier 从 deno specifier 里取显式 Git URL 与锚点。
func denoGitSpecifier(spec string) (url, commit, ref string, ok bool) {
	rest := spec
	switch {
	case strings.HasPrefix(rest, "git+"):
		rest = strings.TrimPrefix(rest, "git+")
	case strings.HasPrefix(rest, "git://"):
		// 合法 scheme，原样保留。
	case strings.HasPrefix(rest, "git:"):
		rest = strings.TrimPrefix(rest, "git:")
	default:
		return "", "", "", false
	}
	anchor := ""
	if i := strings.LastIndex(rest, "#"); i >= 0 {
		anchor, rest = rest[i+1:], rest[:i]
	} else if i := strings.Index(rest, "?"); i >= 0 {
		base, query := rest[:i], rest[i+1:]
		for _, field := range strings.Split(query, "&") {
			kv := strings.SplitN(field, "=", 2)
			if len(kv) == 2 && (kv[0] == "rev" || kv[0] == "tag") && kv[1] != "" {
				anchor, rest = kv[1], base
				break
			}
		}
		if anchor == "" {
			rest = base
		}
	}
	if isFullHex(anchor) {
		return rest, anchor, "", true
	}
	return rest, "", anchor, true
}

// denoNameFromURL 只从声明的 URL 路径末段取名；取不出就交给 skipped，
// 不拿完整 URL 当名字去猜 node_modules 的布局。
func denoNameFromURL(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		after := u[i+3:]
		if slash := strings.Index(after, "/"); slash >= 0 {
			u = after[slash+1:]
		} else {
			return ""
		}
	}
	u = strings.TrimSuffix(u, "/")
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	u = strings.TrimSuffix(u, ".git")
	if u == "" || strings.ContainsAny(u, "?#@ ") {
		return ""
	}
	return u
}
