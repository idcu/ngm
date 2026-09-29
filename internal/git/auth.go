package git

import (
	"os"
	"sort"
	"strings"
)

// 认证纪律（architecture/security-model.md §"token 与凭证管理"）：
//
//	1. ngm 不主动读取凭证；认证交给 git 自己
//	   - SSH：ssh-agent + ~/.ssh/config
//	   - HTTPS：git credential helper（~/.git-credentials / keychain）
//	   - 环境变量：GITHUB_TOKEN 等原样透传给 git 子进程
//	2. token 不得出现在：命令行参数、ngm 输出、日志、lock、ngm.json、mirror config
//	3. 本文件只做两件事：
//	   - 从配置的 token 环境变量名解析出**值**，用于输出脱敏（不用于认证）
//	   - 提供断言助手，供测试与运行期守卫使用
//
// 注意：读取环境变量的值仅用于"把已知密文从输出中抹掉"，不注入任何请求。

// SecretsFromEnvVars 把 `{host: ENV_VAR_NAME}` 映射解析为待脱敏的字面量集合。
//
// 未设置的环境变量被忽略；返回值去重并排序（便于测试断言与日志稳定）。
func SecretsFromEnvVars(m map[string]string) []string {
	seen := map[string]bool{}
	var out []string
	// 稳定顺序：按 host 排序
	hosts := make([]string, 0, len(m))
	for h := range m {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	for _, h := range hosts {
		name := strings.TrimSpace(m[h])
		if name == "" {
			continue
		}
		val, ok := os.LookupEnv(name)
		if !ok || len(strings.TrimSpace(val)) == 0 {
			continue
		}
		if seen[val] {
			continue
		}
		seen[val] = true
		out = append(out, val)
	}
	return out
}

// WellKnownTokenEnvVars 是内置的常见 host → token 环境变量名映射。
//
// 与全局配置 `git.tokenEnvVars` 合并时**配置优先**（配置可覆盖或补充）。
// 内置项的目的：即使用户没写 config，也不会把已知 token 原样打印到日志。
func WellKnownTokenEnvVars() map[string]string {
	return map[string]string{
		"github.com": "GITHUB_TOKEN",
		"gitee.com":  "GITEE_TOKEN",
		"gitlab.com": "GITLAB_TOKEN",
		// 兼容常见变体
		"*": "NGM_TOKEN",
	}
}

// MergeTokenEnvVars 合并内置与用户配置（后者优先）。
func MergeTokenEnvVars(user map[string]string) map[string]string {
	out := WellKnownTokenEnvVars()
	for k, v := range user {
		out[k] = v
	}
	return out
}

// AssertNoSecrets 报告 s 中是否出现了任何 secret 字面量。
//
// 运行期守卫：在写入 lock / ngm.json / 打印用户可见输出前调用。
// 返回第一个命中的 secret 位置（脱敏形式）便于定位；未命中返回 ""。
func AssertNoSecrets(s string, secrets []string) string {
	for _, sec := range secrets {
		if sec == "" {
			continue
		}
		if strings.Contains(s, sec) {
			// 只回报"命中了哪一个 secret"，绝不回显 secret 本身
			return "detected a token literal in output (value redacted)"
		}
	}
	return ""
}
