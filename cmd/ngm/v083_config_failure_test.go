package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// v0.81：**坏配置必须报出来**——不许静默退回默认。
//
// 起点是实测撞到的一件事：把 `git.defaultProtocol` 写成一个非法值，
// 命令照样跑，没有任何提示（协议被悄悄换回 https）。
// 旧注释给出的理由是"读不出来时策略为 nil ⇒ 判定退化为默认档位 ⇒ 更严"——
// 那个论证只覆盖**策略**一个消费者，而另外两个的退化**不是更严**：
//
//	· `git.defaultProtocol` 悄悄退回 https：用户写 `sshh` 这类笔误时协议被换掉，
//	  而报错指向网络（"连不上"），真因（配置根本没被采用）不可见；
//	· `git.tokenEnvVars` 丢掉用户声明的 token 变量名 ⇒ 脱敏集合漏掉它
//	  ⇒ token 会被原样打进日志与报错（正是 env.go 里那段历史事故的形态）。
//
// 判据四侧：没有配置文件照常（①）· 非法值报错并点名文件与字段（②）·
// 未知字段（typo）也报错（③）· 合法配置照常（④，证明不是"有配置就报错"）。

func TestV83BrokenGlobalConfigIsRejectedNotSilentlyDegraded(t *testing.T) {
	home := isolateUserEnv(t)
	dir := t.TempDir()
	writeFileAt(t, dir, "ngm.json", `{
  "schemaVersion": 1,
  "name": "cfg-test",
  "version": "0.1.0",
  "runtime": "node"
}
`)
	cfgPath := filepath.Join(home, ".ngm", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}

	// ① 没有配置文件：不是错误（`Load` 返回 nil 错）——默认值就是为此存在的。
	if err := os.Remove(cfgPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := newProjectEnv(dir); err != nil {
		t.Fatalf("没有全局配置时不该失败（默认值就是为此存在的）：%v", err)
	}

	// ② 非法值：必须报错，且点名文件与字段。
	writeFileAt(t, home, ".ngm/config.json", `{"schemaVersion":1,"git":{"defaultProtocol":"sqlite"}}`)
	assertConfigRejected(t, dir, "defaultProtocol")

	// ③ 未知字段（typo）：同样必须报出来（`DisallowUnknownFields` 已经写了，
	//    此前只是被 `cerr == nil` 吞掉）。
	writeFileAt(t, home, ".ngm/config.json", `{"schemaVersion":1,"gti":{"defaultProtocol":"https"}}`)
	assertConfigRejected(t, dir, "config.json")

	// ④ 合法配置：照常（牙齿：证明这条判据不是"有配置就报错"）。
	writeFileAt(t, home, ".ngm/config.json", `{"schemaVersion":1,"git":{"defaultProtocol":"https"}}`)
	if _, err := newProjectEnv(dir); err != nil {
		t.Fatalf("合法配置不该失败：%v", err)
	}
}

// assertConfigRejected 断言"坏配置"以 ConfigInvalid 的形式报出，且文本里
// 出现 want（文件名或字段名）——**报错必须指向用户能改的那半边**。
func assertConfigRejected(t *testing.T, dir, want string) {
	t.Helper()
	_, err := newProjectEnv(dir)
	if err == nil {
		t.Fatal("坏配置必须报错：静默退回默认会让协议被换掉、脱敏漏掉用户声明的变量")
	}
	var ngm *errs.NgmError
	if !errors.As(err, &ngm) {
		t.Fatalf("错误应当是 *errs.NgmError：%v", err)
	}
	if ngm.Code != errs.CodeConfigInvalid {
		t.Errorf("错误码 = %v, want CodeConfigInvalid（配置类错误 = exit 3）", ngm.Code)
	}
	// 按**用户实际看到的那段文本**判：`FormatHuman` 会渲染 message + cause + hint
	// （字段名在 cause 行里）。只读 Message 会漏掉它——那是判据自己看错了地方。
	whole := errs.FormatHuman(err)
	if !strings.Contains(whole, want) {
		t.Errorf("报错应当点名 %q，实际是：\n%s", want, whole)
	}
}
