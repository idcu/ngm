package verify

import (
	"strings"
	"testing"
)

// FuzzGHEscape 断言 workflow command 的转义**无法被输入伪造**。
//
// 输出里不允许出现裸换行/回车（否则一条注解会被拆成两条命令），
// 且每个 `%` 都必须是受支持的转义序列（首尾不完整的 `%` 同）。
func FuzzGHEscape(f *testing.F) {
	f.Add("index.ts")
	f.Add("a%b\r\nc")
	f.Add("x:y,title=forged")
	f.Add("%25%0A")
	f.Fuzz(func(t *testing.T, s string) {
		out := ghEscape(s)
		if strings.ContainsAny(out, "\r\n") {
			t.Fatalf("escaping left a raw newline in %q", out)
		}
		for i := 0; i < len(out); i++ {
			if out[i] != '%' {
				continue
			}
			if i+2 >= len(out) {
				t.Fatalf("truncated escape at end of %q", out)
			}
			switch out[i+1 : i+3] {
			case "25", "0A", "0D", "3A", "2C":
			default:
				t.Fatalf("unsupported escape %%%s in %q", out[i+1:i+3], out)
			}
			i += 2
		}
	})
}
