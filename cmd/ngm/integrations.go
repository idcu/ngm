package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/idcu/ngm/internal/errs"
	"github.com/idcu/ngm/internal/integrations"
	"github.com/idcu/ngm/internal/mappings"
)

const integrationsUsage = `ngm integrations — make a build tool consume ngm.mappings.json

USAGE:
  ngm integrations add <tool> [--dir=<dir>] [--dry-run] [--json]

TOOLS:
  vite      vite.config.ts          (resolve.alias)
  esbuild   ngm.esbuild.cjs         (runnable: node ngm.esbuild.cjs)
  deno      ngm.importmap.json      (+ deno.json when the project has none)
  webpack   ngm.webpack.cjs         (resolve.alias)

Every tool also gets ngm.tsconfig.json: TypeScript does not understand the
` + "`github:`" + ` prefix, so ` + "`tsc`" + ` and your editor need compilerOptions.paths.

WHAT THIS WILL NOT DO:
  Your own files (vite.config.ts, tsconfig.json, deno.json) are never
  overwritten. If one exists and differs, the command prints the difference and
  exits 3 without writing anything at all - a half-applied scaffold is worse
  than none. Files named ngm.* belong to ngm and are regenerated.

EXIT CODES:
  0  generated, or already up to date
  3  configuration error, or a file ngm will not overwrite
`

// runIntegrations 处理 `ngm integrations <subcommand>`。
func runIntegrations(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("integrations", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	dryRun := fs.Bool("dry-run", false, "show what would be written")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	fs.Usage = func() { fmt.Fprint(stderr, integrationsUsage) }

	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "dry-run", Bool: true}, {Name: "json", Bool: true},
	})); err != nil {
		return 3
	}
	if fs.NArg() != 2 || fs.Arg(0) != "add" {
		fmt.Fprint(stderr, integrationsUsage)
		return 3
	}

	tool, err := integrations.ParseTool(fs.Arg(1))
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	env, err := newProjectEnv(*dirFlag)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	f, err := mappings.Read(mappings.Find(env.ProjectDir))
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if f == nil {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			mappings.FileName+" not found in "+env.ProjectDir,
			"run `ngm install` to generate it"))
	}

	arts, warns, err := integrations.Plan(tool, f)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	outcomes, err := integrations.Apply(env.ProjectDir, arts, *dryRun)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}

	_, conflicts := integrations.FormatOutcomes(outcomes)
	code := exitIntegrations(conflicts)

	if *jsonOut {
		return writeIntegrationsJSON(stdout, stderr, string(tool), *dryRun, outcomes, warns, code)
	}

	for _, w := range warns {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}

	head := fmt.Sprintf("ngm integrations add %s", tool)
	if *dryRun {
		head += " (dry run: nothing was written)"
	}
	fmt.Fprintln(stdout, head)
	text, _ := integrations.FormatOutcomes(outcomes)
	fmt.Fprint(stdout, text)

	if conflicts > 0 {
		fmt.Fprint(stderr, integrations.FormatConflicts(outcomes))
		return runErr(ctx, stdout, stderr, integrations.ConflictError(conflicts))
	}

	fmt.Fprintf(stdout, "\n%s\n", integrations.NextStep(tool))
	return code
}

// exitIntegrations 把冲突数映射为退出码。
func exitIntegrations(conflicts int) int {
	if conflicts > 0 {
		return 3
	}
	return 0
}

func writeIntegrationsJSON(stdout, stderr io.Writer, tool string, dryRun bool, outcomes []integrations.Outcome, warns []string, code int) int {
	type artifact struct {
		Path   string `json:"path"`
		Status string `json:"status"`
		Diff   string `json:"diff,omitempty"`
		Hint   string `json:"hint,omitempty"`
	}
	payload := struct {
		Tool      string     `json:"tool"`
		DryRun    bool       `json:"dryRun"`
		Artifacts []artifact `json:"artifacts"`
		Warnings  []string   `json:"warnings"`
		ExitCode  int        `json:"exitCode"`
	}{Tool: tool, DryRun: dryRun, Warnings: warns, ExitCode: code}
	if payload.Warnings == nil {
		payload.Warnings = []string{}
	}
	for _, o := range outcomes {
		payload.Artifacts = append(payload.Artifacts, artifact{
			Path: o.Path, Status: string(o.Status), Diff: o.Diff, Hint: o.Hint,
		})
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "encode report: %v\n", err)
		return 1
	}
	if _, werr := stdout.Write(append(data, '\n')); werr != nil {
		fmt.Fprintf(stderr, "write report: %v\n", werr)
		return 1
	}
	return code
}
