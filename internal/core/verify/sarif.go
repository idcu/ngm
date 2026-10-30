package verify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SARIF 2.1.0 渲染（`ngm verify --format=sarif`）。
//
// 为什么把这个出口放在本包（v0.105）：包文档写的纪律是"**判定与渲染分离**"——
// 本包产出 Report，CLI 只负责把它渲染成 human 或 --json。SARIF 是第三种渲染，
// 它**不引入任何新判定**：每条 result 都从既有的 CheckResult / DriftKind 一对一
// 映射出来。放在本包而不是 CLI，是因为它需要与 --json 共享同一份结构字段，
// 而分散在两处的一致性只能靠人去对。
//
// 三条纪律：
//
//  1. **确定性**：没有时间戳、没有随机 id、没有 map 迭代顺序依赖。
//     同一份 Report 两次渲染必须逐字节相同（否则"两次 CI 的差异"会变成噪声）。
//  2. **不编造位置**：SARIF 的 region 是可选的，而我们**不知道**行号。
//     写一个 `line: 1` 会让 GitHub 把注解钉在文件第一行——那是假信息。
//  3. **级别跟着阻断语义走**：critical / unexpected 是 error（阻断构建），
//     expected 与"未能完成"是 warning。这与退出码表同源，不是另一套判断。

// SARIF 规则 ID。集合固定且排序，便于消费者配置抑制规则。
const (
	RuleRefUnexpected    = "ngm/ref-unexpected"
	RuleRefExpected      = "ngm/ref-expected"
	RuleBindingMismatch  = "ngm/binding-mismatch"
	RuleDigestMismatch   = "ngm/digest-mismatch"
	RuleLandingIntegrity = "ngm/landing-integrity"
	RuleCheckIncomplete  = "ngm/check-incomplete"
)

const sarifSchema = "https://json.schemastore.org/sarif-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string      `json:"name"`
	InformationURI string      `json:"informationUri"`
	Version        string      `json:"version,omitempty"`
	Rules          []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	ShortDescription     sarifText         `json:"shortDescription"`
	DefaultConfiguration sarifRuleDefaults `json:"defaultConfiguration"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifRuleDefaults struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID     string         `json:"ruleId"`
	Level      string         `json:"level"`
	Message    sarifText      `json:"message"`
	Locations  []sarifLoc     `json:"locations,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
}

type sarifLoc struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

// sarifRules 是固定的规则表（顺序即输出顺序，保证确定性）。
func sarifRules() []sarifRule {
	return []sarifRule{
		{
			ID: RuleRefUnexpected, Name: "RefMovedUnexpectedly",
			ShortDescription:     sarifText{Text: "A ref moved in a way that was not expected (tag re-pointed, branch rewritten, or ref deleted)"},
			DefaultConfiguration: sarifRuleDefaults{Level: "error"},
		},
		{
			ID: RuleRefExpected, Name: "BranchAdvanced",
			ShortDescription:     sarifText{Text: "A branch advanced (fast-forward). Not blocking unless --strict"},
			DefaultConfiguration: sarifRuleDefaults{Level: "warning"},
		},
		{
			ID: RuleBindingMismatch, Name: "ModuleBindingMismatch",
			ShortDescription:     sarifText{Text: "The locked subtreeOid does not match commit:subPath"},
			DefaultConfiguration: sarifRuleDefaults{Level: "error"},
		},
		{
			ID: RuleDigestMismatch, Name: "ContentDigestMismatch",
			ShortDescription:     sarifText{Text: "Replaying the content digest produced a different value than ngm.lock records"},
			DefaultConfiguration: sarifRuleDefaults{Level: "error"},
		},
		{
			ID: RuleLandingIntegrity, Name: "LandingIntegrityFailure",
			ShortDescription:     sarifText{Text: "The installed tree (content store / vendor) does not match the locked content"},
			DefaultConfiguration: sarifRuleDefaults{Level: "error"},
		},
		{
			ID: RuleCheckIncomplete, Name: "CheckIncomplete",
			ShortDescription:     sarifText{Text: "The check could not be completed (network failure or missing local data)"},
			DefaultConfiguration: sarifRuleDefaults{Level: "warning"},
		},
	}
}

// RenderSARIF 把一次 verify 渲染成 SARIF 2.1.0。
//
// toolVersion 由调用方注入（`internal/version`），本包不读版本——
// 那会让"渲染"依赖构建产物，而测试就不该依赖它。
func RenderSARIF(rep *Report, toolVersion string) ([]byte, error) {
	if rep == nil {
		return nil, fmt.Errorf("nil report")
	}
	results := make([]sarifResult, 0, len(rep.Dependencies))
	for i := range rep.Dependencies {
		d := &rep.Dependencies[i]
		emittedOperational := false
		for _, c := range d.Checks {
			if c.Status == StatusOK && !c.Operational {
				continue
			}
			if c.Operational {
				emittedOperational = true
			}
			ruleID, level := sarifRuleFor(d, c)
			results = append(results, sarifResult{
				RuleID:     ruleID,
				Level:      level,
				Message:    sarifText{Text: sanitizeMessage(failureMessage(d, c))},
				Locations:  []sarifLoc{{PhysicalLocation: sarifPhysical{ArtifactLocation: sarifArtifact{URI: sarifURI(d, c)}}}},
				Properties: sarifProperties(rep, d, c),
			})
		}
		// 兜底：`Err` 非空但 checks 里没有任何一条 operational 记录。
		//
		// 这不是理论情形——v0.105 之前的 verify 在"ref 检查操作性失败"那一支
		// **整块跳过** digest 检查且不留痕，于是报告里少一条、`Err` 却非空。
		// 渲染层不能因为上游少写一条就把"没查成"渲染成"没问题"。
		if d.Err != "" && !emittedOperational {
			results = append(results, sarifResult{
				RuleID: RuleCheckIncomplete,
				Level:  "warning",
				Message: sarifText{Text: sanitizeMessage(fmt.Sprintf(
					"%s (%s@%s): %s", d.Name, d.RefType, shortSHA(d.Commit), d.Err))},
				Locations:  []sarifLoc{{PhysicalLocation: sarifPhysical{ArtifactLocation: sarifArtifact{URI: "ngm.lock"}}}},
				Properties: sarifProperties(rep, d, CheckResult{Check: "operational", Operational: true}),
			})
		}
	}

	log := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "ngm",
				InformationURI: "https://gitee.com/idcu/ngm",
				Version:        toolVersion,
				Rules:          sarifRules(),
			}},
			Results: results,
		}},
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// sarifRuleFor 把 (漂移分类, 检查名) 映射到一条规则与一个级别。
func sarifRuleFor(d *DepResult, c CheckResult) (ruleID, level string) {
	if c.Operational {
		return RuleCheckIncomplete, "warning"
	}
	switch d.DriftKind {
	case DriftCritical:
		if c.Check == CheckDigest {
			return RuleDigestMismatch, "error"
		}
		if c.Check == CheckBinding {
			return RuleBindingMismatch, "error"
		}
		return RuleLandingIntegrity, "error"
	case DriftUnexpected:
		return RuleRefUnexpected, "error"
	case DriftExpected:
		return RuleRefExpected, "warning"
	}
	// 没有漂移分类却是 fail：按检查名归位，级别仍是 error——
	// "这条检查失败了，而报告没给出分类"是可行动的信息，不该被降级成 note。
	switch c.Check {
	case CheckDigest:
		return RuleDigestMismatch, "error"
	case CheckBinding:
		return RuleBindingMismatch, "error"
	case CheckLanding:
		return RuleLandingIntegrity, "error"
	case CheckRef:
		return RuleRefUnexpected, "error"
	}
	return RuleCheckIncomplete, "warning"
}

// failureMessage 是两个渲染出口共用的**原始**文本（可能带换行）。
//
// 它刻意不做清洗：换行怎么处理是**格式自己的事**——SARIF 要单行
// （查看器会截断），workflow command 要 `%0A` 转义（否则后半句会变成
// 一条新的命令）。把清洗放进这里，两个格式就只剩一套规则，
// 而那套规则必然对其中一个格式是错的。
func failureMessage(d *DepResult, c CheckResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s@%s)", d.Name, d.RefType, shortSHA(d.Commit))
	if d.SubPath != "" {
		fmt.Fprintf(&b, " [%s]", d.SubPath)
	}
	if c.Detail != "" {
		b.WriteString(": ")
		b.WriteString(c.Detail)
	}
	if d.Remediation != "" {
		b.WriteString(" — next: ")
		b.WriteString(d.Remediation)
	}
	return b.String()
}

// sarifURI 返回这条结果指向的仓库内路径。
//
// 只用**仓库内的相对路径**：SARIF 消费者的 `file=` 匹配是相对仓库根的。
// 用绝对路径会让 GitHub 把注解挂到不存在的位置，且泄漏构建机路径。
func sarifURI(d *DepResult, c CheckResult) string {
	if c.Check == CheckLanding && d.VendorPath != "" {
		return strings.ReplaceAll(d.VendorPath, "\\", "/")
	}
	return "ngm.lock"
}

func sarifProperties(rep *Report, d *DepResult, c CheckResult) map[string]any {
	p := map[string]any{
		"name":       d.Name,
		"ref":        d.Ref,
		"refType":    d.RefType,
		"commit":     d.Commit,
		"check":      c.Check,
		"exitCode":   rep.Summary.ExitCode,
		"offline":    rep.Offline,
		"strict":     rep.Strict,
		"allowDrift": rep.AllowDrift,
	}
	if d.DriftKind != DriftNone {
		p["driftKind"] = string(d.DriftKind)
	}
	if d.ArchiveDigest != "" {
		p["archiveDigest"] = d.ArchiveDigest
	}
	if d.ModuleDigest != "" {
		p["moduleDigest"] = d.ModuleDigest
	}
	if d.SubPath != "" {
		p["subPath"] = d.SubPath
	}
	if c.Operational {
		p["operational"] = true
	}
	return p
}

// sanitizeMessage 去掉换行：SARIF 的 message 是单行文本，
// 而 CheckResult.Detail 可能带 vendor 树的多行差异。
func sanitizeMessage(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "; ")
	s = strings.ReplaceAll(s, "\n", "; ")
	s = strings.ReplaceAll(s, "\r", "; ")
	return strings.TrimSpace(s)
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	if s == "" {
		return "unknown"
	}
	return s
}
