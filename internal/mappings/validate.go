package mappings

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// ValidateEnvironment 是校验所需的项目侧信息。
type ValidateEnvironment struct {
	// ProjectDir 是项目根目录（`to` 中的 `./ngm.vendor/...` 以此为基准）。
	ProjectDir string
	// LockNames 是 ngm.lock 中所有依赖的 name（slug，**不含** subPath）。
	//
	// 用 map 表示"已锁定的依赖集合"；为 nil 时跳过该维度校验
	// （例如只有 ngm.json 尚未 install 的场景）。
	LockNames map[string]bool
	// SubPaths 记录每个 name 在 lock 中的 subPath 集合。
	//
	// 用途（v0.3 E 组）：校验 `path` 字段确实是**被锁定的那个子路径**，
	// 而不是手写或过期值。为 nil 时跳过该维度。
	//
	// 类型是"列表"而不是单个字符串：同一个依赖可以有多条子路径条目。
	SubPaths map[string][]string
}

// Finding 是一条校验发现。
type Finding struct {
	// From 是出问题的映射条目（空表示文件级问题）。
	From string
	// Message 是人类可读的描述。
	Message string
	// Fatal 为 true 表示阻断（exit 3）；false 表示警告。
	Fatal bool
}

// Validate 校验 mappings 与 lock / vendor 的一致性。
//
// 检查项（modules/p4-ecosystem.md §校验）：
//
//  1. `from` 是否在 ngm.lock 中
//  2. `to` 路径是否存在（相对项目根解析）
//  3. `main` / `types` 文件是否存在（相对 `to` 解析）
//
// 另外检查协议自洽性：`version` 受支持、`to` 位于 ngm.vendor/ 下。
//
// 返回值是发现列表；`error` 仅用于不可恢复的 IO 失败。
func Validate(f *File, env ValidateEnvironment) ([]Finding, error) {
	var findings []Finding

	if err := f.Validate(); err != nil {
		return []Finding{{Message: err.Error(), Fatal: true}}, nil
	}

	for _, m := range f.Mappings {
		// 1) from 必须在 lock 中
		if env.LockNames != nil && !env.LockNames[m.From] {
			findings = append(findings, Finding{
				From: m.From,
				Message: "is not present in " + lockFileName +
					"; run `ngm install` to refresh (or remove the stale mapping)",
				Fatal: true,
			})
		}

		// 1b) `path`（v0.3 E 组）：它必须是被锁定的那个子路径，且与 `to` 自洽。
		//
		// 这两条合起来回答同一个问题："这个条目说的子路径，和我们锁定的、以及
		// vendor 里落地的是不是同一件事"。三者不一致时，集成脚手架会生成
		// 一条指向别处的别名——那种错误在运行时表现为"模块找不到"，很难回溯。
		if env.SubPaths != nil {
			locked := env.SubPaths[m.From]
			if m.Path != "" && !containsString(locked, m.Path) {
				findings = append(findings, Finding{
					From: m.From,
					Message: "`path` is " + m.Path + ", which is not a locked subpath of this dependency" +
						lockedSubpathsHint(locked) + "; run `ngm install` to regenerate",
					Fatal: true,
				})
			}
			if m.Path == "" && len(locked) > 0 {
				findings = append(findings, Finding{
					From: m.From,
					Message: "has no `path` although the lock records subpath(s) " +
						strings.Join(locked, ", ") + "; the file predates the field — run `ngm install` to regenerate",
					Fatal: false,
				})
			}
			if m.Path != "" && !strings.HasSuffix(strings.TrimSuffix(m.To, "/"), "/"+strings.Trim(m.Path, "/")) {
				findings = append(findings, Finding{
					From: m.From,
					Message: "`to` (" + m.To + ") does not end with the declared `path` (" + m.Path + "); " +
						"`path` and `to` describe the same thing and must agree",
					Fatal: true,
				})
			}
		}

		// 2) to 必须位于 ngm.vendor/ 下且真实存在
		if !strings.HasPrefix(m.To, VendorRelRoot+"/") {
			findings = append(findings, Finding{
				From:    m.From,
				Message: "`to` must point inside " + VendorRelRoot + "/ (got " + m.To + ")",
				Fatal:   true,
			})
			continue
		}
		toAbs := filepath.Join(env.ProjectDir, filepath.FromSlash(strings.TrimPrefix(m.To, "./")))
		toInfo, err := os.Lstat(toAbs)
		if err != nil {
			findings = append(findings, Finding{
				From: m.From,
				Message: "`to` path does not exist on disk (" + m.To + "); " +
					"the vendor tree is missing — run `ngm install`",
				Fatal: true,
			})
			continue
		}

		// symlink 模式的 vendor 条目是链接，os.Stat 跟随它判断目标可读性
		if toInfo.Mode()&os.ModeSymlink != 0 {
			if _, serr := os.Stat(toAbs); serr != nil {
				findings = append(findings, Finding{
					From:    m.From,
					Message: "`to` is a symlink but its target is unreachable (" + m.To + ")",
					Fatal:   true,
				})
				continue
			}
		}

		// 3) main / types 必须存在（相对 to 解析）
		checkEntry := func(kind, p string) {
			if p == "" {
				return
			}
			abs := filepath.Join(toAbs, filepath.FromSlash(strings.TrimPrefix(p, "./")))
			if _, err := os.Stat(abs); err != nil {
				findings = append(findings, Finding{
					From:    m.From,
					Message: "`" + kind + "` file does not exist: " + p,
					Fatal:   true,
				})
			}
		}
		checkEntry("main", m.Main)
		checkEntry("types", m.Types)

		// 无入口字段不算错误，但值得提醒（构建工具会找不到入口）
		if m.Main == "" && m.Types == "" {
			findings = append(findings, Finding{
				From: m.From,
				Message: "has no `main` or `types` — build tools cannot resolve this import; " +
					"upstream can declare them in its ngm.json",
				Fatal: false,
			})
		}
	}

	return findings, nil
}

// FormatFindings 把发现渲染为人类可读文本。
//
// 返回 (text, fatalCount)。
func FormatFindings(findings []Finding) (string, int) {
	if len(findings) == 0 {
		return "", 0
	}
	var sb strings.Builder
	fatal := 0
	for _, f := range findings {
		label := "warning"
		if f.Fatal {
			label = "error"
			fatal++
		}
		if f.From != "" {
			sb.WriteString(label + ": " + f.From + " " + f.Message + "\n")
		} else {
			sb.WriteString(label + ": " + f.Message + "\n")
		}
	}
	return sb.String(), fatal
}

// containsString 报告列表里是否有该值。
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// lockedSubpathsHint 在"`path` 不被锁定时"给出可操作的提示：
// 把 lock 里实际记着的子路径列出来，用户一眼能看出是不是写错了。
func lockedSubpathsHint(locked []string) string {
	if len(locked) == 0 {
		return " (the lock records no subpath for it)"
	}
	return " (the lock records: " + strings.Join(locked, ", ") + ")"
}

// lockFileName 与 internal/lock 的 FileName 保持一致。
//
// 刻意不 import lock：mappings 是协议层，不应对解析层产生依赖；
// 两者口径一致由 CLI 的集成测试保证。
const lockFileName = "ngm.lock"

// ErrInvalid 构造校验失败的统一错误（exit 3）。
func ErrInvalid(summary string) error {
	return errs.New(errs.CodeConfigInvalid, summary,
		"fix the reported mappings or run `ngm install` to regenerate "+FileName)
}
