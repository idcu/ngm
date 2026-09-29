package git

import (
	"context"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// RemoteRef 是 `git ls-remote` 输出的一行。
type RemoteRef struct {
	// SHA 是 40 位十六进制对象 ID。
	SHA string
	// Name 是完整 ref 名（如 refs/heads/main、refs/tags/v1.2.3）。
	// 对于被剥壳（peeled）的 annotated tag 行，Name 不含 `^{}` 后缀，
	// 由 Peeled 字段区分。
	Name string
	// Peeled 为 true 表示原始行是 `<ref>^{}`——即 annotated tag 解引用后的 commit。
	Peeled bool
}

// LSRemote 执行 `git ls-remote <url> [patterns...]` 并解析输出。
//
// 说明：
//   - 不带 patterns 时输出全部 refs
//   - 对 annotated tag，git 会额外输出一行 `<sha>\t<ref>^{}`
//     （该行无法用 --refs 过滤掉，必须在解析层保留 Peeled 标记）
//   - `--refs` 不可用：它会丢掉我们需要的 peeled 行
//
// 返回的切片保持 git 的输出顺序（未排序），调用方自行索引。
func LSRemote(ctx context.Context, opts Options, url string, patterns ...string) ([]RemoteRef, error) {
	args := []string{"ls-remote"}
	args = append(args, patterns...)
	args = append(args, url)

	// 注意参数顺序：git 要求 options/patterns 在 repository 之前。
	// 上面的拼接保证 url 永远是最后一个参数。
	res, err := Run(ctx, opts, args...)
	if err != nil {
		return nil, err
	}
	return ParseLSRemoteOutput(res.Stdout), nil
}

// ParseLSRemoteOutput 解析 ls-remote 的原始 stdout 字节。
//
// 格式：每行 `<40-hex-sha>\t<refname>`，行尾 LF（git 在 Windows 上也输出 LF）。
// 无法识别的行被跳过（git 偶尔会输出 warning 到 stdout 的极端情况）。
func ParseLSRemoteOutput(out []byte) []RemoteRef {
	var refs []RemoteRef
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		sha := line[:tab]
		name := line[tab+1:]
		if len(sha) != 40 || !isHex(sha) {
			continue
		}
		peeled := strings.HasSuffix(name, "^{}")
		if peeled {
			name = strings.TrimSuffix(name, "^{}")
		}
		refs = append(refs, RemoteRef{SHA: sha, Name: name, Peeled: peeled})
	}
	return refs
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// RefIndex 是 RemoteRef 切片的便捷索引。
type RefIndex struct {
	byName map[string]RemoteRef
	// peeled 记录带 `^{}` 的行（annotated tag 的 commit 指向）。
	peeled map[string]string
}

// NewRefIndex 为 refs 建立索引。
//
// 同一 Name 若同时存在 peeled 与未剥壳行，peeled 优先记录到 peeled map，
// 未剥壳行记录到 byName。调用方应按需取用：
//
//	idx.Peeled("refs/tags/v1") // annotated tag → commit
//	idx.Lookup("refs/tags/v1") // 原始对象（可能是 tag object）
func NewRefIndex(refs []RemoteRef) *RefIndex {
	idx := &RefIndex{
		byName: make(map[string]RemoteRef, len(refs)),
		peeled: make(map[string]string),
	}
	for _, r := range refs {
		if r.Peeled {
			idx.peeled[r.Name] = r.SHA
			continue
		}
		idx.byName[r.Name] = r
	}
	return idx
}

// Lookup 返回未剥壳的 ref 行。
func (i *RefIndex) Lookup(name string) (RemoteRef, bool) {
	r, ok := i.byName[name]
	return r, ok
}

// Peeled 返回 annotated tag 解引用后的 commit（不存在则 ok=false）。
func (i *RefIndex) Peeled(name string) (string, bool) {
	s, ok := i.peeled[name]
	return s, ok
}

// Len 返回索引到的 ref 数量（不含 peeled 重复行）。
func (i *RefIndex) Len() int { return len(i.byName) }

// ErrRefNotFound 构造"ref 不存在"的配置类错误（退出码 3）。
//
// 归类理由：ref 不存在意味着 ngm.json / 上游声明的 ref 与实际不符，
// 属于"配置/策略错误"（observability.md 的退出码 3），而非网络故障。
func ErrRefNotFound(url, ref, refType, hint string) error {
	return errs.New(
		errs.CodeConfigInvalid,
		url+": "+refType+"/"+ref+" not found on remote",
		hint,
	)
}
