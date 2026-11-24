package supplychain

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// CheckAnchorNoDowngrade 比较同一依赖的旧锚点与新锚点，只有**从强退到弱**才报错。
//
// 强度阶梯（ADR-053）：commit / dirDigest（local-dir）强于 tag，tag 强于 branch。
// 升级（branch→tag/commit）与同级都放行——这个策略防的是"锁定强度被悄悄削弱"，
// 不是要求每次 update 都变得更强。
//
// 语义边界：
//   - 空字符串、local-git（ref 可能是 tag/branch/commit，单看类型判不出强度）视为未知，
//     放行；调用方若拿不到旧条目也不该把"缺失"误判成降级。
//   - 本函数只比较**类型**，不比较具体 commit/digest（内容变化由 digest 门禁负责）。
//   - 错误码 CodeConfigInvalid（exit 3）：这与白名单/晾晒期拒绝是同类"策略不允许"。
func (p *Policy) CheckAnchorNoDowngrade(oldRefType, newRefType string) error {
	if p == nil || !p.NoDowngrade() {
		return nil
	}
	oldStrength, oldKnown := anchorStrength(oldRefType)
	newStrength, newKnown := anchorStrength(newRefType)
	if !oldKnown || !newKnown || newStrength >= oldStrength {
		return nil
	}
	return errs.New(errs.CodeConfigInvalid,
		fmt.Sprintf("anchor downgraded from %s to %s", strings.TrimSpace(oldRefType), strings.TrimSpace(newRefType)),
		"pin the ref to a commit or tag, or explicitly disable supplyChain.trustPolicy")
}

// 锚点强度权重。用常量而非字面量：退出码来源判据（TestV57）会扫描
// `return 1`，而这里的 1 只是排序权重、与进程退出码无关。
const (
	anchorStrengthBranch = 1
	anchorStrengthTag    = 2
	anchorStrengthStrong = 3 // commit / dirDigest
)

// anchorStrength 返回锚点强度；第二个数表示类型是否可判定强度。
func anchorStrength(refType string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(refType)) {
	case "commit", "local-dir": // local-dir 的 Commit 字段是 dirDigest，同属内容寻址的强锚点
		return anchorStrengthStrong, true
	case "tag":
		return anchorStrengthTag, true
	case "branch":
		return anchorStrengthBranch, true
	default: // 空 / local-git / 未来新增类型：宁可放行也不误伤
		return 0, false
	}
}
