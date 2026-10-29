// Package boundbuf 提供一个**有上界**的 io.Writer，用于捕获子进程输出。
//
// 存在理由（v0.107）：ngm 会运行**别人的代码**（sandbox 里的 postinstall、
// 外部引擎、wasm 模块），并把它们的 stdout/stderr 全量收进 `bytes.Buffer`。
// 一条 `while (true) console.log("x")` 就能让 `ngm install` / `ngm build`
// 被 OOM 打死——沙箱限制了网络、写入与派生，却没限制**内存**，
// 而内存是同一个沙箱边界内最容易打穿的一维。
//
// 三条纪律：
//
//  1. **只保留前 N 字节**，多出来的丢弃，绝不让它进内存。
//  2. `Write` 永远返回 `len(p), nil`：对子进程来说写入"成功"，
//     它不会因为 EPIPE 提前退出——我们要的是**有界的失败**，
//     不是让工具在跑到一半时被 SIGPIPE 打断。
//  3. 截断**必须说出来**：`Bytes()` 末尾追加一行标记。静默截断会让
//     "输出就这么多"与"我们只留了这么多"变得无法区分（v0.17 的同一课）。
package boundbuf

import (
	"bytes"
	"fmt"
	"sync"
)

// DefaultLimit 是单个通道（stdout / stderr）默认保留的字节数。
//
// 1 MiB 对诊断足够（真实报错通常几百字节到几 KB），而两个通道合计 2 MiB
// 让"几十个依赖同时失败"也不会把内存顶穿。
const DefaultLimit = 1 << 20

// Buffer 是一个有上界的写入缓冲。
//
// 满足 io.Writer；零值不可用（用 New）。
type Buffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated bool
	dropped   int64
}

// New 创建一个最多保留 limit 字节的缓冲区。limit <= 0 时用 DefaultLimit。
func New(limit int) *Buffer {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Buffer{limit: limit}
}

// Write 实现 io.Writer：超出的部分被丢弃并计数，但**永远报告写成功**。
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	room := b.limit - b.buf.Len()
	if room <= 0 {
		b.truncated = true
		b.dropped += int64(len(p))
		return len(p), nil
	}
	if len(p) > room {
		b.buf.Write(p[:room])
		b.truncated = true
		b.dropped += int64(len(p) - room)
		return len(p), nil
	}
	b.buf.Write(p)
	return len(p), nil
}

// Truncated 报告是否丢弃过字节。
func (b *Buffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}

// Dropped 返回被丢弃的字节数。
func (b *Buffer) Dropped() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// Bytes 返回保留的字节。发生截断时**末尾带一行显式标记**——
// 读这段文本的人（以及写判据的人）必须能看出输出不全。
func (b *Buffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.truncated {
		out := make([]byte, b.buf.Len())
		copy(out, b.buf.Bytes())
		return out
	}
	marker := fmt.Sprintf("\n[ngm: output truncated after %d bytes; %d more byte(s) were discarded]\n",
		b.limit, b.dropped)
	out := make([]byte, 0, b.buf.Len()+len(marker))
	out = append(out, b.buf.Bytes()...)
	out = append(out, marker...)
	return out
}

// String 是 Bytes 的便捷形式。
func (b *Buffer) String() string { return string(b.Bytes()) }
