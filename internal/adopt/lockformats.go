package adopt

import (
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
	url = strings.TrimPrefix(url, "git:")
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
func parseYarnLock(raw []byte) (map[string]lockEntry, error) {
	out := map[string]lockEntry{}
	lines := strings.Split(string(raw), "\n")
	var key string
	var keyName, version, resolved, commit string

	flush := func() {
		if key == "" {
			return
		}
		if entry, ok := lockEntryFromGit(resolved, commit, "", version); ok {
			name := keyName
			if name == "" {
				name = yarnNameFromKey(key)
			}
			// scoped 名（@scope/pkg）保持原样，与 node_modules/@scope/pkg 对齐。
			if name != "" {
				out["node_modules/"+name] = entry
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
	return out, nil
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
//	"github.com/foo/bar#v1.0.0" → "bar"
//	"@scope/pkg@^1.0.0"          → "@scope/pkg"
//	"left-pad@1.3.0"             → "left-pad"
func yarnNameFromKey(key string) string {
	key = strings.Trim(key, "\"'")
	if i := strings.IndexAny(key, "@#"); i > 0 {
		return key[:i]
	}
	if strings.HasPrefix(key, "@") {
		// scoped：跳过 scope 段再找 @。
		if i := strings.Index(key[1:], "@"); i >= 0 {
			return key[:i+1]
		}
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
func parsePNPMLock(raw []byte) (map[string]lockEntry, error) {
	out := map[string]lockEntry{}
	lines := strings.Split(string(raw), "\n")
	inPackages := false
	var key string
	var name, version, gitURL, commit string

	flush := func() {
		if key == "" {
			return
		}
		if gitURL != "" {
			if entry, ok := lockEntryFromGit(gitURL, commit, "", version); ok {
				n := name
				if n == "" {
					n = pnpmNameFromKey(key)
				}
				if n != "" {
					out["node_modules/"+n] = entry
				}
			}
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
	return out, nil
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
