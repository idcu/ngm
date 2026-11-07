package digest

import (
	"strings"
	"testing"
)

// FuzzRenderManifest 断言清单渲染/摘要对任意字节都不 panic，且结果形状恒定。
//
// 这些输入不是"用户在配置文件里能写出的东西"：manifest 来自 mirror 里的
// 对象归档（见 verify），因此它必须对**任意**字节保持健壮（P2-8）。
func FuzzRenderManifest(f *testing.F) {
	f.Add([]byte("2\x00a\x00index.ts\x00100644\x00hash\x00"))
	f.Add([]byte{})
	f.Add([]byte("\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = RenderManifest(b)
		d := Digest(b)
		if !strings.HasPrefix(d, "sha256:") || len(d) != len("sha256:")+64 {
			t.Fatalf("Digest(%q) = %q, not sha256:<64 hex>", b, d)
		}
		h := HashBytes(b)
		if len(h) != 64 {
			t.Fatalf("HashBytes(%q) = %q, want 64 hex chars", b, h)
		}
		_, _ = DetectLFSPointer(b)
	})
}
