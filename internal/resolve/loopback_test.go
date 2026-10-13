package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// v0.83（ADR-029）：**回环是唯一放开的单标签主机**。
//
// 起点是一个实测事实：v0.80 想让本机 `git daemon` 跑端到端（没有外网），
// 被"unknown host localhost"挡住；而把地址校验放宽成"内网主机都行"并不必要——
// 带点的内网主机（`git.corp.example.com`、`192.168.1.10`）**本来就允许**。
// 于是这一版的规则是**窄**的：`localhost` / `127.0.0.1` / `::1` 收下，
// 其余单标签主机名照旧拒绝（它们绝大多数是笔误）。
//
// 判据分两侧：收下的必须**各处写法一致**（URL / 裸 canonical / host:path），
// 不收的必须**继续不收**（否则这条放宽会悄悄变成"什么都行"）。

func TestV83LoopbackHostsAreAccepted(t *testing.T) {
	inputs := []string{
		"git://localhost/org/repo",
		"https://localhost/org/repo",
		"localhost/org/repo",
		"localhost:org/repo",
		"git://127.0.0.1/org/repo",
		"127.0.0.1:org/repo",
	}
	for _, in := range inputs {
		got, err := Normalize(in)
		if err != nil {
			t.Errorf("Normalize(%q) 被拒了，而回环应当放行：%v", in, err)
			continue
		}
		if !isLoopbackHost(got.Host) {
			t.Errorf("Normalize(%q).Host = %q，不是回环形态", in, got.Host)
		}
		if got.Path != "org/repo" {
			t.Errorf("Normalize(%q).Path = %q, want org/repo", in, got.Path)
		}
	}

	// 同一个仓库的多种写法必须归一到**同一个** canonical——
	// 否则"能写短形式"会变成一处需要记的例外（镜像路径也会分叉）。
	a := MustNormalize("git://localhost/org/repo")
	b := MustNormalize("localhost/org/repo")
	if a != b {
		t.Errorf("两种写法应当归一到同一个 canonical：%v vs %v", a, b)
	}
}

func TestV83NonLoopbackSingleLabelHostsStayRejected(t *testing.T) {
	// 继续拒绝：单标签主机名（不是回环）。
	for _, in := range []string{"git://myserver/org/repo", "https://git/org/repo", "git://local/org/repo"} {
		_, err := Normalize(in)
		if err == nil {
			t.Errorf("Normalize(%q) 应当被拒（不是回环）", in)
			continue
		}
		// 报错必须**说清怎么办**：把回环列出来（否则下一个人会去猜）。
		// 注意 hint 不在 Error() 里（那只给 message）——要读字段。
		var ngm *errs.NgmError
		if !errors.As(err, &ngm) || !strings.Contains(ngm.Hint, "loopback") {
			t.Errorf("Normalize(%q) 的 hint 应当提到 loopback 这条路：%v", in, err)
		}
	}

	// 已知边界：`[::1]` 仍然被拒（它在 URL 形式那一站就被挡下，见 isLoopbackHost 的注释）。
	// 这条断言是为了让"要不要支持 IPv6 回环"变成一个**看得见**的决定。
	if _, err := Normalize("git://[::1]/org/repo"); err == nil {
		t.Error("`[::1]` 目前不收；若哪天收了，请连同 ADR-029 与这里的注释一起更新")
	}

	// 而"内网 IP / 内网域名"本来就允许——这一条是**既有行为**，
	// 写在这里是为了防止将来有人把 ADR-029 读成"内网都不行"。
	for _, in := range []string{"https://192.168.1.10/org/repo", "https://git.corp.example.com/group/sub/repo"} {
		if _, err := Normalize(in); err != nil {
			t.Errorf("Normalize(%q) 应当允许（带点的内网主机一直是允许的）：%v", in, err)
		}
	}
}
