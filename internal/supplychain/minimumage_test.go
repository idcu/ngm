package supplychain

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
)

func policyWithAge(t *testing.T, iso string) *Policy {
	t.Helper()
	p, err := FromConfig(&config.SupplyChainConfig{MinimumReleaseAge: iso})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestCheckMinimumAge 是纯计算的表驱动测试：不碰 git，因此也不需要 fixture。
func TestCheckMinimumAge(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name         string
		iso          string
		committedAgo time.Duration
		wantErr      bool
		wantCode     int // 期望退出码；0 表示不报错
	}{
		{"exactly at the threshold passes", "P3D", 3 * 24 * time.Hour, false, 0},
		{"older than the threshold passes", "P3D", 10 * 24 * time.Hour, false, 0},
		{"one hour short fails", "P3D", 3*24*time.Hour - time.Hour, true, 3},
		{"hours granularity", "PT12H", 11 * time.Hour, true, 3},
		{"hours granularity passes", "PT12H", 13 * time.Hour, false, 0},
		{"weeks", "P1W", 6 * 24 * time.Hour, true, 3},
		{"no policy never blocks", "", time.Minute, false, 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := policyWithAge(t, c.iso)
			err := p.CheckMinimumAge("github:o/r", now.Add(-c.committedAgo), now)
			if c.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				if code := errs.ExitCode(err); code != c.wantCode {
					t.Errorf("exit = %d, want %d", code, c.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("want no error, got: %v", err)
			}
		})
	}
}

// TestCheckMinimumAge_ReportsWhenItBecomesAvailable 固定"报错必须给出最早可用时间"。
//
// 只说"太新了"等于让用户去猜要等多久；把时间点写出来，他才知道该等还是该走例外。
func TestCheckMinimumAge_ReportsWhenItBecomesAvailable(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	committed := now.Add(-24 * time.Hour)

	p := policyWithAge(t, "P3D")
	err := p.CheckMinimumAge("github:o/r", committed, now)
	if err == nil {
		t.Fatal("want an error")
	}

	var ne *errs.NgmError
	if !errors.As(err, &ne) {
		t.Fatalf("want *errs.NgmError, got %T", err)
	}
	// 最早可用 = committed + 3 天
	want := committed.Add(3 * 24 * time.Hour).Format(time.RFC3339)
	if !strings.Contains(ne.Hint, want) {
		t.Errorf("hint should state the earliest usable time %q; got %q", want, ne.Hint)
	}
	// 时长要人能读（"1d" 而不是 "24h0m0s"）
	if !strings.Contains(ne.Message, "1d ago") {
		t.Errorf("message should render a readable age; got %q", ne.Message)
	}
}

// TestCheckMinimumAge_MissingCommitterDateFails 固定"读不到时间不能静默放行"。
//
// 静默放行会让一道安全门禁在"时间源缺失"时悄悄失效——这比报错危险得多。
func TestCheckMinimumAge_MissingCommitterDateFails(t *testing.T) {
	p := policyWithAge(t, "P3D")
	err := p.CheckMinimumAge("github:o/r", time.Time{}, time.Now())
	if err == nil {
		t.Fatal("a missing committer date must not be treated as old enough")
	}
	if code := errs.ExitCode(err); code != 4 {
		t.Errorf("exit = %d, want 4 (a git/operations failure, not a policy verdict)", code)
	}
}

// TestIsAllowlisted 覆盖紧急通道：被显式放行的仓库不受晾晒期约束。
func TestIsAllowlisted(t *testing.T) {
	p, err := FromConfig(&config.SupplyChainConfig{AllowlistRepos: []string{"github.com/trusted/*"}})
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsAllowlisted("github.com", "trusted/lib") {
		t.Error("a listed repo must be allowlisted")
	}
	if p.IsAllowlisted("github.com", "other/lib") {
		t.Error("an unlisted repo must not be allowlisted")
	}

	// 没有名单就没有例外——不能因为"没配"就放行一切
	empty, err := FromConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if empty.IsAllowlisted("github.com", "any/lib") {
		t.Error("without allowlistRepos nothing may be treated as allowlisted")
	}
}
