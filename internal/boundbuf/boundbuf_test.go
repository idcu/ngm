package boundbuf

import (
	"bytes"
	"strings"
	"testing"
)

// v0.107：捕获别人的输出必须有上界。
//
// 缺陷形态：sandbox / 引擎子进程的 stdout、stderr 此前都是裸 `bytes.Buffer`。
// 沙箱约束了网络、写入、派生，却没有约束**内存**——一条
// `while (true) console.log("x")` 就能把 `ngm install` OOM 掉。
func TestBufferKeepsAtMostTheLimit(t *testing.T) {
	b := New(10)
	n, err := b.Write([]byte("0123456789ABCDEF"))
	if err != nil {
		t.Fatal(err)
	}
	// 对子进程来说写入**必须**"成功"：否则它会因 EPIPE 提前退出，
	// 而我们想要的是一次有界的失败，不是让工具跑到一半被打断。
	if n != 16 {
		t.Errorf("Write returned %d, want len(p) = 16 — a short write would make the child fail on a full pipe", n)
	}
	if got := string(b.Bytes()); !strings.HasPrefix(got, "0123456789") {
		t.Errorf("kept bytes = %q, want the first 10", got)
	}
	if !b.Truncated() {
		t.Error("Truncated() must be true after dropping bytes")
	}
	if b.Dropped() != 6 {
		t.Errorf("Dropped() = %d, want 6", b.Dropped())
	}
}

// 截断必须**说出来**：静默截断会让"输出就这么多"与"我们只留了这么多"无法区分。
func TestTruncationIsVisibleInTheBytes(t *testing.T) {
	b := New(4)
	if _, err := b.Write([]byte("abcdefgh")); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if !strings.Contains(got, "truncated") || !strings.Contains(got, "4 more") {
		t.Errorf("the captured text must say it was cut, got %q", got)
	}
}

func TestNoMarkerWhenNothingWasDropped(t *testing.T) {
	b := New(64)
	if _, err := b.Write([]byte("short")); err != nil {
		t.Fatal(err)
	}
	if b.Truncated() {
		t.Error("Truncated() must be false when everything fit")
	}
	if got := b.String(); got != "short" {
		t.Errorf("captured = %q, want it byte-identical (a marker here would corrupt normal output)", got)
	}
}

// 分多次写入也要守住上界（子进程的输出不会一次到齐）。
func TestLimitHoldsAcrossManyWrites(t *testing.T) {
	b := New(8)
	for i := 0; i < 100; i++ {
		if _, err := b.Write([]byte("1234567890")); err != nil {
			t.Fatal(err)
		}
	}
	if kept := len(b.Bytes()); kept > 8+200 {
		t.Errorf("kept %d bytes for a limit of 8 — the cap is not holding", kept)
	}
	if b.Dropped() != 1000-8 {
		t.Errorf("Dropped() = %d, want %d", b.Dropped(), 1000-8)
	}
}

// 零值不可用，但 New(0) 必须给一个合理默认值（调用方不该被迫写常量）。
func TestNewWithNonPositiveLimitUsesTheDefault(t *testing.T) {
	b := New(0)
	if _, err := b.Write(bytes.Repeat([]byte("x"), DefaultLimit+1)); err != nil {
		t.Fatal(err)
	}
	if !b.Truncated() {
		t.Error("DefaultLimit must bound the capture")
	}
}
