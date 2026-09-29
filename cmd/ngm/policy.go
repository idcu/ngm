package main

import (
	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/supplychain"
)

// policyChecker 把项目声明的供应链策略编译成一个判定函数，供依赖图解析注入（见 ADR-009）。
//
// 返回 nil 表示**未配置策略**（不门禁）——把"没配策略"当成"全部拒绝"会让所有既有项目
// 突然装不上，那不是安全，那是事故。
//
// 放在 CLI 层而不是 resolve / supplychain：策略的**来源**是项目的 ngm.json，
// 而 resolve 不能依赖 config（config 依赖 resolve 做校验），
// 所以由这里把两者接起来。
func policyChecker(pf *config.ProjectFile) (func(host, repoPath string) error, error) {
	if pf == nil {
		return nil, nil
	}
	p, err := supplychain.FromConfig(pf.SupplyChain)
	if err != nil {
		return nil, err
	}
	if p.IsEmpty() {
		return nil, nil
	}
	return p.CheckRepo, nil
}
