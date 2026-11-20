package config

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

// ParseISODuration 解析 ISO 8601 duration（如 P3D / PT12H / P1W），返回 time.Duration。
//
// 用途：`supplyChain.minimumReleaseAge` 的取值校验与门禁计算（见 ADR-009 与
// docs/architecture/supply-chain.md）。放在 config 包是因为它是**配置值的形状**问题：
// 非法取值应在配置加载时就被拒绝，而不是等到 install 开始访问远端之后。
//
// 支持的形式：
//
//	P[nW][nD][T[nH][nM][nS]]     例如 P3D、PT12H、P1W、P1W2D、PT1H30M
//
// **刻意不支持年与月**：一个月的长度是 28–31 天、一年是 365–366 天。"晾晒期"是一道安全阈值，
// 若允许这两种单位，同一条配置在不同时刻会判出不同结果——我们会得到一条**会漂移的门禁**。
// 与其给一个说不准的语义，不如显式拒绝并告诉用户换用周/天/小时。
//
// 返回值必须为正：P0D / PT0S 会被拒绝——"晾晒零秒"没有意义，多半是写错了。
func ParseISODuration(s string) (time.Duration, error) {
	orig := s
	if s == "" {
		return 0, errors.New("empty ISO 8601 duration")
	}
	if s[0] != 'P' {
		return 0, fmt.Errorf(
			"%q is not an ISO 8601 duration; it must start with 'P' (e.g. P3D, PT12H, P1W)", orig)
	}
	s = s[1:]

	var total time.Duration
	inTime := false
	sawField := false

	for s != "" {
		if s[0] == 'T' {
			if inTime {
				return 0, fmt.Errorf("%q: 'T' may appear only once", orig)
			}
			inTime = true
			s = s[1:]
			continue
		}

		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, fmt.Errorf("%q: expected a number at %q", orig, s)
		}
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("%q: invalid number %q", orig, s[:i])
		}
		if i >= len(s) {
			return 0, fmt.Errorf("%q: missing unit after %d (expected one of W/D/H/M/S)", orig, n)
		}

		unit := s[i]
		s = s[i+1:]

		// v0.136：先定单位纳秒数，再检查 n 是否会溢出 int64。此前直接乘，
		// 极端值回绕成小正数，末尾的 total<=0 检不出来——一条"很长"的
		// minimumReleaseAge 静默变成很短的门禁（安全阈值失效）。
		var perUnit time.Duration
		var d time.Duration
		switch {
		case unit == 'W' && !inTime:
			perUnit = 7 * 24 * time.Hour
		case unit == 'D' && !inTime:
			perUnit = 24 * time.Hour
		case unit == 'H' && inTime:
			perUnit = time.Hour
		case unit == 'M' && inTime:
			perUnit = time.Minute
		case unit == 'S' && inTime:
			perUnit = time.Second
		case unit == 'Y' || (unit == 'M' && !inTime):
			return 0, fmt.Errorf(
				"%q uses %q, which is not supported: a month is 28-31 days and a year is 365-366 days, "+
					"so the same policy would decide differently depending on when it runs; "+
					"use weeks/days/hours instead (e.g. P14D instead of P2W is not needed - P2W is fine)",
				orig, string(unit))
		default:
			return 0, fmt.Errorf(
				"%q: unsupported unit %q (expected W/D before 'T', H/M/S after it)", orig, string(unit))
		}

		// 逐字段检查：n * perUnit 不得溢出，且累加后也不得溢出。
		if n > 0 && time.Duration(n) > math.MaxInt64/perUnit {
			return 0, fmt.Errorf("%q: %d%c is too large (would overflow time.Duration)", orig, n, unit)
		}
		d = time.Duration(n) * perUnit
		if d > 0 && total > math.MaxInt64-d {
			return 0, fmt.Errorf("%q: the total duration is too large (would overflow time.Duration)", orig)
		}

		total += d
		sawField = true
	}

	if !sawField {
		return 0, fmt.Errorf("%q has no value; expected something like P3D or PT12H", orig)
	}
	if total <= 0 {
		return 0, fmt.Errorf("%q is zero; a release age gate of zero seconds would not gate anything", orig)
	}
	return total, nil
}
