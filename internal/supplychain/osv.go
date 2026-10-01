package supplychain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/idcu/ngm/internal/errs"
)

// OSV 查询与缓存。
//
// 设计约束（supply-chain.md §OSV.dev 集成）：
//   - 按 **lock 中的 commit** 查询，而不是按版本号——ngm 锁的是 commit
//   - 缓存到 `~/.ngm/cache/osv/<commit>.json`，默认 24 小时
//   - `--no-cache` 强制刷新；离线模式只读缓存
//
// 诚实边界：OSV.dev **不是万能的**——零日漏洞不在数据库中，多数公告按 semver 版本记录，
// 对 Git commit 的匹配覆盖率有限。因此"查不到"不等于"没漏洞"，报告必须把这句话带给用户
// （见 CoverageNote）。

// DefaultOSVURL 是 OSV.dev 的查询端点。
const DefaultOSVURL = "https://api.osv.dev/v1/query"

// CacheTTL 是缓存的有效期（24 小时）。
const CacheTTL = 24 * time.Hour

// CoverageNote 是必须出现在报告里的覆盖局限说明。
//
// 它不是一个可选项：不写这句，"审计通过"就会被读成"没有漏洞"，而实际含义只是
// "库里没有关于这个 commit 的记录"。这两者之间的差距，正是供应链工具最常制造的安全错觉。
const CoverageNote = "OSV.dev coverage is limited: zero-day issues are absent, and most advisories are " +
	"recorded against semver versions rather than Git commits. A clean result means no known entry for " +
	"this commit - not that the dependency is proven safe."

// Vuln 是一条漏洞记录（只保留 ngm 需要的字段）。
type Vuln struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Severity string `json:"severity"`
	FixedIn  string `json:"fixedIn,omitempty"`
}

// OSVConfig 控制一次查询。
type OSVConfig struct {
	// CacheDir 是缓存目录（通常 <NGM_HOME>/cache/osv）。为空表示不缓存。
	CacheDir string
	// BaseURL 覆盖查询端点；为空时用 DefaultOSVURL。测试把它指向 httptest 服务器——
	// 全项目纪律是"禁止测试依赖公网"。
	BaseURL string
	// Offline 为 true 时**不发起网络请求**：命中缓存即用（哪怕已过期），未命中即失败（exit 4）。
	Offline bool
	// NoCache 为 true 时忽略缓存并强制刷新。
	NoCache bool
	// Now 用于测试；为空时用 time.Now。
	Now func() time.Time

	// CheckNet 在**发起请求之前**判定 `net:<host>` 权限。
	//
	// 为什么放在这里而不是调用方（v0.5 C 组）：只有本函数知道最终要访问哪个主机
	// （BaseURL 可被覆盖，测试指向 httptest 时主机是 127.0.0.1）。放在调用方
	// 就会出现"判定的主机与实际访问的主机不是同一个"这种漏洞——
	// 而那正是门禁类改动最容易留下的形态。
	//
	// **只在实际要发请求时判定**：命中缓存与 `--offline` 都不碰网络，因此都不需要
	// `net:` 权限（与"本地 mirror 不算网络访问"同一条纪律）。
	//
	// nil 表示调用方没有可用的判定器：本函数不替它假设，直接放行。
	// cmd/ngm 永远传入真实策略（配置读不出来时也是更严的那一侧）。
	CheckNet func(host string) error
}

// cacheEnvelope 是缓存文件的结构。
//
// FetchedAt 写进文件而不是依赖 mtime：复制、备份、checkout 都会改动 mtime，
// 而"这份数据是什么时候取的"必须是数据自己的属性。
type cacheEnvelope struct {
	FetchedAt time.Time `json:"fetchedAt"`
	Vulns     []Vuln    `json:"vulns"`
}

// QueryOSV 查询某个 commit 的已知漏洞。
//
// 返回的错误已带退出码：网络失败且无缓存 → `CodeGitFetch`（exit 4）；
// 其余解析类失败 → `CodeConfigInvalid`（exit 3）。"查到几条"不是错误，由调用方裁决。
func QueryOSV(ctx context.Context, cfg OSVConfig, commit string) ([]Vuln, error) {
	now := time.Now()
	if cfg.Now != nil {
		now = cfg.Now()
	}

	if cfg.CacheDir != "" && !cfg.NoCache {
		if v, ok, cerr := readCache(cfg.CacheDir, commit, now); cerr != nil {
			return nil, cerr
		} else if ok {
			return v, nil
		}
	}

	if cfg.Offline {
		// 离线：允许使用**过期**缓存（有数据好过没数据），但没有就是失败
		if v, ok, cerr := readStaleCache(cfg.CacheDir, commit); cerr != nil {
			return nil, cerr
		} else if ok {
			return v, nil
		}
		return nil, errs.New(errs.CodeGitFetch,
			"no cached OSV result for "+commit+" and network access is disabled",
			"run `ngm audit` once with network access to populate the cache")
	}

	vulns, err := queryRemote(ctx, cfg, commit)
	if err != nil {
		return nil, err
	}
	if cfg.CacheDir != "" {
		if werr := writeCache(cfg.CacheDir, commit, cacheEnvelope{FetchedAt: now, Vulns: vulns}); werr != nil {
			return nil, werr
		}
	}
	return vulns, nil
}

// hostOf 取出端点 URL 的主机名，供 `net:` 判定使用。
//
// 取不到主机名时**不继续**：拿不到就意味着门禁无法判定，而此时放行等于跳过门禁。
// 端点写成 `https://`、忘了协议、或写成相对路径都会走到这里。
func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid, "parse the OSV endpoint "+raw, "", err)
	}
	host := u.Hostname()
	if host == "" {
		return "", errs.New(errs.CodeConfigInvalid,
			"the OSV endpoint has no host: "+raw,
			"set NGM_OSV_URL to a full URL such as "+DefaultOSVURL)
	}
	return host, nil
}

func queryRemote(ctx context.Context, cfg OSVConfig, commit string) ([]Vuln, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultOSVURL
	}

	// 门禁在**构造请求之前**（与 git 的 run:git 同一条纪律：拒绝时不该留下任何副作用，
	// 也不该让用户从"连不上"去猜"其实是我的配置不允许访问它"）。
	if cfg.CheckNet != nil {
		host, herr := hostOf(base)
		if herr != nil {
			return nil, herr
		}
		if err := cfg.CheckNet(host); err != nil {
			return nil, err
		}
	}

	body, err := json.Marshal(map[string]string{"commit": commit})
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "encode the OSV query", "", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, bytes.NewReader(body))
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "build the OSV request", "", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errs.Wrap(errs.CodeGitFetch,
			"query OSV.dev for "+commit,
			"check network access, or run again with an available cache", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		return nil, errs.New(errs.CodeGitFetch,
			fmt.Sprintf("OSV.dev returned HTTP %d for %s", res.StatusCode, commit),
			"retry later, or run again with an available cache")
	}

	var parsed struct {
		Vulns []struct {
			ID       string `json:"id"`
			Summary  string `json:"summary"`
			Severity string `json:"severity"`
			Affected []struct {
				Ranges []struct {
					Events []struct {
						Fixed string `json:"fixed"`
					} `json:"events"`
				} `json:"ranges"`
			} `json:"affected"`
		} `json:"vulns"`
	}
	if err := json.NewDecoder(res.Body).Decode(&parsed); err != nil {
		return nil, errs.Wrap(errs.CodeGitFetch,
			"decode the OSV response for "+commit,
			"OSV.dev may have changed its response shape; check for an ngm update", err)
	}

	out := make([]Vuln, 0, len(parsed.Vulns))
	for _, v := range parsed.Vulns {
		item := Vuln{ID: v.ID, Summary: v.Summary, Severity: normalizeSeverity(v.Severity)}
		// 修复版本藏在 affected[].ranges[].events[].fixed 里：取第一个非空值
		for _, a := range v.Affected {
			for _, r := range a.Ranges {
				for _, e := range r.Events {
					if e.Fixed != "" {
						item.FixedIn = e.Fixed
						break
					}
				}
				if item.FixedIn != "" {
					break
				}
			}
			if item.FixedIn != "" {
				break
			}
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// normalizeSeverity 统一严重级别的大小写（OSV 的 CVSS 与 database_specific 可能不一致）。
func normalizeSeverity(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "LOW", "MEDIUM", "MODERATE":
		if strings.EqualFold(s, "MODERATE") {
			return "MEDIUM"
		}
		return strings.ToUpper(s)
	case "HIGH", "CRITICAL":
		return strings.ToUpper(s)
	default:
		return "UNKNOWN"
	}
}

// IgnoreSeverities 按策略过滤掉被忽略的级别；返回保留项与被忽略的条数。
//
// 被忽略的条数要报出来：否则"忽略 LOW"看起来像是"没有 LOW"，用户会低估噪声之外的实际情况。
func IgnoreSeverities(vulns []Vuln, ignored []string) (kept []Vuln, ignoredCount int) {
	drop := make(map[string]bool, len(ignored))
	for _, s := range ignored {
		if s != "" {
			drop[strings.ToUpper(s)] = true
		}
	}
	kept = make([]Vuln, 0, len(vulns))
	for _, v := range vulns {
		if drop[strings.ToUpper(v.Severity)] {
			ignoredCount++
			continue
		}
		kept = append(kept, v)
	}
	return kept, ignoredCount
}

func cachePath(dir, commit string) string {
	return filepath.Join(dir, commit+".json")
}

func readCache(dir, commit string, now time.Time) ([]Vuln, bool, error) {
	if dir == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(cachePath(dir, commit))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, errs.Wrap(errs.CodeConfigInvalid, "read the OSV cache", "", err)
	}
	var env cacheEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		// 缓存损坏不该让命令失败——按未命中处理，下次会重新拉取
		return nil, false, nil
	}
	if now.Sub(env.FetchedAt) > CacheTTL {
		return nil, false, nil
	}
	return env.Vulns, true, nil
}

func readStaleCache(dir, commit string) ([]Vuln, bool, error) {
	if dir == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(cachePath(dir, commit))
	if err != nil {
		return nil, false, nil
	}
	var env cacheEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, nil
	}
	return env.Vulns, true, nil
}

func writeCache(dir, commit string, env cacheEnvelope) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "create the OSV cache dir", "check permissions on the ngm home directory", err)
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "encode the OSV cache entry", "", err)
	}
	if err := os.WriteFile(cachePath(dir, commit), append(data, '\n'), 0o644); err != nil {
		return errs.Wrap(errs.CodeConfigInvalid, "write the OSV cache", "check permissions on the ngm home directory", err)
	}
	return nil
}
