package config

import (
	"strings"
	"testing"
)

// --- v0.105：两条"清单看着合法、工具自己说法不一"的回归判据 ---

const v105FullCommit = "0123456789abcdef0123456789abcdef01234567"

// 窄默认（ADR-035）必须**写回**解析结果。
//
// 缺陷形态（v0.105 实测）：`Validate` 在 `for _, d := range` 的**副本**上补
// `RefType = commit`，于是 `ngm config validate` 打印 `ngm.json OK`（exit 0），
// 而 `ngm tree` / `ngm install` 走 `toDepSpecs` 拿到空 refType，报
// `ConfigInvalid: invalid refType ""`（exit 3）。同一份文件两个结论，
// 而 CI 只会跑到前者。
//
// 判据刻意走 `decodeProject`（生产入口）而不是直接调 `d.Validate()`：
// 后者一直是绿的，正因为它测的是那个副本。
func TestV105NarrowRefTypeDefaultSurvivesDecode(t *testing.T) {
	src := `{"schemaVersion":1,"name":"github.com:acme/app","version":"0.1.0","runtime":"node",` +
		`"dependencies":[{"name":"github:acme/lib","ref":"` + v105FullCommit + `"}]}`

	p, err := decodeProject(strings.NewReader(src), "ngm.json")
	if err != nil {
		t.Fatalf("decodeProject: %v", err)
	}
	if len(p.Dependencies) != 1 {
		t.Fatalf("deps = %d, want 1", len(p.Dependencies))
	}
	if got := p.Dependencies[0].RefType; got != RefTypeCommit {
		t.Errorf("Dependencies[0].RefType = %q, want %q — "+
			"the narrow default is computed on a copy and never written back", got, RefTypeCommit)
	}
}

// 同一个洞的另一半：短 hash 仍然必须被拒（窄默认不得被顺手放宽）。
func TestV105NarrowRefTypeStaysNarrow(t *testing.T) {
	src := `{"schemaVersion":1,"name":"github.com:acme/app","version":"0.1.0","runtime":"node",` +
		`"dependencies":[{"name":"github:acme/lib","ref":"0123456"}]}`
	if _, err := decodeProject(strings.NewReader(src), "ngm.json"); err == nil {
		t.Fatal("a 7-char ref without refType must still be rejected (short hash is ambiguous with a tag)")
	}
}

// UTF-8 BOM 必须被容忍。
//
// 缺陷形态（v0.105 实测）：PowerShell 5.1 的 `Set-Content -Encoding utf8` 与
// Notepad 的"UTF-8"默认都写 BOM，而 encoding/json 在它上面报
// `invalid character '\ufeff'`——文件肉眼完全正确，工具说不合法。
// ngm 的第一顺位用户恰好在 Windows 上。
func TestV105Utf8BomIsTolerated(t *testing.T) {
	const body = `{"schemaVersion":1,"name":"github.com:acme/app","version":"0.1.0","runtime":"node"}`

	p, err := decodeProject(strings.NewReader("\ufeff"+body), "ngm.json")
	if err != nil {
		t.Fatalf("a plan file written by PowerShell/Notepad must be readable: %v", err)
	}
	if p.Name != "github.com:acme/app" {
		t.Errorf("name = %q", p.Name)
	}

	// 全局配置同一套解码，同一个缺陷。
	g, err := decodeGlobal(strings.NewReader("\ufeff"+`{"git":{"defaultProtocol":"ssh"}}`), "config.json")
	if err != nil {
		t.Fatalf("global config with BOM: %v", err)
	}
	if g.Git == nil || g.Git.DefaultProtocol != "ssh" {
		t.Errorf("git config not decoded: %+v", g.Git)
	}
}

// BOM 只在**开头**有意义：出现在字段中间的 U+FEFF 仍然是非法 JSON，
// 不许因为"容忍 BOM"就把整份解码器放宽。
func TestV105BomToleranceDoesNotWeakenParsing(t *testing.T) {
	if _, err := decodeProject(strings.NewReader(`{"name":"\ufeffx"}`), "ngm.json"); err == nil {
		t.Fatal("U+FEFF inside a string is legal but this manifest is invalid for other reasons; " +
			"more importantly the decoder must not silently strip it everywhere")
	}
}
