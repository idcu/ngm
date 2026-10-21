package main

import (
	"context"
	"encoding/json"

	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
)

// moduleProof 是一个模块条目的证明材料。
//
// v0.102 过渡期：install/update 同时写 archiveDigest 与这组字段；
// v0.103 起模块条目只保留 moduleDigest/subtreeOid/version。
type moduleProof struct {
	Digest     string
	SubtreeOid string
	Version    string
	// Material 是已算好的模块材料，供 content store 复用（v0.102）。
	Material *git.ArchiveMaterial
}

// buildModuleProofs 按 (mirror, commit) 分组，一次 git 调用算出一组模块的证明。
//
// 返回值以节点 Key（name#subPath）为键；根节点不在结果里。
func buildModuleProofs(ctx context.Context, env *projectEnv, nodes []*resolve.Node) (map[string]moduleProof, error) {
	type group struct {
		mirror string
		commit string
		subs   []string
		keys   map[string]string // subPath → node Key
	}
	groups := map[string]*group{}
	for _, n := range nodes {
		if n.SubPath == "" {
			continue
		}
		key := n.MirrorPath + "\x00" + n.Commit
		g := groups[key]
		if g == nil {
			g = &group{mirror: n.MirrorPath, commit: n.Commit, keys: map[string]string{}}
			groups[key] = g
		}
		g.subs = append(g.subs, n.SubPath)
		g.keys[n.SubPath] = n.Key
	}

	out := make(map[string]moduleProof, len(nodes))
	for _, g := range groups {
		mats, err := git.BuildModuleMaterials(ctx, env.GitOpts, g.mirror, g.commit, g.subs)
		if err != nil {
			return nil, err
		}
		for sub, m := range mats {
			out[g.keys[sub]] = moduleProof{
				Digest:     m.Digest,
				SubtreeOid: m.SubtreeOid,
				Version:    moduleVersionFromMaterial(m),
				Material:   m,
			}
		}
	}
	return out, nil
}

// moduleVersionFromMaterial 从模块 material 里读 `ngm.json` 的 version。
//
// 读不到、没有 version、或清单不是合法 JSON 都返回空串；这不是错误——
// 测试模块允许没有版本，正式发布由 `modules validate --release` 要求。
func moduleVersionFromMaterial(m *git.ArchiveMaterial) string {
	for i, e := range m.Entries {
		if e.Path != "ngm.json" || i >= len(m.Contents) {
			continue
		}
		var f struct {
			Version string `json:"version"`
		}
		if json.Unmarshal(m.Contents[i], &f) != nil {
			return ""
		}
		return f.Version
	}
	return ""
}
