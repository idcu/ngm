package main

import (
	"context"
	"sync"

	"github.com/idcu/ngm/internal/core/git"
)

// materialCache 让同一 (mirror, commit) 的整树 material/digest 只算一次。
//
// 一个集合仓库的多个模块通常指向同一个 commit；install/update 的循环此前会为
// 每个模块各跑一次 `ls-tree -r` + `cat-file`。失败不缓存，便于上层重试。
type materialCache struct {
	mu sync.Mutex
	m  map[string]*materialEntry
}

type materialEntry struct {
	done     chan struct{}
	material *git.ArchiveMaterial
	err      error
}

func newMaterialCache() *materialCache {
	return &materialCache{m: map[string]*materialEntry{}}
}

func (c *materialCache) get(ctx context.Context, opts git.Options, mirrorPath, commit string) (*git.ArchiveMaterial, error) {
	key := mirrorPath + "\x00" + commit
	c.mu.Lock()
	if e, ok := c.m[key]; ok {
		c.mu.Unlock()
		<-e.done
		return e.material, e.err
	}
	e := &materialEntry{done: make(chan struct{})}
	c.m[key] = e
	c.mu.Unlock()

	e.material, e.err = git.BuildArchiveMaterial(ctx, opts, mirrorPath, commit)
	close(e.done)
	if e.err != nil {
		c.mu.Lock()
		delete(c.m, key)
		c.mu.Unlock()
	}
	return e.material, e.err
}
