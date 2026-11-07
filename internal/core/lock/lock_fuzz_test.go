package lock

import (
	"encoding/json"
	"testing"
)

// FuzzParse 断言 lock 解析对任意字节都不 panic，且"成功"永远给出非 nil 文件。
//
// lock 是信任根：它来自仓库（可能被恶意构造），所以解析器必须对任意输入
// 保持健壮；`Parse` 返回 (nil, nil) 会让调用方在下一行解引用空指针（P2-8）。
func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"version":2}`))
	f.Add([]byte(`{"version":2,"dependencies":[]}`))
	f.Add([]byte(`not json`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		lf, err := Parse(b, "ngm.lock")
		if err != nil {
			return
		}
		if lf == nil {
			t.Fatal("Parse returned (nil, nil)")
		}
		// 解析成功的文件必须能被校验与重新编码，且不 panic。
		_ = lf.Validate()
		if _, merr := json.Marshal(lf); merr != nil {
			t.Fatalf("Marshal after a successful Parse failed: %v", merr)
		}
		if !IsSafeRelPath("a/b") {
			t.Fatal("IsSafeRelPath('a/b') must stay true")
		}
	})
}
