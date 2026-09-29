package adapter

import (
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// Selection 是一次能力调用的引擎选择（简写与完整写法归一化后的同一形态）。
//
// 归一化是 M6 的验收项之一：`"esbuild"` 与 `{"primary":"esbuild"}` 必须
// 得到完全相同的执行结果，否则"简写是语法糖"这句话就不成立。
type Selection struct {
	// Primary 是首选引擎名（必填）。
	Primary string
	// Fallbacks 是按顺序尝试的备选引擎（默认空 = 不做自动回退）。
	Fallbacks []string
	// Options 是覆盖清单 defaultOptions 的选项。
	Options map[string]any
}

// Chain 返回实际尝试顺序：primary 在前，fallbacks 依次在后。
func (s Selection) Chain() []string {
	out := make([]string, 0, 1+len(s.Fallbacks))
	if s.Primary != "" {
		out = append(out, s.Primary)
	}
	for _, f := range s.Fallbacks {
		if strings.TrimSpace(f) != "" {
			out = append(out, f)
		}
	}
	return out
}

// SelectionFor 从 `ngm.json` 的 engines 段取出某个能力类别的选择。
//
// raw 是 engines 对象的原始值映射（能力类别名 → 简写或完整写法），由
// config.EnginesConfig.AsMap 提供。本包刻意**不** import config：
// "怎么执行引擎"不应依赖"怎么解析配置"，两者只在调用处拼接一次。
//
// 返回 ok=false 表示该能力类别没有配置（调用方回退到全局默认或内置默认）。
func SelectionFor(raw map[string]any, kind EngineKind) (Selection, bool, error) {
	if raw == nil {
		return Selection{}, false, nil
	}
	v, ok := raw[string(kind)]
	if !ok || v == nil {
		return Selection{}, false, nil
	}
	sel, err := ResolveSelection(v)
	if err != nil {
		return Selection{}, false, errs.Wrap(errs.CodeConfigInvalid,
			"ngm.json: engines."+string(kind), "", err)
	}
	return sel, true, nil
}

// ResolveSelection 把简写或完整写法归一化为 Selection。
//
//	"esbuild"                                        → {Primary:"esbuild", Fallbacks:[]}
//	{"primary":"esbuild"}                            → 同上
//	{"primary":"a","fallbacks":["b"],"options":{…}}  → 原样
//
// **未知键报错而不是忽略**：`engines` 在 ngm.json 里是 `any`，结构体层面的
// DisallowUnknownFields 管不到它，只能在这里补上严格性——否则
// `{"primry":"esbuild"}` 会被静默当成"没配引擎"，然后悄悄用默认引擎构建，
// 而用户以为自己的配置生效了。
func ResolveSelection(v any) (Selection, error) {
	switch t := v.(type) {
	case nil:
		return Selection{}, errs.New(errs.CodeConfigInvalid,
			"engine selection is null",
			shorthandHint())
	case string:
		name := strings.TrimSpace(t)
		if name == "" {
			return Selection{}, errs.New(errs.CodeConfigInvalid,
				"engine selection is an empty string", shorthandHint())
		}
		return Selection{Primary: name, Fallbacks: []string{}}, nil
	case map[string]any:
		return resolveSelectionMap(t)
	default:
		return Selection{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("engine selection must be a string or an object, got %T", v),
			shorthandHint())
	}
}

func resolveSelectionMap(m map[string]any) (Selection, error) {
	var sel Selection

	for key := range m {
		switch key {
		case "primary", "fallbacks", "options":
		default:
			return Selection{}, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("unknown key %q in engine selection", key),
				"valid keys: primary, fallbacks, options")
		}
	}

	switch p := m["primary"].(type) {
	case nil:
		return Selection{}, errs.New(errs.CodeConfigInvalid,
			"engine selection is missing `primary`",
			"use a shorthand string (`\"esbuild\"`) or {\"primary\": \"esbuild\"}")
	case string:
		sel.Primary = strings.TrimSpace(p)
		if sel.Primary == "" {
			return Selection{}, errs.New(errs.CodeConfigInvalid, "`primary` is empty", "")
		}
	default:
		return Selection{}, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("`primary` must be a string, got %T", m["primary"]), "")
	}

	if raw, ok := m["fallbacks"]; ok && raw != nil {
		list, ok := raw.([]any)
		if !ok {
			return Selection{}, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("`fallbacks` must be an array of engine names, got %T", raw), "")
		}
		for _, item := range list {
			name, ok := item.(string)
			if !ok {
				return Selection{}, errs.New(errs.CodeConfigInvalid,
					fmt.Sprintf("`fallbacks` entries must be strings, got %T", item), "")
			}
			name = strings.TrimSpace(name)
			if name == "" {
				return Selection{}, errs.New(errs.CodeConfigInvalid, "`fallbacks` contains an empty name", "")
			}
			sel.Fallbacks = append(sel.Fallbacks, name)
		}
	}
	if sel.Fallbacks == nil {
		sel.Fallbacks = []string{}
	}

	if raw, ok := m["options"]; ok && raw != nil {
		opts, ok := raw.(map[string]any)
		if !ok {
			return Selection{}, errs.New(errs.CodeConfigInvalid,
				fmt.Sprintf("`options` must be an object, got %T", raw), "")
		}
		sel.Options = opts
	}

	return sel, nil
}

func shorthandHint() string {
	return `use a shorthand string ("esbuild") or the full form ({"primary": "esbuild"})`
}
