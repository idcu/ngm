package resolve

// RefType 是依赖引用的 ref 类型。
//
// 定义在本包（而非 config）是为了让依赖方向保持单向：
// config 依赖 resolve 做 URL / 名称校验，resolve 不反向依赖 config。
// config.RefType 是它的类型别名，两者可互换使用。
//
// 声明中 refType 必填——`@main` 无法区分 tag 叫 main 还是 branch 叫 main
// （见 architecture/dependency-resolution.md 与 guides/configuration.md）。
type RefType string

const (
	// RefTypeCommit：`ref` 是 commit hash（通常 40 位完整 hash）。
	RefTypeCommit RefType = "commit"
	// RefTypeTag：`ref` 是 tag 名（annotated tag 会被解引用到 commit）。
	RefTypeTag RefType = "tag"
	// RefTypeBranch：`ref` 是分支名。
	RefTypeBranch RefType = "branch"
	// RefTypeLocalGit：`ref` 是本地 Git 仓库里的 tag/branch/commit（必须显式给出）。
	// sourcePath 指向本地 git 仓根；证明链与 git 来源完全一致（commit + tree digest）。
	RefTypeLocalGit RefType = "local-git"
	// RefTypeLocalDir：`ref` 是 dirDigest（sha256:<hex>），sourcePath 指向任意本地目录。
	// 锚点是目录内容摘要，不走 git（见 internal/core/localfs，ADR-052）。
	RefTypeLocalDir RefType = "local-dir"
)

// IsLocal 报告 r 是否为本地来源（local-git / local-dir）。
//
// 消费方（graph/vendor/verify）用这一处判定做路径分流，避免各处散写字符串比较。
func (r RefType) IsLocal() bool {
	return r == RefTypeLocalGit || r == RefTypeLocalDir
}

// ValidRefTypes 返回全部合法 refType，顺序稳定（便于错误提示与测试）。
func ValidRefTypes() []RefType {
	return []RefType{RefTypeCommit, RefTypeTag, RefTypeBranch, RefTypeLocalGit, RefTypeLocalDir}
}

// IsValid 报告 r 是否为受支持的 refType。
func (r RefType) IsValid() bool {
	for _, v := range ValidRefTypes() {
		if r == v {
			return true
		}
	}
	return false
}

// String 实现 fmt.Stringer。
func (r RefType) String() string { return string(r) }
