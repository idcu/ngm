// Command fakeengine 是一个**测试用假引擎**，用于验证 ngm 的 subprocess 协议。
//
// 为什么需要一个真二进制而不是 shell 脚本：
//
//   - 三平台一致（Windows 上没有 /bin/sh，.bat 的行为又不同）
//   - 能精确控制 argv / 退出码 / stderr / stdin，从而断言 ngm 的翻译结果
//   - 不依赖真装 esbuild，也不联网，CI 可离线运行
//
// 它按 P4 的 subprocess 协议工作：stdout 是产物，stderr 是诊断，退出码是成败。
// 行为全部由环境变量驱动，因此同一个二进制可以扮演多种引擎：
//
//	FAKE_VERSION       --version 的输出（默认 "1.0.0-fake"）
//	FAKE_EXIT          退出码（默认 0）
//	FAKE_STDERR        额外写到 stderr 的文本（默认空）
//	FAKE_OUT           写到 stdout 的产物（默认一段合成 bundle）
//	FAKE_ECHO_STDIN    "1" 时把 stdin 原样写到输出
//	FAKE_ECHO_ARGS     "1" 时把 argv 回显到 stderr（默认安静，像规矩的引擎）
//	FAKE_DUMP_ARGS     非空时把 argv 逐行写入该文件（**断言翻译结果的主力**）
//	                     用文件而不是 stderr：stderr 会混入其他诊断，
//	                     而文件是干净的、不会被截断或裁行
//
// 参数中出现 `--outfile=<path>` 时，产物写到该路径（与真实引擎一致），
// 否则写到 stdout。
//
// 该目录名为 testdata，因此 `go build ./...` 与 `go vet ./...` 会跳过它；
// 测试通过显式包路径构建（见 testutils.BuildHelperBinary）。
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	args := os.Args[1:]

	for _, a := range args {
		if a == "--version" {
			fmt.Println(envOr("FAKE_VERSION", "1.0.0-fake"))
			os.Exit(0)
		}
	}

	if dump := os.Getenv("FAKE_DUMP_ARGS"); dump != "" {
		if err := os.WriteFile(dump, []byte(strings.Join(args, "\n")+"\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "fakeengine: cannot dump args:", err)
			os.Exit(99)
		}
	}

	// argv 回显是**可选**的：默认像一个规矩的引擎那样保持 stderr 干净，
	// 只有需要排查时才打开。否则"类型检查通过"这类断言会被噪声污染。
	if os.Getenv("FAKE_ECHO_ARGS") == "1" {
		fmt.Fprintln(os.Stderr, "fakeengine argv: "+strings.Join(args, " "))
	}
	if msg := os.Getenv("FAKE_STDERR"); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
	}

	out := os.Getenv("FAKE_OUT")
	if os.Getenv("FAKE_ECHO_STDIN") == "1" {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "fakeengine: cannot read stdin:", err)
			os.Exit(99)
		}
		out = string(body)
	}
	if out == "" {
		out = "/* fake bundle */\n" + strings.Join(args, "\n") + "\n"
	}

	// typeDecl 的 `--outfile` 是**输出目录**（见 adapter 的 genericInvocation）：
	// 真引擎（tsc）往目录里写多个 .d.ts。假引擎因此在那种形态下写一个固定文件，
	// 使测试能断言"声明真的落盘、且 ngm 把实际出现的文件报了出来"。
	//
	// FAKE_EMIT_NOTHING=1 时什么都不写：用于固定"引擎退出 0 但没有任何产出"的提示
	// （它与"命令悄悄什么都没做"长得一样，是本项目最防的那类失败）。
	if flagValue(args, "--kind=") == "typeDecl" {
		if os.Getenv("FAKE_EMIT_NOTHING") != "1" {
			if dir := flagValue(args, "--outfile="); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					fmt.Fprintln(os.Stderr, "fakeengine: cannot create declaration dir:", err)
					os.Exit(99)
				}
				if err := os.WriteFile(filepath.Join(dir, "index.d.ts"), []byte(out), 0o644); err != nil {
					fmt.Fprintln(os.Stderr, "fakeengine: cannot write declaration:", err)
					os.Exit(99)
				}
			}
		}
	} else if outfile := flagValue(args, "--outfile="); outfile != "" {
		if dir := filepath.Dir(outfile); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				fmt.Fprintln(os.Stderr, "fakeengine: cannot create output dir:", err)
				os.Exit(99)
			}
		}
		if err := os.WriteFile(outfile, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "fakeengine: cannot write output:", err)
			os.Exit(99)
		}
	} else {
		fmt.Print(out)
	}

	if raw := os.Getenv("FAKE_EXIT"); raw != "" {
		var code int
		if _, err := fmt.Sscanf(raw, "%d", &code); err == nil && code != 0 {
			os.Exit(code)
		}
	}
}

// flagValue 取出 `--name=value` 形式参数的值（无则返回空串）。
func flagValue(args []string, prefix string) string {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return strings.TrimPrefix(a, prefix)
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
