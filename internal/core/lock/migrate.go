// v2 → v3 lock 迁移（评估报告 §6 P1、ADR-055）。
//
// 立场：**只搬能证明的字段**。2.x lock 的 git 来源条目在 v3 里有确定的字段对应
// （commit / digest / vendorPath / requiredBy 原样保留），但一旦条目形状不合法、
// 少了内容锚点、或带了 v3-only 的本地来源字段，就无法证明它该变成什么——
// 那种条目一律 skipped，由调用方汇总并要求人工重装，绝不猜。
package lock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/resolve"
)

// MigrateSkip 是迁移过程中**无法安全映射**的一个条目。
//
// Reason 给人读，Fix 是可操作的下一步；二者的区分与 errs.NgmError 一致。
type MigrateSkip struct {
	Index  int    `json:"index"`
	Entry  string `json:"entry,omitempty"`
	Reason string `json:"reason"`
	Fix    string `json:"fix"`
}

// MigrateOutcome 是 MigrateV2 的结果。
//
// AlreadyCurrent 为真表示输入已是 v3（不需要迁移）；Skipped 非空表示有条目
// 无法安全映射——调用方**必须**在写文件之前检查它（本函数不写任何文件）。
type MigrateOutcome struct {
	AlreadyCurrent      bool
	FromLockfileVersion string
	File                *File
	Skipped             []MigrateSkip
}

// v2TopFields / v2DependencyFields 是允许原样映射的字段全集。
//
// 任何**不在集合里**的键都当作"来源语义无法证明"而 skip：未知字段可能是新版本
// 的语义，静默丢掉它等于替用户做决定（ADR-055）。
var v2TopFields = map[string]bool{
	"version": true, "lockfileVersion": true, "dependencies": true,
}

var v2DependencyFields = map[string]bool{
	"name": true, "ref": true, "refType": true, "commit": true,
	"archiveDigest": true, "moduleDigest": true, "subtreeOid": true,
	"version": true, "resolvedAt": true, "vendorPath": true,
	"subPath": true, "requiredBy": true,
	// 以下是 v3-only 的本地来源字段：出现在 2.x lock 里属于语义变化。
	"sourcePath": true, "localRepo": true, "dirDigest": true,
}

// v2DependencyRaw 是 2.x 条目的宽松解码形状。
//
// 字段类型刻意宽松（string 而不是强类型）：JSON 里写成数字的 refType 也在这里
// 解码成功，然后在映射阶段被拒——错误信息要说"哪个条目的哪个字段形状不对"，
// 而不是一个笼统的 "cannot unmarshal"。
type v2DependencyRaw struct {
	Name          string   `json:"name"`
	Ref           string   `json:"ref"`
	RefType       string   `json:"refType"`
	Commit        string   `json:"commit"`
	ArchiveDigest string   `json:"archiveDigest"`
	ModuleDigest  string   `json:"moduleDigest"`
	SubtreeOid    string   `json:"subtreeOid"`
	Version       string   `json:"version"`
	ResolvedAt    string   `json:"resolvedAt"`
	VendorPath    string   `json:"vendorPath"`
	SubPath       string   `json:"subPath"`
	RequiredBy    []string `json:"requiredBy"`
	SourcePath    string   `json:"sourcePath"`
	LocalRepo     string   `json:"localRepo"`
	DirDigest     string   `json:"dirDigest"`
}

// MigrateV2 把一份 2.x lock 的字节映射成 v3 lock。
//
// 它**不读文件、不写文件**：调用方负责 IO、备份与 dry-run。返回的错误只用于
// "整份文件都无法处理"的情形；单个条目的无法映射走 outcome.Skipped。
func MigrateV2(data []byte, path string) (*MigrateOutcome, error) {
	var top map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&top); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse "+path+" for migration (is it valid JSON?)",
			"fix the JSON syntax in "+path+", then run `ngm migrate` again", err)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("unexpected trailing JSON value after the lock object")
		}
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse "+path+" for migration (trailing content after the lock object)",
			"remove the extra JSON value, then run `ngm migrate` again", err)
	}

	from := rawString(top["lockfileVersion"])
	major := majorVersion(from)
	if major == majorVersion(FileVersion) {
		// 已是 v3。版本号相同但内容非法的文件仍由 Read/Validate 在别处负责；
		// migrate 的职责到"版本已是 v3"为止，幂等退出。
		return &MigrateOutcome{AlreadyCurrent: true, FromLockfileVersion: from}, nil
	}
	if major != 2 {
		return nil, errs.New(errs.CodeConfigInvalid,
			"migrate supports lockfileVersion 2.x only (got "+from+")",
			"this command maps 2.x → "+FileVersion+"; newer or older locks need a matching ngm build")
	}
	if rawInt(top["version"]) != 0 && rawInt(top["version"]) != SchemaVersion {
		return nil, errs.New(errs.CodeConfigInvalid,
			"unsupported lock `version` "+strconv.Itoa(rawInt(top["version"])),
			"migrate expects the lock schema `version` to be "+strconv.Itoa(SchemaVersion))
	}
	for k := range top {
		if !v2TopFields[k] {
			return nil, errs.New(errs.CodeConfigInvalid,
				"unknown top-level field "+k+" in "+path,
				"remove the field or regenerate the lock; migrate does not guess unknown field semantics")
		}
	}

	rawDeps, ok := top["dependencies"]
	if !ok {
		return nil, errs.New(errs.CodeConfigInvalid,
			path+" is missing `dependencies`",
			"a lock must list its dependencies; regenerate it with `ngm install`")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(rawDeps, &entries); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse dependencies in "+path, "", err)
	}

	out := NewFile()
	out.LockfileVersion = FileVersion
	outcome := &MigrateOutcome{FromLockfileVersion: from, File: out}
	for i, fields := range entries {
		dep, reason := mapV2Dependency(i, fields)
		if reason != nil {
			outcome.Skipped = append(outcome.Skipped, *reason)
			continue
		}
		out.Dependencies = append(out.Dependencies, dep)
	}
	out.Sort()
	return outcome, nil
}

// mapV2Dependency 映射一个 2.x 条目。
//
// 返回 (dep, nil) 表示可安全映射；返回 (zero, skip) 表示该条目必须人工处理。
func mapV2Dependency(index int, fields map[string]json.RawMessage) (Dependency, *MigrateSkip) {
	label := "dependencies[" + strconv.Itoa(index) + "]"
	skip := func(entry, reason string) *MigrateSkip {
		return &MigrateSkip{
			Index:  index,
			Entry:  entry,
			Reason: reason,
			Fix:    "re-install this dependency (or regenerate the lock) so ngm can record a full v3 proof",
		}
	}

	for k := range fields {
		if !v2DependencyFields[k] {
			return Dependency{}, skip("", label+" contains unknown field "+k+
				"; its v3 meaning cannot be proven")
		}
	}
	var raw v2DependencyRaw
	if b, err := json.Marshal(fields); err != nil {
		return Dependency{}, skip("", label+" cannot be re-encoded: "+err.Error())
	} else if err := json.Unmarshal(b, &raw); err != nil {
		return Dependency{}, skip("", label+" has a field of the wrong type: "+err.Error())
	}

	entry := raw.Name
	if raw.SubPath != "" {
		entry += "#" + raw.SubPath
	}
	reject := func(reason string) (Dependency, *MigrateSkip) {
		return Dependency{}, skip(entry, reason)
	}

	// 本地来源是 v3 才有的语义：2.x lock 里出现它，等于来源含义发生了变化。
	for field, val := range map[string]string{
		"sourcePath": raw.SourcePath, "localRepo": raw.LocalRepo, "dirDigest": raw.DirDigest,
	} {
		if val != "" {
			return reject(label + " (" + entry + ") carries v3-only field " + field +
				"; a 2.x lock cannot prove its local-source semantics")
		}
	}
	if raw.Name == "" || raw.Ref == "" || raw.RefType == "" {
		return reject(label + " (" + entry + ") is missing name/ref/refType")
	}
	if rt := resolve.RefType(raw.RefType); !rt.IsValid() || rt.IsLocal() {
		return reject(label + " (" + entry + ") has refType " + raw.RefType +
			", which is not a git ref type that a 2.x lock can prove")
	}
	if !isFullCommit(raw.Commit) {
		return reject(label + " (" + entry + ") has no full 40-hex commit; " +
			"an immutable git anchor cannot be recovered from a partial hash")
	}
	if raw.VendorPath == "" || !IsSafeVendorPath(raw.VendorPath) {
		return reject(label + " (" + entry + ") has a missing or unsafe vendorPath")
	}
	if raw.SubPath != "" && !IsSafeRelPath(raw.SubPath) {
		return reject(label + " (" + entry + ") has an unsafe subPath")
	}

	dep := Dependency{
		Name:       raw.Name,
		Ref:        raw.Ref,
		RefType:    raw.RefType,
		Commit:     raw.Commit,
		Version:    raw.Version,
		ResolvedAt: raw.ResolvedAt,
		VendorPath: raw.VendorPath,
		SubPath:    raw.SubPath,
		RequiredBy: append([]string(nil), raw.RequiredBy...),
	}
	if raw.SubPath == "" {
		// 根条目：只认完整 archiveDigest；模块字段出现即为 v2 过渡态的不合法形状。
		if raw.ModuleDigest != "" || raw.SubtreeOid != "" {
			return reject(label + " (" + entry + ") is a root entry that also carries moduleDigest/subtreeOid")
		}
		if !IsArchiveDigest(raw.ArchiveDigest) {
			return reject(label + " (" + entry + ") has no full sha256 archiveDigest; " +
				"content proof must be recomputed by re-installing")
		}
		dep.ArchiveDigest = raw.ArchiveDigest
	} else {
		// 模块条目：只认 moduleDigest + subtreeOid；archiveDigest 是已废止的过渡态。
		if raw.ArchiveDigest != "" {
			return reject(label + " (" + entry + ") is a module entry that also carries archiveDigest " +
				"(the retired v2 transition shape); its content proof is ambiguous")
		}
		if !strings.HasPrefix(raw.ModuleDigest, digest.Algorithm+":") ||
			len(raw.ModuleDigest) != len(digest.Algorithm)+1+64 || !isHex(raw.ModuleDigest[len(digest.Algorithm)+1:]) {
			return reject(label + " (" + entry + ") has no full sha256 moduleDigest; " +
				"module content proof must be recomputed by re-installing")
		}
		if !isSubtreeOid(raw.SubtreeOid) {
			return reject(label + " (" + entry + ") has no valid subtreeOid; " +
				"the module binding anchor must be recomputed by re-installing")
		}
		dep.ModuleDigest = raw.ModuleDigest
		dep.SubtreeOid = raw.SubtreeOid
	}
	return dep, nil
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

func rawString(b json.RawMessage) string {
	var s string
	if len(b) == 0 || json.Unmarshal(b, &s) != nil {
		return ""
	}
	return s
}

func rawInt(b json.RawMessage) int {
	var n int
	if len(b) == 0 || json.Unmarshal(b, &n) != nil {
		return 0
	}
	return n
}
