package resolve

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
)

// LocalNamespace 是 ADR-052 为本地来源保留的 name 命名空间。
// 它与 Git host 的短名空间不重叠：Git host 必须有至少一个点或属于 loopback，
// 因此 `local:<name>` 永远不会被 ParseSlug 接受——这正是本文件存在的理由。
const LocalNamespace = "local:"

// IsLocalName 判断一个 lock/清单里的 name 是否声明为本地来源。
// 只看形状，不校验后缀；需要构造身份时用 LocalCanonical。
func IsLocalName(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), LocalNamespace)
}

// LocalCanonical 把 `local:<name>` 转成与 resolveLocalOne 同口径的伪 Canonical。
//
// local-dir 没有 mirror，local-git 有本地仓，两者都不该走 ParseSlug/Normalize
// 的 host 校验；但伪 Canonical 让下游的 Slug()、Equal()、Target 匹配继续可用。
// 后缀规则与 localfs.RelName 一致（此处不 import localfs，避免 layering 反向依赖）。
func LocalCanonical(name string) (Canonical, error) {
	raw := strings.TrimSpace(name)
	if !strings.HasPrefix(raw, LocalNamespace) {
		return Canonical{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("%q is not a local dependency name", name),
			"a local dependency name must start with `local:`")
	}
	n := strings.TrimPrefix(raw, LocalNamespace)
	if n == "" || n == "." || n == ".." ||
		strings.ContainsAny(n, `/\`) || strings.ContainsAny(n, "\x00\r\n") {
		return Canonical{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("invalid local dependency name %q", name),
			"use `local:<name>` where <name> is a single safe path segment")
	}
	return Canonical{Host: "local", Path: n}, nil
}

// ParseIdentityArg 解析 CLI 传入的身份（可能来自 `local:` 命名空间）。
// 与 DepIdentity 的区别：CLI 参数没有 refType 字段，只能靠名字前缀判定。
func ParseIdentityArg(name string) (Canonical, error) {
	if IsLocalName(name) {
		return LocalCanonical(name)
	}
	return ParseSlug(name)
}

// DepIdentity 是把 lock/清单条目转成身份的唯一入口。
//
// 使用规则（v0.103 回归教训）：凡是要对 lock.Dependency.Name 做身份解析的
// 消费点（gc / sbom / attest / verify 按名筛选 / why / remove / config 去重），
// 都必须调本函数，不得裸调 ParseSlug——否则 local 条目会在解析处直接 exit 3。
// 注意 refType 是权威信号：即便 name 被手工改成非 local: 形状，local 来源也
// 不能滑进 Git 身份通道。
func DepIdentity(name string, refType RefType) (Canonical, error) {
	if refType.IsLocal() || IsLocalName(name) {
		return LocalCanonical(name)
	}
	return ParseSlug(name)
}
