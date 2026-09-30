package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/git"
)

// TestV03CredentialDiscipline 是 v0.3 D 组的第二项验收：**凭证纪律的落地审计**。
//
// 它审计的是"运行之后留下的东西"：命令输出、ngm.lock、ngm.json、vendor 树，
// 以及用户态目录（mirror 的 .git/config、content store、cache）。
//
// 做法：让一个**可识别的假 token** 存在于环境里（配置把 `GITHUB_TOKEN` 当作
// github.com 的凭证来源），跑一遍会碰 git 与文件写入的完整流程，然后逐字节扫描。
//
// 为什么用可识别值而不是真 token：扫描要能断言"它一次都没出现"。
// 为什么扫描**运行产物**而不是源码：源码里的假 token 是测试数据（本文件就有一个），
// 而纪律说的是"ngm 不会把它写出去"。
func TestV03CredentialDiscipline(t *testing.T) {
	const token = "ghp_v03credentialaudit0000000000000000"
	t.Setenv("GITHUB_TOKEN", token)
	home := isolateUserEnv(t)
	_ = home

	scUpstream(t, "github:cred/audit", "export const c = 1\n", "")
	proj := newProject(t)

	steps := []struct {
		args []string
	}{
		{[]string{"add", "github:cred/audit@v1", "--ref-type=tag"}},
		{[]string{"install"}},
		{[]string{"verify"}},
		{[]string{"tree"}},
	}
	var output strings.Builder
	for _, step := range steps {
		args := append(append([]string{}, step.args...), "--dir="+proj)
		code, out := runCaptureCode(t, args...)
		output.WriteString(out)
		if code != 0 {
			t.Fatalf("%v exited %d:\n%s", args, code, out)
		}
	}

	// 1) 命令输出
	if msg := git.AssertNoSecrets(output.String(), []string{token}); msg != "" {
		t.Errorf("command output leaked the token: %s", msg)
	}

	// 2) 项目与被隔离的用户态目录里的一切文件
	roots := []string{proj}
	if ngmHome := os.Getenv("NGM_HOME"); ngmHome != "" {
		roots = append(roots, ngmHome)
	}
	scanned := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				// 读不了的文件（链接目标、权限）不是本次审计的对象
				return nil
			}
			scanned++
			if msg := git.AssertNoSecrets(string(data), []string{token}); msg != "" {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s leaked the token: %s", rel, msg)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
	}
	if scanned == 0 {
		t.Fatal("nothing was scanned: the audit would pass vacuously")
	}

	// 3) mirror 的 remote URL 不含凭证（token 只应作为环境变量透传给 git，
	//    绝不能被 ngm 写进任何配置）
	layout := defaultLayoutForTest(t)
	cfg := filepath.Join(layout.MirrorRoot(), "github.com", "cred", "audit.git", "config")
	if data, rerr := os.ReadFile(cfg); rerr == nil {
		if strings.Contains(string(data), token) {
			t.Errorf("the mirror config recorded the token:\n%s", data)
		}
	}
}
