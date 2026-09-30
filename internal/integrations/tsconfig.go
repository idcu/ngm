package integrations

import "github.com/idcu/ngm/internal/mappings"

const (
	// TsconfigSidecarPath 是 ngm 生成的 tsconfig 片段（文件名属于 ngm，可重生成）。
	TsconfigSidecarPath = "ngm.tsconfig.json"
	// TsconfigPath 是项目自己的 tsconfig（可能已有，永不覆盖）。
	TsconfigPath = "tsconfig.json"
)

// tsconfigSidecar 只放 `paths`，别的什么都不放。
//
// 生成的文件越小，与用户已有配置冲突的面就越小：用户通过 `extends` 继承它时，
// 自己的 compilerOptions 会覆盖同名字段——我们只声明真正必需的那一项。
type tsconfigSidecar struct {
	CompilerOptions struct {
		Paths map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// tsconfigRoot 只在项目**没有** tsconfig.json 时生成，给新项目一个可用起点。
//
// 不写 `baseUrl`：TypeScript 7 已移除该选项（写上去直接报
// TS5102: Option 'baseUrl' has been removed），而 `paths` 单独就能工作（已用真实 tsc 验证）。
type tsconfigRoot struct {
	Extends         string `json:"extends"`
	CompilerOptions struct {
		Target            string `json:"target"`
		Module            string `json:"module"`
		ModuleResolution  string `json:"moduleResolution"`
		ResolveJSONModule bool   `json:"resolveJsonModule"`
		Strict            bool   `json:"strict"`
	} `json:"compilerOptions"`
	Include []string `json:"include"`
}

// tsconfigArtifacts 生成 `github:` 前缀的类型解析配置。
//
// 它属于每个集成：Vite / esbuild / Webpack 能跑不代表编辑器与 `tsc` 认。
func tsconfigArtifacts(f *mappings.File) ([]Artifact, []string, error) {
	var warns []string
	for _, m := range f.Mappings {
		if m.Main == "" && m.Types == "" {
			warns = append(warns, m.Specifier()+" has no `main` or `types`; "+
				"its tsconfig entry can only point at a directory, which TypeScript resolves "+
				"only when it happens to contain index.*")
		}
	}

	var sidecar tsconfigSidecar
	sidecar.CompilerOptions.Paths = TsconfigPaths(f)
	sidecarBody, err := MarshalJSONFile(sidecar)
	if err != nil {
		return nil, warns, err
	}

	var root tsconfigRoot
	root.Extends = "./" + TsconfigSidecarPath
	root.CompilerOptions.Target = "es2020"
	root.CompilerOptions.Module = "esnext"
	root.CompilerOptions.ModuleResolution = "bundler"
	root.CompilerOptions.ResolveJSONModule = true
	root.CompilerOptions.Strict = true
	root.Include = []string{"src"}
	rootBody, err := MarshalJSONFile(root)
	if err != nil {
		return nil, warns, err
	}

	arts := []Artifact{
		{
			Path:    TsconfigSidecarPath,
			Content: sidecarBody,
			Owned:   true,
			Purpose: "tsconfig `paths` for the github: prefix (static: re-run after dependency changes)",
		},
		{
			Path:       TsconfigPath,
			Content:    rootBody,
			CreateOnly: true,
			Hint: "your tsconfig.json was left untouched - add \"extends\": \"./" + TsconfigSidecarPath +
				"\" to it, or merge that file's compilerOptions.paths (if your tsconfig already " +
				"defines paths, yours wins and you must merge by hand)",
			Purpose: "tsconfig for a project that has none",
		},
	}
	return arts, warns, nil
}
