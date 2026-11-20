package attest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestV70TimestampsAreOptIn —— A 批判据（竞品全维度评估 A2）。
//
// SLSA v1 的 runDetails.metadata 允许 startedOn/finishedOn，但 ngm 的核心承诺是
// "同一 lock ⇒ 同一字节"。这两者用 --with-timestamps（Options.WithTimestamps）和解：
// 默认省略时间字段（确定性），显式开启才写标准时间字段（放弃该产物的字节确定性）。
//
// 本判据守三条：
//  1. 默认路径字节里没有 startedOn/finishedOn（否则 TestV70Deterministic 会随墙钟红）；
//  2. 开启后两个字段都在、是 RFC3339、且等于注入的 Timestamp（可复现断言）；
//  3. invocationId 恒为 lock 的字节哈希（无论时间戳开关）——它不是随机运行 id。
func TestV70TimestampsAreOptIn(t *testing.T) {
	fixed := time.Date(2026, 11, 21, 9, 30, 0, 0, time.UTC)

	// 1. 默认：无时间字段、invocationId = lock 哈希。
	rawDefault, docDefault := buildAndParse(t, testEntries(), testOptions())
	for _, key := range []string{"startedOn", "finishedOn"} {
		if strings.Contains(rawDefault, `"`+key+`"`) {
			t.Errorf("默认路径不该出现 %q（会破坏字节确定性）", key)
		}
	}
	if docDefault.Predicate.RunDetails.Metadata["invocationId"] != testLockHex {
		t.Errorf("默认 invocationId = %v, want lock 哈希 %q",
			docDefault.Predicate.RunDetails.Metadata["invocationId"], testLockHex)
	}

	// 2. 开启：字段出现、RFC3339、等于注入时刻。
	opts := testOptions()
	opts.WithTimestamps = true
	opts.Timestamp = fixed
	rawOn, docOn := buildAndParse(t, testEntries(), opts)
	md := docOn.Predicate.RunDetails.Metadata
	want := fixed.UTC().Format(time.RFC3339)
	for _, key := range []string{"startedOn", "finishedOn"} {
		got, ok := md[key].(string)
		if !ok {
			t.Fatalf("开启后 %s 缺失或不是字符串: %v（raw=%s）", key, md[key], rawOn)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
		if _, err := time.Parse(time.RFC3339, got); err != nil {
			t.Errorf("%s 不是合法 RFC3339: %q: %v", key, got, err)
		}
	}
	// invocationId 不受时间戳开关影响。
	if md["invocationId"] != testLockHex {
		t.Errorf("开启时间戳后 invocationId 变了: %v", md["invocationId"])
	}

	// 3. 开启时同样注入时刻 ⇒ 同样字节（可复现；生产路径用 time.Now 才不保证）。
	a := opts
	b := opts
	ra, err := Build(testEntries(), a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := Build(testEntries(), b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ra) != string(rb) {
		t.Error("同一 Timestamp 两次导出字节不同")
	}

	// 4. 默认与开启的输出必须不同（否则 flag 是空转）。
	if rawDefault == rawOn {
		t.Error("--with-timestamps 没有改变输出——flag 是空转")
	}

	// 5. 输出仍是合法 JSON、字段顺序稳定（结构体的字段顺序即序列化顺序）。
	var check map[string]any
	if err := json.Unmarshal([]byte(rawOn), &check); err != nil {
		t.Fatalf("开启时间戳后输出不是合法 JSON: %v", err)
	}
}
