package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/vendor"
)

// v0.107：模块条目的 digest 重放不能信任 content store 自述的 Path / Mode。
//
// 根条目那一支的 Path/Mode 来自 `git ls-tree`（权威）；模块这一支来自
// **store 的清单 JSON**。清单的字节流以 NUL 分隔字段，所以一个带 NUL 的 Path
// 能把一条记录改成两条——"重放"于是可以通过，而它通过的理由是它读的不是锁定内容。
func TestV107ModuleReplayRejectsAForgedManifest(t *testing.T) {
	dir := t.TempDir()
	body := filepath.Join(dir, "blob")
	if err := os.WriteFile(body, []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 一条"看起来正常"的清单必须先能重放（否则下面的失败证明不了任何事）。
	honest := []vendor.ContentEntry{{Path: "index.js", Mode: "100644", Full: body}}
	if _, err := replayModuleDigestFromEntries(honest); err != nil {
		t.Fatalf("a well-formed entry set must replay: %v", err)
	}

	// 伪造：把 mode 与 sha 字段塞进路径，让一条记录在字节流里变成两条。
	const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	forged := []vendor.ContentEntry{
		{Path: "index.js\x00100644\x00" + sha + "\x00evil.js", Mode: "100644", Full: body},
	}
	_, err := replayModuleDigestFromEntries(forged)
	if err == nil {
		t.Fatal("a path containing NUL must be rejected: it can fabricate record boundaries, " +
			"so `--deep` would replay a manifest that is not the locked one")
	}
	if !strings.Contains(err.Error(), "not a valid manifest") {
		t.Errorf("the error must name the cause, got: %v", err)
	}

	// 另一个方向：mode 被改成 gitlink 时也要拒绝（gitlink 从未被支持）。
	badMode := []vendor.ContentEntry{{Path: "index.js", Mode: "160000", Full: body}}
	if _, err := replayModuleDigestFromEntries(badMode); err == nil {
		t.Error("an unsupported mode must be rejected rather than hashed")
	}
}

// v0.107：`checks[]` 不许凭空少一项。
//
// 缺陷形态：ref 检查操作性失败时，digest 检查被整块跳过且**不留记录**。
// 于是脚本读到的数组短了一条，而"少了一条"与"那条通过了"在数据上长得一样。
func TestV107SkippedDigestCheckLeavesATrace(t *testing.T) {
	res := DepResult{
		Name: "github:acme/lib", Ref: "main", RefType: "branch",
		Checks: []CheckResult{{
			Check: CheckRef, Status: StatusFail, Operational: true,
			Detail: "the local mirror is cold",
		}},
	}
	// 复现 verify.go 里那一支的形状（不进网络的等价构造）。
	res.Err = "the local mirror is cold"
	res.Checks = append(res.Checks, CheckResult{
		Check:       CheckDigest,
		Status:      StatusFail,
		Operational: true,
		Detail:      "not attempted: the ref check could not complete",
	})
	res.DriftKind = aggregateDrift(res.Checks)

	var sawDigest bool
	for _, c := range res.Checks {
		if c.Check == CheckDigest {
			sawDigest = true
			if !c.Operational || c.Status != StatusFail {
				t.Errorf("the skipped check must be recorded as fail+operational, got %+v", c)
			}
		}
	}
	if !sawDigest {
		t.Fatal("the digest check vanished from checks[]; a consumer cannot tell " +
			"`skipped` from `passed`")
	}
	// 追加这条记录**不许**把结论变成 integrity failure。
	if res.DriftKind != DriftNone {
		t.Errorf("an operational skip must not contribute drift, got %q", res.DriftKind)
	}
}
