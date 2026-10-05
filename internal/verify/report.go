// Package verify 实现 `ngm verify` 的三级检查与退出码契约。
//
// 规范唯一事实源：
//
//	architecture/observability.md §ngm verify   三级检查、driftKind、退出码
//	architecture/locking.md §更新策略           verify 的职责边界（检查漂移）
//	architecture/trust-model.md                 信任链：能证明什么、不能证明什么
//	development/v0.1-plan.md M5                 任务与验收
//
// 三级检查（默认全开，--deep 为可选增强）：
//
//	检查一 ref → commit  重新解析 refType，与 lock 对比 → 漂移分类
//	检查二 digest 重放   从 mirror 的同一 commit 重建清单、重算 digest，与 lock 对比
//	检查三 落地完整性    content store 元数据 + vendor 树（--deep 追加逐文件哈希）
//
// 设计纪律：
//
//   - **判定与渲染分离**：本包只产出 Report，退出码由 Report.Summary.ExitCode 承载，
//     CLI 只负责把它渲染成 human 或 --json。这样两种输出必然共享同一份判定。
//   - **不完整的校验不得被读作"通过"**：操作性失败（网络/资源缺失）使该依赖
//     标记 Err，并让整体退出码为 4——即使其余依赖全部匹配。
//   - **不猜测**：无法证明快进时按"非预期漂移"处理（保守），而不是默认放行。
package verify

import (
	"encoding/json"

	"github.com/idcu/ngm/internal/errs"
)

// ReportVersion 是 --json 报告的 schema 版本。
//
// 与 lockfileVersion / 清单规范版本无关：报告是 CLI 的对外契约，
// 不与仓库格式绑定，因此单独演进。
const ReportVersion = 1

// 检查名（也用于 --json 的 `checks[].check`）。
const (
	// CheckRef 检查一：ref → commit。
	CheckRef = "ref"
	// CheckDigest 检查二：digest 重放。
	CheckDigest = "digest"
	// CheckLanding 检查三：落地完整性。
	CheckLanding = "landing"
)

// 检查状态。
const (
	// StatusOK 表示该检查通过。
	StatusOK = "ok"
	// StatusFail 表示该检查未通过（含"未能完成"）。
	StatusFail = "fail"
)

// DriftKind 是漂移分类，取值为 observability.md 定义的三种之一。
//
// 它决定了退出码与"是否阻断构建"，因此是本命令最重要的输出字段：
// CI 只应依赖 `driftKind` 与退出码，不应解析人类文本。
type DriftKind string

const (
	// DriftNone 表示无漂移（--json 中省略该字段）。
	DriftNone DriftKind = ""
	// DriftExpected 预期更新：branch 快进。默认不阻断（--strict 时 exit 1）。
	DriftExpected DriftKind = "expected"
	// DriftUnexpected 非预期漂移：tag 被重打 / 分支历史被改写（--allow-drift 可降级为 0）。
	DriftUnexpected DriftKind = "unexpected"
	// DriftCritical 严重事件：digest 重放不匹配或落地内容被篡改。始终 exit 2。
	DriftCritical DriftKind = "critical"
)

// Rank 返回分类的严重度，用于聚合多个检查的结论（critical > unexpected > expected > none）。
func (k DriftKind) Rank() int {
	switch k {
	case DriftCritical:
		return 3
	case DriftUnexpected:
		return 2
	case DriftExpected:
		return 1
	default:
		return 0
	}
}

// CheckResult 是单个检查的结果。
type CheckResult struct {
	// Check 是检查名：ref / digest / landing。
	Check string `json:"check"`
	// Status 是 ok / fail。
	Status string `json:"status"`
	// Drift 是该检查贡献的漂移分类（通过或无漂移时为空）。
	Drift DriftKind `json:"driftKind,omitempty"`
	// Detail 是给用户看的细节（失败时必填，--json 中用于诊断）。
	Detail string `json:"detail,omitempty"`
	// Operational 为 true 表示该检查**未能完成**（网络/资源问题），
	// 而不是得出了"有问题"的结论。区别很重要：前者不该被当成判定。
	Operational bool `json:"operational,omitempty"`
}

// DepResult 是单个依赖的三级检查汇总。
type DepResult struct {
	// Name / Ref / RefType / SubPath / Commit / ArchiveDigest / VendorPath 回显 lock 声明，
	// 让 --json 的消费者无需再读 ngm.lock。
	Name          string `json:"name"`
	Ref           string `json:"ref"`
	RefType       string `json:"refType"`
	SubPath       string `json:"subPath,omitempty"`
	Commit        string `json:"commit"`
	ArchiveDigest string `json:"archiveDigest"`
	VendorPath    string `json:"vendorPath,omitempty"`

	// ResolvedCommit 是本次重新解析得到的 commit。与 Commit 不同即表示 ref 已移动。
	ResolvedCommit string `json:"resolvedCommit,omitempty"`
	// DriftKind 是聚合后的漂移分类（空 = 匹配且落地完整）。
	DriftKind DriftKind `json:"driftKind,omitempty"`
	// Stale 为 true 表示 ref 对比基于本地 mirror 快照（--offline）。
	// 此时"未漂移"只对快照成立——上游可能已经移动，必须如实告知消费者。
	Stale bool `json:"stale,omitempty"`
	// Checks 是三级检查的结果，按执行顺序（ref → digest → landing）。
	Checks []CheckResult `json:"checks"`
	// Remediation 是给用户的下一步建议（human 输出的 `→` 行）。
	Remediation string `json:"remediation,omitempty"`
	// Err 是操作性失败的描述。非空表示校验未能完成，整体退出码为 4。
	Err string `json:"error,omitempty"`
	// ErrHint 是**那个错误自己带的建议**（沿包装链找第一条非空的）。
	//
	// 为什么要有这个字段（v0.35）：`res.Err = merr.Error()` 只留 message，
	// 而最能说清"接下来做什么"的往往是**最贴近成因的那一层**——
	// 权限层知道要往 `permissions.allow` 里加哪个 host，mirror 层知道
	// "run once without --offline"。丢掉它，报告就只能给一句通用的建议
	// （实测：`ngm install` 对"权限被拒"是错的下一步——它也会被拒）。
	ErrHint string `json:"errorHint,omitempty"`
}

// Summary 是报告聚合，也是退出码的唯一来源。
type Summary struct {
	Total int `json:"total"`
	// OK 是三级检查全部通过、且无漂移的依赖数。
	OK int `json:"ok"`
	// Expected / Unexpected / Critical 按 driftKind 计数。
	Expected   int `json:"expected"`
	Unexpected int `json:"unexpected"`
	Critical   int `json:"critical"`
	// Operational 是未能完成校验的依赖数（网络/资源缺失）。
	Operational int `json:"operational"`
	// ExitCode 是本次 verify 的全局退出码：0 / 1 / 2 / 4。
	// 3 由 CLI 层在"lock 缺失或非法"等场景直接返回，不出现在本报告里。
	ExitCode int `json:"exitCode"`
}

// Report 是一次 verify 的完整结果。
type Report struct {
	Version      int         `json:"version"`
	Offline      bool        `json:"offline"`
	Deep         bool        `json:"deep"`
	Strict       bool        `json:"strict"`
	AllowDrift   bool        `json:"allowDrift"`
	Dependencies []DepResult `json:"dependencies"`
	Summary      Summary     `json:"summary"`
}

// ExitCode 依据 observability.md 的退出码契约计算最终退出码。
//
// 优先级（高 → 低）：
//
//	4  操作性失败：有依赖因网络/资源缺失而未能完成校验。
//	   刻意高于 2 —— 不完整的校验不得被读作"完整性通过"。
//	2  完整性失败：digest 重放不匹配 / vendor 或 content store 被篡改。
//	   --allow-drift **不**降级它（observability.md："critical → 阻断，exit 2，禁止构建"）。
//	1  非预期漂移（tag 重打 / 分支改写）；--allow-drift 降级为 0。
//	1  仅有预期更新且给了 --strict；否则 0。
//	0  全部匹配。
//
// 注意 --strict 与 --allow-drift 同时给出时的语义：两者作用于**不同**的分类
// （strict → expected，allow-drift → unexpected），互不抵消，因此可能出现
// "expected 被升级、unexpected 被降级"的组合结果。文档未规定二者互斥，故不报错。
func (r *Report) ExitCode() int {
	var hasOperational, hasCritical, hasUnexpected, hasExpected bool
	for i := range r.Dependencies {
		d := &r.Dependencies[i]
		if d.Err != "" {
			hasOperational = true
			continue
		}
		switch d.DriftKind {
		case DriftCritical:
			hasCritical = true
		case DriftUnexpected:
			hasUnexpected = true
		case DriftExpected:
			hasExpected = true
		}
	}

	switch {
	case hasOperational:
		return errs.CodeGitFetch.ExitCode()
	case hasCritical:
		return errs.CodeDigestMismatch.ExitCode()
	case hasUnexpected && !r.AllowDrift:
		return errs.CodeRefDrift.ExitCode()
	case hasExpected && r.Strict:
		return errs.CodeRefDrift.ExitCode()
	default:
		return 0
	}
}

// Summarize 依据 Dependencies 重新计算 Summary（含 ExitCode）并写回。
//
// 必须在所有依赖检查完成后调用一次；Report 的其他字段不受影响。
func (r *Report) Summarize() {
	s := Summary{Total: len(r.Dependencies)}
	for i := range r.Dependencies {
		d := &r.Dependencies[i]
		if d.Err != "" {
			s.Operational++
			continue
		}
		switch d.DriftKind {
		case DriftCritical:
			s.Critical++
		case DriftUnexpected:
			s.Unexpected++
		case DriftExpected:
			s.Expected++
		default:
			s.OK++
		}
	}
	s.ExitCode = r.ExitCode()
	r.Summary = s
}

// Marshal 返回确定格式的 --json 字节流。
//
// 格式纪律与 lock / mappings 一致：2 空格缩进、LF、文件末尾单个换行、
// 字段顺序由 struct 声明固定。CI 解析 --json 时不得受格式抖动影响。
func (r *Report) Marshal() ([]byte, error) {
	out := *r
	if out.Dependencies == nil {
		out.Dependencies = []DepResult{}
	}
	for i := range out.Dependencies {
		if out.Dependencies[i].Checks == nil {
			out.Dependencies[i].Checks = []CheckResult{}
		}
	}
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "marshal verify report", "", err)
	}
	return append(data, '\n'), nil
}
