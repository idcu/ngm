package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/testutils"
)

// 把"测试声明的引擎权限"落进隔离 HOME 的全局配置。
//
// 为什么需要注入：`run:` 的默认档位是"需配置"（v0.3 D 组），而验收测试会在
// **每个子测试**里重新调用 isolateUserEnv（每次都换一个临时 HOME）。
// 注入保证每次新的 HOME 都带上本测试声明的权限，而不是逐个补写。
//
// 实现放在本包而不是 testutils：那里不能 import internal/config
// （testutils → config → resolve → git 会构成测试期的 import 环）。
func init() {
	testutils.WriteUserConfig = func(home string) error {
		engines := testutils.AllowedEngines()
		if len(engines) == 0 {
			return nil
		}

		seen := map[string]bool{}
		allow := make([]string, 0, len(engines))
		for _, e := range engines {
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			allow = append(allow, "run:"+e)
		}
		sort.Strings(allow)

		body, err := json.MarshalIndent(map[string]any{
			"permissions": map[string]any{"allow": allow},
		}, "", "  ")
		if err != nil {
			return err
		}

		path := config.GlobalPath(home)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, append(body, '\n'), 0o600)
	}
}
