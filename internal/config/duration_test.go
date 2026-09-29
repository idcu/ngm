package config

import (
	"strings"
	"testing"
	"time"
)

// TestParseISODuration 表驱动覆盖取值与拒绝两侧。
//
// 拒绝的用例与接受的用例同样重要：`minimumReleaseAge` 是安全阈值，
// 一个"看起来被接受、实际语义说不准"的取值比直接报错更危险
// （尤其是年/月这两种长度不固定的单位）。
func TestParseISODuration(t *testing.T) {
	hour := time.Hour
	day := 24 * hour

	cases := []struct {
		in   string
		want time.Duration
		ok   bool
	}{
		// 文档里给出的三种形式
		{"P3D", 3 * day, true},
		{"PT12H", 12 * hour, true},
		{"P1W", 7 * day, true},

		// 组合与边界
		{"P1W2D", 9 * day, true},
		{"PT1H30M", hour + 30*time.Minute, true},
		{"PT30S", 30 * time.Second, true},
		{"P1DT1S", day + time.Second, true},

		// 拒绝：长度不固定的单位（本实现的核心取舍）
		{"P1M", 0, false},
		{"P1Y", 0, false},
		{"P1Y6M", 0, false},

		// 拒绝：形状错误
		{"", 0, false},
		{"P", 0, false},
		{"3D", 0, false},
		{"P3", 0, false},
		{"P-1D", 0, false},
		{"PD", 0, false},
		{"P3X", 0, false},
		{"P1D2H", 0, false},     // 时分必须在 T 之后
		{"PT1D", 0, false},      // 天不能在 T 之后
		{"P1DT2DT3H", 0, false}, // 重复段
		{"P1DT1H T2H", 0, false},
		{"  P3D", 0, false},

		// 拒绝：零值（"晾晒零秒"没有意义，多半是写错了）
		{"P0D", 0, false},
		{"PT0S", 0, false},
		{"P0W", 0, false},
	}

	for _, c := range cases {
		got, err := ParseISODuration(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseISODuration(%q): unexpected error: %v", c.in, err)
				continue
			}
			if got != c.want {
				t.Errorf("ParseISODuration(%q) = %v, want %v", c.in, got, c.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseISODuration(%q) = %v, want an error", c.in, got)
			continue
		}
		// 错误信息必须是可操作的：说清哪儿错、以及该换成什么
		if !strings.Contains(err.Error(), c.in) && c.in != "" {
			t.Errorf("ParseISODuration(%q) error does not mention the input: %v", c.in, err)
		}
	}
}

// TestParseISODuration_MonthAndYearExplainWhy 固定"为什么拒绝"这件事必须在错误里讲出来。
//
// 若只说 "invalid duration"，用户会去猜是不是格式写错了；而真实原因是**语义不可靠**。
func TestParseISODuration_MonthAndYearExplainWhy(t *testing.T) {
	for _, in := range []string{"P1M", "P1Y"} {
		_, err := ParseISODuration(in)
		if err == nil {
			t.Fatalf("ParseISODuration(%q): want error", in)
		}
		msg := err.Error()
		for _, want := range []string{"28-31", "365-366"} {
			if !strings.Contains(msg, want) {
				t.Errorf("ParseISODuration(%q) error should explain the variable length (%q missing): %v", in, want, err)
			}
		}
	}
}
