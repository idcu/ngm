package resolve

import "testing"

// v0.99：`slugHosts` 与 `hostSlugs` 是**互为逆映射的两张表**，而此前没有任何判据。
//
// normalize.go 的原话（第 70 行）："仅收录已知的公共托管服务；其他 host 保持完整。
// **新增 host 时必须同步 hostSlugs**。"——这是一条靠人记住的纪律，而它的失败形态很不显眼：
// 只往一张表里加一行，`github:o/r` 与 `github.com/o/r` 就有一半方向走不通，
// 而走不通的那一半**恰好是反向解析**（用户从 lock 抄一个 slug 回来时才会碰到）。
//
// 三个层次：
//  1. 表本身互为逆映射（两个方向都查）；
//  2. 走公开 API 的往返：`ParseSlug`（slug → Canonical）→ `Slug()`（Canonical → slug）回到原样；
//  3. 未知 host 必须**原样保留**（不能被折叠成某个短名）——那是"其他 host 保持完整"这条。
func TestV99HostMapsAreInverse(t *testing.T) {
	if len(slugHosts) == 0 || len(hostSlugs) == 0 {
		t.Fatal("两张表都不该是空的——空的「逆映射」恒成立，这条判据会退化成什么都不守")
	}

	t.Run("两张表互为逆映射", func(t *testing.T) {
		for host, short := range slugHosts {
			if back, ok := hostSlugs[short]; !ok {
				t.Errorf("slugHosts 有 %q → %q，但 hostSlugs 里没有 %q（反向解析会失败）", host, short, short)
			} else if back != host {
				t.Errorf("两张表不对偶：%q → %q → %q（应当回到 %q）", host, short, back, host)
			}
		}
		for short, host := range hostSlugs {
			if fwd, ok := slugHosts[host]; !ok {
				t.Errorf("hostSlugs 有 %q → %q，但 slugHosts 里没有 %q", short, host, host)
			} else if fwd != short {
				t.Errorf("两张表不对偶：%q → %q → %q（应当回到 %q）", short, host, fwd, short)
			}
		}
	})

	t.Run("公开 API 的往返", func(t *testing.T) {
		for host, short := range slugHosts {
			slug := short + ":org/repo"
			c, err := ParseSlug(slug)
			if err != nil {
				t.Errorf("ParseSlug(%q)：%v", slug, err)
				continue
			}
			if c.Host != host {
				t.Errorf("ParseSlug(%q).Host = %q，want %q", slug, c.Host, host)
			}
			if got := c.Slug(); got != slug {
				t.Errorf("往返没回到原样：%q → %+v → %q", slug, c, got)
			}
		}
	})

	t.Run("未知 host 原样保留", func(t *testing.T) {
		const unknown = "git.example.com"
		c, err := ParseSlug("git.example.com:org/repo")
		if err != nil {
			t.Fatalf("未知但不含空格的 host 应当被接受：%v", err)
		}
		if c.Host != unknown {
			t.Errorf("未知 host 被改写了：%q → %q", unknown, c.Host)
		}
		if got := c.Slug(); got != "git.example.com:org/repo" {
			t.Errorf("未知 host 的 slug 应当原样保留完整域名，得到 %q", got)
		}
	})
}
