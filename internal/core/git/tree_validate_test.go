package git

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
)

// TestMaterialFromEntriesRefusesUnencodablePath 钉住 v0.136 的安装期校验：
// Git 允许文件名含反斜杠 / 换行，但 digest 清单用 NUL + POSIX 路径编码，
// 这类记录 install 期必须 exit 3，而不是写进 lock 后由 verify 判 critical(2)。
//
// 直接喂 crafted TreeEntry（同一包内），跨平台——Windows 文件系统无法创建
// 字面量反斜杠文件名，但 Git tree 里可以有。
func TestMaterialFromEntriesRefusesUnencodablePath(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cases := []struct {
		name string
		path string
	}{
		{"backslash", "dir\\file.ts"},
		{"newline", "dir\nfile.ts"},
		{"carriage return", "dir\rfile.ts"},
		{"dot dot", "dir/../file.ts"},
		{"absolute", "/etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := []TreeEntry{{Mode: digest.ModeRegular, Type: "blob", SHA: sha, Path: tc.path}}
			_, err := materialFromEntries(context.Background(), Options{}, t.TempDir(),
				"1111111111111111111111111111111111111111", entries, digest.ManifestVersion, false)
			if err == nil {
				t.Fatal("an unencodable path must be refused at install time")
			}
			if errs.ExitCode(err) != 3 {
				t.Errorf("must be a config-class error (exit 3), got %d: %v", errs.ExitCode(err), err)
			}
			if !strings.Contains(err.Error(), "unsupported path") {
				t.Errorf("error should point at the path: %v", err)
			}
		})
	}
}
