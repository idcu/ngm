package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// v0.88：`scripts/*.ps1` 必须**能被 PowerShell 解析**。
//
// 为什么值得一条网：仓库里的 `.ps1` 是**交付物的一部分**（Windows 用户的入口，
// 按纪律只转调 `.sh`，见各文件头部）。而本机这批 `.ps1` 都是 UTF-8 且**没有 BOM**，
// Windows PowerShell 5.1 会按 ANSI 解码：中文注释变乱码（无害），但一旦某个字节对
// 吞掉一个引号，整个脚本**直接不解析**——而它坏得很安静（没人跑就没人知道）。
//
// 这不是假想的：v0.88 写这条网时，手上一个临时 `.ps1` 就正因如此报了
// `The string is missing the terminator: "` ——同一台机器、同一个原因。
func TestV88PowerShellScriptsParse(t *testing.T) {
	ps := findPowerShell(t)
	if ps == "" {
		t.Skip("no powershell/pwsh on this machine — the .ps1 scripts are not parsed here")
	}

	scripts, err := filepath.Glob(filepath.Join(repoRoot(t), "scripts", "*.ps1"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("no .ps1 under scripts/ (err=%v) —— 路径变了？这条网会静默地什么都不守", err)
	}

	for _, s := range scripts {
		name := filepath.Base(s)
		t.Run(name, func(t *testing.T) {
			// ① 含非 ASCII 的 `.ps1` **必须带 UTF-8 BOM**。
			//
			// 为什么：PowerShell 5.1 无 BOM 时按 ANSI 解码 UTF-8 文本，而中文在 GBK 下
			// 是双字节——一个前导字节会**把后面的换行一起吃掉**（成对消费），于是代码被
			// 并成一行、直接解析失败。实测：`check-library-standard.ps1` 与
			// `packagegate-demo.ps1` 在 v0.88 就是这样坏的（按 UTF-8 解码 0 错，
			// 按 ANSI 3 错）。没有 BOM 就只剩"字节配对的运气"，而那东西会变。
			raw, err := os.ReadFile(s)
			if err != nil {
				t.Fatalf("读不出 %s：%v", name, err)
			}
			nonASCII := false
			for _, b := range raw {
				if b >= 0x80 {
					nonASCII = true
					break
				}
			}
			hasBOM := len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF
			if nonASCII && !hasBOM {
				t.Errorf("%s 含非 ASCII 却没有 UTF-8 BOM —— PowerShell 5.1 会按 ANSI 解码它（换行可能被吞、脚本直接不解析）", name)
			}

			// ② 用 PowerShell 自己的解析器：CI 与用户真正会遇到的那一步。
			cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-Command",
				"$errs = $null; [System.Management.Automation.Language.Parser]::ParseFile('"+
					filepath.ToSlash(s)+"', [ref]$null, [ref]$errs) | Out-Null; "+
					"if ($errs -and $errs.Count -gt 0) { $errs | ForEach-Object { $_.Message }; exit 1 }; 'parse-ok'")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("%s 不能解析（PowerShell 会拒绝执行它）：%v\n%s", name, err, out)
				return
			}
			if !strings.Contains(string(out), "parse-ok") {
				t.Errorf("%s 的解析结果读不出来：%s", name, out)
			}
		})
	}
}

// findPowerShell 找 Windows PowerShell 或 PowerShell Core；找不到返回空串（判据会 skip）。
func findPowerShell(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"powershell", "pwsh"} {
		if p, err := exec.LookPath(name); err == nil && p != "" {
			return p
		}
	}
	// Windows 上 PATH 里可能只有带扩展名的形式。
	for _, cand := range []string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"),
	} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	return ""
}
