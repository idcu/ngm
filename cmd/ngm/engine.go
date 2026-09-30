package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/idcu/ngm/internal/adapter"
	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/mappings"
)

// engineContext 汇总一次引擎调用需要的外部资源。
//
// 抽成一处的原因：build / typecheck / css / engines 四个命令共用同一套
// "项目声明 + 三级配置 + 引擎清单 + 执行器"的装配逻辑；分散实现会让
// 引擎选择的优先级在不同命令间漂移。
type engineContext struct {
	env *projectEnv
	// pf 是项目的 ngm.json 原始声明（用于 main 入口与 engines 段）。
	pf *config.ProjectFile
	// resolved 是三级合并后的配置（用于全局默认引擎）。
	resolved *config.Resolved
	catalog  *adapter.Catalog
	runner   *adapter.Runner
}

// newEngineContext 装配运行环境。
func newEngineContext(dirFlag string, stderr io.Writer) (*engineContext, error) {
	env, err := newProjectEnv(dirFlag)
	if err != nil {
		return nil, err
	}
	pf, err := env.ReadManifest()
	if err != nil {
		return nil, err
	}
	resolved, err := config.Load(env.ProjectDir, homeDirOrEmpty())
	if err != nil {
		return nil, err
	}
	cat, err := adapter.LoadCatalog(env.ProjectDir, env.Layout.Home)
	if err != nil {
		return nil, err
	}

	runner := adapter.NewRunner(cat, env.ProjectDir)
	// 回退是隐式行为，必须让用户看见：primary 挂了却只看到 fallback 的
	// 输出，会让人误判是哪个引擎在干活。
	runner.OnWarn(func(format string, args ...any) {
		fmt.Fprintf(stderr, "note: "+format+"\n", args...)
	})
	// `run:<引擎>` 权限：判定挂在**实际要执行的那个**引擎上（含回退链上的每一个），
	// 而不是选择阶段——选择里可能带着永远不会用到的 fallback。
	runner.OnEngine(func(entry adapter.Entry) error {
		target := engineRunTarget(entry)
		if target == "" {
			return nil
		}
		return env.Policy.CheckRun(target)
	})

	return &engineContext{env: env, pf: pf, resolved: resolved, catalog: cat, runner: runner}, nil
}

// selectionFor 解析某个能力类别要用的引擎选择。
//
// 优先级（guides/configuration.md §engines）：
//
//	--engine=<name>                        最高
//	ngm.json engines.<kind>                项目配置（简写或完整写法）
//	~/.ngm/config.json engines.default<X>  全局默认
//	内置默认                                bundle/transform → esbuild
func (ec *engineContext) selectionFor(kind adapter.EngineKind, engineFlag string) (adapter.Selection, error) {
	if flag := strings.TrimSpace(engineFlag); flag != "" {
		return adapter.Selection{Primary: flag, Fallbacks: []string{}}, nil
	}

	if sel, ok, err := adapter.SelectionFor(ec.pf.Engines.AsMap(), kind); err != nil {
		return adapter.Selection{}, err
	} else if ok {
		return sel, nil
	}

	if g := ec.resolved.GlobalEffective; g != nil && g.Engines != nil {
		if name := globalDefaultFor(g.Engines, kind); name != "" {
			return adapter.Selection{Primary: name, Fallbacks: []string{}}, nil
		}
	}

	if name := builtinDefaultFor(kind); name != "" {
		return adapter.Selection{Primary: name, Fallbacks: []string{}}, nil
	}

	return adapter.Selection{}, errs.New(errs.CodeConfigInvalid,
		fmt.Sprintf("no engine configured for `%s`", kind),
		"pass --engine=<name>, or set engines."+string(kind)+" in ngm.json"+
			availableHintFor(ec.catalog, kind))
}

// engineRunTarget 返回引擎对应的权限目标（`run:<exe>`）；没有外部进程时返回空串。
//
// 内置的 self 引擎不派生任何进程——它是 ngm 进程内的一段逻辑，
// 因此不适用 run: 权限：要求一个 "run:self" 会凭空造出用户从未听说过的权限名，
// 而他会照提示把它写进配置。
func engineRunTarget(entry adapter.Entry) string {
	if entry.Adapter != adapter.AdapterSubprocess {
		return ""
	}
	prog := strings.TrimSpace(entry.Program)
	if prog == "" {
		return ""
	}
	base := filepath.Base(prog)
	// Windows 上 Program 可能带 .exe/.cmd：权限名与文档一致用无扩展名的形式（run:esbuild）
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// globalDefaultFor 从全局配置取某能力类别的默认引擎。
//
// typeDecl 与 css 没有对应的全局字段（guides/configuration.md 只定义了
// transform / bundle / typeCheck 三个默认），因此返回空串表示"没有全局默认"。
func globalDefaultFor(g *config.GlobalEngines, kind adapter.EngineKind) string {
	switch kind {
	case adapter.KindTransform:
		return g.DefaultTransform
	case adapter.KindBundle:
		return g.DefaultBundle
	case adapter.KindTypeCheck:
		return g.DefaultTypeCheck
	default:
		return ""
	}
}

// builtinDefaultFor 返回内置默认引擎。
//
// 只有 bundle 与 transform 有默认（esbuild）：v0.1 适配的引擎只有它，
// 给 typeCheck / css 编一个默认值等于对用户撒谎——他会得到"引擎不可用"，
// 却不知道自己什么时候配过那个引擎。
func builtinDefaultFor(kind adapter.EngineKind) string {
	switch kind {
	case adapter.KindBundle, adapter.KindTransform:
		return "esbuild"
	default:
		return ""
	}
}

// availableHintFor 生成"该类别可用引擎"的提示后缀。
func availableHintFor(c *adapter.Catalog, kind adapter.EngineKind) string {
	names := c.NamesFor(kind)
	if len(names) == 0 {
		return " (no `" + string(kind) + "` engine is available in this build)"
	}
	return " (available: " + strings.Join(names, ", ") + ")"
}

// mappingsAlias 读取 ngm.mappings.json 并转成引擎别名表。
//
// 这是 `ngm build` 相对"直接跑引擎"的全部增量：让 `github:org/repo` 这样的
// 裸导入解析到 ngm.vendor 里那份**可证明**的代码（guides/build.md）。
// 没有 mappings 文件不是错误——项目可能还没有依赖。
//
// 别名**优先指向 `main` 文件**而不是目录：`main` 在 mappings schema 里的
// 定义就是"该依赖的入口文件"，指向文件比指向目录更精确，也免去依赖各引擎
// 自己的目录解析规则（esbuild 对目录会去找 package.json main / index.js，
// 未必认 index.ts）。`main` 缺失时才退化为目录。
//
// 键用的是**导入标识符**（Mapping.Specifier）而不是 `from`：同一个仓库的
// 多个子路径条目共用 `from`，用它当键会让后一行覆盖前一行。后果不是"某个导入
// 失败"那么明显——存活的那一行若指向一个存在的目录，构建会**成功**，
// 只是把两个子路径解析到了同一份代码。
//
// 别名之间的顺序不影响结果：实测 esbuild 在多个键都能匹配时会选**最具体**的那个
// （与 `--alias:` 的先后无关）。适配器仍按键排序输出，那是为了让 argv 可复现。
func (ec *engineContext) mappingsAlias() (map[string]string, error) {
	mf, err := mappings.Read(mappings.Find(ec.env.ProjectDir))
	if err != nil {
		return nil, err
	}
	if mf == nil || len(mf.Mappings) == 0 {
		return nil, nil
	}

	alias := make(map[string]string, len(mf.Mappings))
	for _, m := range mf.Mappings {
		key := m.Specifier()
		to := strings.TrimSpace(m.To)
		if strings.TrimSpace(m.From) == "" || to == "" {
			continue
		}
		if main := strings.TrimSpace(m.Main); main != "" {
			// 用字符串拼接而不是 path.Join：后者会把前导 "./" 吃掉，
			// 而 esbuild 会把不带 "./" 的相对路径当**包名**去 node_modules 里找。
			//
			// 同时把 main 自身的前导 "./" 去掉，避免拼出 `.../lib/./index.ts`
			// 这种合法但难读的路径（--dry-run 的输出要给人看）。
			main = strings.TrimLeft(strings.TrimPrefix(main, "./"), "/")
			to = strings.TrimRight(to, "/") + "/" + main
		}
		alias[key] = to
	}
	return alias, nil
}

// readInputFile 读取相对项目根的输入文件。
//
// 路径相对**项目目录**而不是进程 cwd 解析：`ngm css src/app.css --dir ../app`
// 应当找 `../app/src/app.css`，而不是当前工作目录下的 `src/app.css`。
// 绝对路径原样使用。
func readInputFile(projectDir, input string) ([]byte, error) {
	p := input
	if !filepath.IsAbs(p) {
		p = filepath.Join(projectDir, p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+input,
			"the path is resolved relative to the project directory ("+projectDir+")", err)
	}
	return data, nil
}

// printPlans 渲染 --dry-run 的执行计划。
func printPlans(w io.Writer, plans []adapter.Plan) {
	for i, p := range plans {
		label := "primary"
		if i > 0 {
			label = fmt.Sprintf("fallback %d", i)
		}
		symbol := "✓"
		status := "available"
		switch {
		case p.Stub:
			symbol, status = "·", "stub — never produces an artifact (dry-run only)"
		case !p.Available:
			symbol, status = "✗", "NOT AVAILABLE (not on PATH)"
		case p.Version != "":
			status = "available (" + p.Version + ")"
		}

		fmt.Fprintf(w, "%s %s: %s (%s, %s)\n", symbol, label, p.Engine, p.Kind, p.Adapter)
		fmt.Fprintf(w, "    status:  %s\n", status)
		fmt.Fprintf(w, "    command: %s\n", p.CommandLine())
		if p.ReadsStdin {
			fmt.Fprintf(w, "    input:   stdin\n")
		}
		source := "built-in catalog"
		if !p.Builtin {
			source = adapter.FileName
		}
		fmt.Fprintf(w, "    source:  %s\n", source)
	}
}
