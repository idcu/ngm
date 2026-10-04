package resolve

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// TestV12PortIsRefusedNotDropped 固定端口处理的语义。
//
// 此前 `https://git.example.com:8443/org/repo` 里的 `:8443` 被 splitURLForm
// 静默剥掉：ngm 去连**默认端口上的另一个服务**，失败后报"检查网络连通性"，
// 而网络是好的（v0.11 C 组 D1 实测，CONNECT 隧道 502）。
//
// 现在的规则只有两条，且都不含"悄悄换一个地址"：
//
//	默认端口 → 与不写端口语义相同，丢弃无副作用
//	其它端口 → 明确拒绝（exit 3 + 报出那个端口），而不是支持一半
func TestV12PortIsRefusedNotDropped(t *testing.T) {
	t.Run("a non-default port is refused, and the message names it", func(t *testing.T) {
		_, err := Normalize("https://git.example.com:8443/org/repo")
		if err == nil {
			t.Fatal("a port ngm cannot carry must not be silently dropped")
		}
		if code := errs.ExitCode(err); code != 3 {
			t.Errorf("exit code=%d want 3 (an address ngm cannot represent is a configuration error)", code)
		}
		// 端口出现在 hint 里，因此断言渲染后的文本（NgmError.Error() 不含 hint）。
		if human := errs.FormatHuman(err); !strings.Contains(human, "8443") {
			t.Errorf("the refusal must quote the port it refuses; got:\n%s", human)
		}
	})

	t.Run("the scheme's own default port is the same thing as no port", func(t *testing.T) {
		for _, in := range []string{
			"https://git.example.com:443/org/repo",
			"http://git.example.com:80/org/repo",
			"git://git.example.com:9418/org/repo",
			"ssh://git@git.example.com:22/org/repo",
		} {
			got, err := Normalize(in)
			if err != nil {
				t.Fatalf("%s: the scheme default is not a distinct address: %v", in, err)
			}
			if strings.Contains(got.String(), ":") {
				t.Errorf("%s: canonical form must not carry a port, got %s", in, got)
			}
		}
	})

	t.Run("the rule is per scheme, not per port number", func(t *testing.T) {
		// :443 在 https 下是默认端口，在 git:// 下不是——按 scheme 判定，不按端口号猜。
		if _, err := Normalize("git://git.example.com:443/org/repo"); err == nil {
			t.Fatal(":443 under git:// is not the scheme default and must be refused")
		}
	})

	t.Run("a colon with no number is refused too", func(t *testing.T) {
		if _, err := Normalize("https://git.example.com:/org/repo"); err == nil {
			t.Fatal("a missing port number must be an error, not silently dropped")
		}
	})
}
