package main

import (
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// v0.63：**按 m6 的顺序**重测那四个引擎命令的码 6。
//
// 起点是 v0.62 的一处**错误结论**：那一版说 `AllowedEngines()` 在整个仓库里没有消费者、
// 于是 `AllowEngines` 是个死钩子 ✗。真相是它的消费者在 `testenv_test.go` 的 `init()` 里：
//
//	testutils.WriteUserConfig = func(home string) error {
//		engines := testutils.AllowedEngines()      // ← 就在这里读
//		…写 permissions.allow = ["run:fake-engine", …]
//	}
//
// 而 `WriteUserConfig` 由 `isolateUserEnv` 调用（每次换 HOME 都会写一份）⇒
// **声明的时机必须在 isolate 之前**：`TestM6Acceptance` 正是这么排的
// （第 107 行，在任何项目/HOME 之前），而 v0.62 的测量把声明放在了 isolate **之后** ✗。
//
// 这个测试照 m6 的顺序重来，逐个确认四个命令能不能走到"写报告"那一步 ⇒ 退 6。
func TestV63EngineCommandsAreMeasuredForCodeSix(t *testing.T) {
	// 顺序就是判据：**在任何 isolate/newProject 之前**声明本测试要执行的引擎。
	// 它由 `testenv_test.go` 的注入实现写进每个新 HOME 的全局配置。
	testutils.AllowEngines(t, "fake-engine")
	fakeEngine = testutils.BuildHelperBinary(t, "./internal/adapter/testdata/fakeengine", "fake-engine")

	t.Run("build", func(t *testing.T) {
		measureInternalFailure(t, "build", "--engine=fake", "--dir="+engineProject(t, "bundle"))
	})
	t.Run("typecheck", func(t *testing.T) {
		measureInternalFailure(t, "typecheck", "--engine=fake", "--dir="+engineProject(t, "typeCheck"))
	})
	t.Run("css", func(t *testing.T) {
		proj := engineProject(t, "css")
		testutils.WriteFile(t, proj, "src/app.css", "body { color: red }\n")
		measureInternalFailure(t, "css", "src/app.css", "--engine=fake", "--dir="+proj)
	})
	t.Run("transform", func(t *testing.T) {
		measureInternalFailure(t, "transform", "--engine=fake", "--dir="+engineProject(t, "transform"))
	})
}

// engineProject 建一个"引擎可用"的项目：
// **隔离用户态 → 新项目 → 把假引擎登记进给定能力类别**。
//
// 三步缺一不可，而第一★步最容易漏（v0.63 实测到的两处坑）：
//
//   - `AllowEngines` 要声明在**隔离之前**（顶部那行）——注入实现是在
//     `isolateUserEnv` 里读它的，写进那一刻的 HOME；
//   - `isolateUserEnv` **必须显式调用**：`newProject` 只管目录与 `init`，
//     它**不换 HOME**。漏掉它，命令读的就是**真实的** `~/.ngm/config.json`
//     ——于是 `run:fake-engine` 没被授权，命令退 3（`permission denied`）。
//     v0.62 把这处失败读成了"钩子是死的"，其实只是这里少了一行。
func engineProject(t *testing.T, kinds ...string) string {
	t.Helper()
	isolateUserEnv(t)
	proj := newProject(t)
	m6Catalog(t, proj, fakeEngine, kinds...)
	return proj
}
