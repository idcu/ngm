package integrations

import (
	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/mappings"
)

const (
	// DenoImportMapPath 是 ngm 生成的 import map（文件名属于 ngm，可重生成）。
	DenoImportMapPath = "ngm.importmap.json"
	// DenoConfigPath 是 Deno 的配置文件（用户可能已有，永不覆盖）。
	DenoConfigPath = "deno.json"
)

// denoConfig 只在项目没有 deno.json 时生成，作用是把它指向 ngm 的 import map。
//
// 为什么不让 ngm 去改已有的 deno.json：那是用户的文件（常带注释，是 jsonc），
// 而我们无法在不破坏它的前提下安全地插入一个键。指向一份**独立**的 import map
// 还有个好处：依赖变化时只需重生成 ngm 自己的文件。
const denoConfig = `{
  "importMap": "./ngm.importmap.json"
}
`

func denoArtifacts(f *mappings.File) ([]Artifact, []string, error) {
	_, imports, warns := DenoImports(f)
	if len(imports) == 0 {
		return nil, warns, errs.New(errs.CodeConfigInvalid,
			"there is nothing to map for Deno: "+mappings.FileName+" has no usable entries",
			"run `ngm install` to (re)generate mappings")
	}

	body, err := MarshalJSONFile(struct {
		Imports map[string]string `json:"imports"`
	}{Imports: imports})
	if err != nil {
		return nil, warns, err
	}

	arts := []Artifact{
		{
			Path:    DenoImportMapPath,
			Content: body,
			Owned:   true,
			// 与 Vite / esbuild / Webpack 不同，这里**必须**重生成：
			// import map 是静态 JSON，无法在运行时读取 mappings。
			Purpose: "Deno import map (static: re-run after dependency changes)",
		},
		{
			Path:       DenoConfigPath,
			Content:    []byte(denoConfig),
			CreateOnly: true,
			Hint: "your deno.json was left untouched - add \"importMap\": \"./ngm.importmap.json\" " +
				"to it, or run deno with --import-map=ngm.importmap.json",
			Purpose: "Deno config pointing at the generated import map",
		},
	}
	return arts, warns, nil
}
