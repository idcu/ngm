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
//
// v0.136：调用点 install.go / update.go 都是**单 goroutine 顺序循环**，此前的
// mutex + done channel 是为从未发生的并发准备的，且有两个真实缺陷：
//   - 等待方 `<-e.done` 不监听自己的 ctx.Done()；
//   - 若 git.BuildArchiveMaterial panic，close(e.done) 不执行 ⇒ 等待者永久阻塞。
//
// 现在退化为带 mutex 的普通 map：只做记忆化，不做跨 goroutine 协调。
// 若将来真有并发调用，应在这里显式加 ctx 监听与 defer close，而不是靠现在这套。
type materialCache struct {
	mu sync.Mutex
	m  map[string]*git.ArchiveMaterial
}

func newMaterialCache() *materialCache {
	return &materialCache{m: map[string]*git.ArchiveMaterial{}}
}

func (c *materialCache) get(ctx context.Context, opts git.Options, mirrorPath, commit string) (*git.ArchiveMaterial, error) {
	key := mirrorPath + "\x00" + commit
	c.mu.Lock()
	if m, ok := c.m[key]; ok {
		c.mu.Unlock()
		return m, nil
	}
	c.mu.Unlock()

	m, err := git.BuildArchiveMaterial(ctx, opts, mirrorPath, commit)
	if err != nil {
		return nil, err // 失败不缓存，便于上层重试
	}
	c.mu.Lock()
	c.m[key] = m
	c.mu.Unlock()
	return m, nil
}
