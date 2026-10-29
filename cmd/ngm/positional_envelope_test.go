package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// v0.108：**位置参数**失败时也要有机器可读的那一份。
//
// 缺陷形态（实测）：`ngm why --json`（漏给依赖）、`ngm sbom --json ZZ`、
// `ngm attest --json ZZ`、`ngm engines --json` 都是 exit 3 + **stdout 0 字节**。
// 用户明明要机器可读输出，却什么也没拿到——脚本只能看到"退出码 + 空"。
//
// v0.54 修的是 `fs.Parse` 失败那一支，v0.105 补的是 `doctor`；
// 位置参数（`fs.NArg()`）这一支此前四条命令都没覆盖。
// 这是**同一份契约的第三处缺口**，所以判据按命令枚举，而不是只钉一条。
func TestV108PositionalFailuresCarryAnEnvelope(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"why without a dependency", []string{"why", "--json"}},
		{"why is positional-count-sensitive", []string{"why", "a", "b", "--json"}},
		{"tree with a stray argument", []string{"tree", "ZZ-JUNK-ARG", "--json"}},
		{"engines without a subcommand", []string{"engines", "--json"}},
		// `sbom` / `attest` **不在**这张表里：它们不接受 `--json`
		// （stdout 就是文档本身），而按 v0.16 规则④，"命令不接受 --json 时
		// stdout 仍为空"是正确行为。把它们列进来，判据就会逼出一条死分支。
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateUserEnv(t)
			p := newProject(t)
			args := append([]string{}, tc.args...)
			args = append(args, "--dir="+p)

			var out, errb bytes.Buffer
			code := dispatch(args, &out, &errb)

			if code != 3 {
				t.Fatalf("exit = %d, want 3 (a usage error):\n%s", code, errb.String())
			}
			if out.Len() == 0 {
				t.Fatalf("`ngm %s` asked for --json but wrote nothing to stdout — "+
					"a script cannot tell what went wrong", tc.name)
			}
			var env struct {
				Version int `json:"version"`
				Error   *struct {
					Code     string `json:"code"`
					ExitCode int    `json:"exitCode"`
					Message  string `json:"message"`
					Hint     string `json:"hint"`
				} `json:"error"`
			}
			if err := json.Unmarshal(out.Bytes(), &env); err != nil {
				t.Fatalf("stdout is not the error envelope: %v\n%s", err, out.String())
			}
			if env.Error == nil {
				t.Fatalf("no `error` object in the envelope:\n%s", out.String())
			}
			// 规矩 3：信封里的退出码必须等于进程退出码。
			if env.Error.ExitCode != code {
				t.Errorf("envelope exitCode = %d, process exit = %d — machine output must not lie",
					env.Error.ExitCode, code)
			}
			if env.Error.Hint == "" {
				t.Error("the envelope must carry the next step too (v0.51)")
			}
		})
	}

	// 对照：没提 `--json` 时 stdout 必须仍为空（不许给没要的人塞 JSON）。
	t.Run("without --json stdout stays empty", func(t *testing.T) {
		isolateUserEnv(t)
		p := newProject(t)
		var out, errb bytes.Buffer
		if code := dispatch([]string{"why", "--dir=" + p}, &out, &errb); code != 3 {
			t.Fatalf("exit = %d, want 3", code)
		}
		if out.Len() != 0 {
			t.Errorf("stdout = %q, want empty: nobody asked for machine-readable output", out.String())
		}
	})
}
