package version

import "regexp"

// 本文件把仓库里"什么是版本号"的判定收在一处。
//
// 这里**刻意提供两个不同严格度的解析器**而不是一个：
//   - ParseSemver 是严格的 tag 语义（必须三段），用于 `outdated` 选最新 tag；
//     把 `v2.4` 当 semver 会让"tag-date 回退"与"semver"两条规则互相抢。
//   - LooseNumber 是宽松的数字序列提取（"deno 2.4.3" → "2.4.3"），用于引擎
//     版本门槛；引擎输出常带前缀/后缀，且门槛允许只写 major.minor。
//
// 两者曾分别在 observability/outdated.go 与 adapter/engines_extra.go 里用
// 各自的正则实现（2026-11-18 发现行为分叉：v2.4 在一处不匹配、在另一处可比）。
// 收在这里不是为了强行同语义，而是让差异可见、可测、不会再各写一份。

var (
	reSemver      = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)([-+].*)?$`)
	reLooseNumber = regexp.MustCompile(`\d+(?:\.\d+)*`)
)

// SemverParts 返回严格 semver 的 major/minor/patch；不是 semver 时返回 false。
// 接受可选的前导 `v` 与 `-pre` / `+meta` 后缀。
func SemverParts(s string) ([3]int, bool) {
	m := reSemver.FindStringSubmatch(s)
	if m == nil {
		return [3]int{}, false
	}
	return [3]int{atoi(m[1]), atoi(m[2]), atoi(m[3])}, true
}

// LooseNumber 从任意版本串里提取第一段点分数字（"deno 2.4.3" → "2.4.3"）。
// 找不到时返回空串。
func LooseNumber(s string) string { return reLooseNumber.FindString(s) }

// DottedParts 把点分数字串拆成整数切片（"2.4.1" → []int{2,4,1}）。
func DottedParts(s string) []int {
	out := []int{}
	cur := 0
	digits := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			cur = addDigit(cur, int(c-'0'))
			digits = true
			continue
		}
		if c == '.' && digits {
			out = append(out, cur)
			cur, digits = 0, false
		}
	}
	if digits {
		out = append(out, cur)
	}
	return out
}

func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n = addDigit(n, int(s[i]-'0'))
	}
	return n
}

// addDigit 在累加前检查上界，避免超长数字串回绕成负数（审计 I-05）。
// 溢出时夹到最大 int：比较语义仍可用，且不会产生"负 major"这种谎值。
func addDigit(n, d int) int {
	max := int(^uint(0) >> 1)
	if n > (max-d)/10 {
		return max
	}
	return n*10 + d
}
