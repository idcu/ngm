package adapter

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/errs"
)

// ADR-013 的结论是"不发布 remote adapter"。这条决定**以措辞的形式**交付给用户：
// 报错必须说明"这是决定"，而不是继续暗示"将来会有"——后者是在承诺一件不会发生的事。
//
// 因此这里不只是断言"报错了"，而是断言**没有承诺**。
func TestRemoteAdapterIsExcludedByDecision(t *testing.T) {
	entry := Entry{
		Name:    "corp-builder",
		Kind:    KindBundle,
		Adapter: AdapterRemote,
		Command: "https://build.example.internal",
	}

	_, err := NewEngine(entry, t.TempDir())
	if err == nil {
		t.Fatal("a remote engine must not construct: there is nothing to run")
	}
	shown := errs.FormatHuman(err)

	if !strings.Contains(shown, "excluded by decision") {
		t.Errorf("the message must say this is a decision, not a gap:\n%s", shown)
	}
	if !strings.Contains(shown, "adr-013") {
		t.Errorf("it must point at the argument:\n%s", shown)
	}
	// 这一条是本测试的重点：没有"将来会有"的暗示。
	for _, promise := range []string{"planned", "coming soon", "not implemented", "尚未实现", "待实现"} {
		if strings.Contains(strings.ToLower(shown), promise) {
			t.Errorf("no promise may be implied for a decision that was made (%q):\n%s", promise, shown)
		}
	}
}

// 清单校验对 remote 条目给出同样的判断（它也是用户会先看到的地方）。
func TestCatalogValidate_RemoteIsExcludedNotPending(t *testing.T) {
	cat := &Catalog{Version: CatalogVersion, Engines: []Entry{
		{Name: "corp-builder", Kind: KindBundle, Adapter: AdapterRemote, Command: "https://build.example.internal"},
	}}
	cat.normalize()

	var found bool
	for _, is := range cat.Validate() {
		if is.Entry == "corp-builder/bundle" && is.Kind == IssueUnimplemented {
			found = true
			if !strings.Contains(is.Message, "excluded by decision") {
				t.Errorf("validate must report the decision, not a pending feature: %s", is.Message)
			}
		}
	}
	if !found {
		t.Error("a remote entry must still be reported (it is a real entry that cannot run)")
	}
}

// 枚举里保留 `remote` 是有意的：删掉它会让"为什么没有 remote"无处可查。
// 若将来有人"清理"掉它，这条会红——那时应当先修订 ADR-013，而不是改这里的期望。
func TestAdapterEnumerationKeepsRemote(t *testing.T) {
	if !AdapterRemote.IsValid() {
		t.Error("`remote` must stay recognised in the catalog grammar (ADR-013); it is excluded, not removed")
	}
	if !strings.Contains(adapterList(), string(AdapterRemote)) {
		t.Errorf("the adapter list shown to users must include remote with its explanation: %s", adapterList())
	}
}
