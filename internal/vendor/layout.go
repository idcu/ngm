package vendor

import (
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/errs"
)

// Layout 描述 ngm 的用户态目录布局（默认位于 `~/.ngm/`）。
//
// 目录职责（architecture/vendor-layers.md）：
//
//	<home>/mirror/     层 1：Git 裸仓库镜像（<host>/<org>/<repo>.git）
//	<home>/content/    层 2：content-addressable store（sha256/<hex>/{tree,meta.json}）
//	<home>/cache/      层 4：缓存（metadata / osv / tmp）
//	<home>/config.json 全局配置
//	<home>/global-vendor/  mode=global 时的共享 vendor
type Layout struct {
	// Home 是 ngm 用户态根目录（默认 `~/.ngm`）。
	Home string
}

// DefaultLayout 返回基于 `~/.ngm` 的默认布局。
//
// 环境变量 NGM_HOME 可覆盖（便于测试与多环境隔离）；
// 未设置时使用 os.UserHomeDir()/.ngm。
func DefaultLayout() (Layout, error) {
	if h := os.Getenv("NGM_HOME"); h != "" {
		abs, err := filepath.Abs(h)
		if err != nil {
			return Layout{}, errs.Wrap(errs.CodeConfigInvalid, "resolve NGM_HOME", "", err)
		}
		return Layout{Home: abs}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return Layout{}, errs.Wrap(errs.CodeConfigInvalid,
			"cannot determine user home directory",
			"set NGM_HOME to an explicit directory", err)
	}
	return Layout{Home: filepath.Join(home, ".ngm")}, nil
}

// MirrorRoot 返回 mirror 层根目录（层 1）。
func (l Layout) MirrorRoot() string { return filepath.Join(l.Home, "mirror") }

// ContentRoot 返回 content store 根目录（层 2）。
func (l Layout) ContentRoot() string { return filepath.Join(l.Home, "content") }

// CacheRoot 返回缓存根目录（层 4）。
func (l Layout) CacheRoot() string { return filepath.Join(l.Home, "cache") }

// GlobalConfigPath 返回全局配置路径。
func (l Layout) GlobalConfigPath() string { return filepath.Join(l.Home, "config.json") }

// GlobalVendorRoot 返回 mode=global 时的共享 vendor 目录。
func (l Layout) GlobalVendorRoot() string { return filepath.Join(l.Home, "global-vendor") }

// EnsureDirs 创建布局所需的目录（mirror / content / cache）。
//
// 幂等；权限为 0755（父目录 0700 由 ~/.ngm 自身在必要时设置）。
func (l Layout) EnsureDirs() error {
	for _, d := range []string{l.Home, l.MirrorRoot(), l.ContentRoot(), l.CacheRoot()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return errs.Wrap(errs.CodeConfigInvalid, "create "+d, "check directory permissions", err)
		}
	}
	return nil
}
