package main

import (
	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/security"
)

// newPermissionPolicy 从全局配置构造权限策略（未配置时返回 nil，表示按默认档位判定）。
//
// 单点定义：`newProjectEnv` 与 `ngm config validate` 必须给出**同一份**判定。
// 两处各写一遍的话，迟早出现"validate 说没问题、实际却拒绝了"这种最难查的分歧。
func newPermissionPolicy(gp *config.GlobalPermissions) (*security.Policy, error) {
	if gp == nil {
		return nil, nil
	}
	return security.NewPolicy(gp.Allow, gp.Deny, globalConfigSource())
}
