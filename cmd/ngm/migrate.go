// ngm migrate：把 v1/v2 项目清单与 2.x lock 迁移到 schemaVersion 3 / lockfileVersion 3.0.0。
//
// 立场（ADR-055）：只搬能证明的字段；任何无法安全映射的条目进入结构化 skipped
// 清单，整次迁移不写任何文件（包括其它文件的备份），要求人工重装该条目。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/idcu/ngm/internal/config"
	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/lock"
	"github.com/idcu/ngm/internal/core/resolve"
)

const migrateUsage = `ngm migrate — migrate v1/v2 manifests and 2.x locks to v3

USAGE:
  ngm migrate [--dir=<dir>] [--dry-run] [--json]

FLAGS:
  --dir       project directory containing ngm.json / ngm.lock (default: .)
  --dry-run   report the changes and backups that would happen; write nothing
  --json      write a machine-readable report to stdout (CI should use this)

WHAT IT DOES:
  schemaVersion 1/2 → 3, lockfileVersion 2.x → 3.0.0. Only fields with a proven
  mapping are carried over (ref / refType / commit / digest / vendorPath /
  subPath / requiredBy). Entries with a missing digest, an invalid shape or a
  changed meaning are listed under "skipped" and force a non-zero exit; nothing
  is written, so a half-migrated project can never be produced.

  Before any write the original file is copied to <file>.bak; the report prints
  every backup path. If both files are already v3 the command says so and exits 0.

EXIT CODES:
  0  migrated (or already v3; or a successful --dry-run)
  3  an entry could not be mapped safely, or the input is not 1/2 / 2.x
  6  internal failure (report was produced but could not be written)
`

// migrateReport 是 migrate 的成功/中止报告（两种通道读同一份数据）。
type migrateReport struct {
	Dir            string              `json:"dir"`
	DryRun         bool                `json:"dryRun"`
	AlreadyCurrent bool                `json:"alreadyCurrent,omitempty"`
	Files          []migrateFileChange `json:"files"`
	Skipped        []migrateSkip       `json:"skipped,omitempty"`
	NeedsManual    bool                `json:"needsManual"`
}

type migrateFileChange struct {
	Path            string `json:"path"`
	From            string `json:"from"`
	To              string `json:"to"`
	Backup          string `json:"backup,omitempty"`
	EntriesMigrated int    `json:"entriesMigrated,omitempty"`
	Note            string `json:"note,omitempty"`
	data            []byte // 仅进程内使用，不进 JSON
	original        []byte
}

type migrateSkip struct {
	File   string `json:"file"`
	Index  int    `json:"index,omitempty"`
	Entry  string `json:"entry,omitempty"`
	Reason string `json:"reason"`
	Fix    string `json:"fix"`
}

func (r *migrateReport) Marshal() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (r *migrateReport) Render(w io.Writer) {
	fmt.Fprintf(w, "migrate: %s（%s）\n", r.Dir, map[bool]string{true: "dry-run", false: "写入"}[r.DryRun])
	for _, f := range r.Files {
		fmt.Fprintf(w, "  %s: %s → %s", filepath.Base(f.Path), f.From, f.To)
		if f.EntriesMigrated > 0 {
			fmt.Fprintf(w, " · 迁移 %d 条", f.EntriesMigrated)
		}
		if f.Note != "" {
			fmt.Fprintf(w, " · %s", f.Note)
		}
		fmt.Fprintln(w)
		if f.Backup != "" {
			if r.DryRun {
				fmt.Fprintf(w, "    备份（dry-run，未创建）: %s\n", f.Backup)
			} else {
				fmt.Fprintf(w, "    备份: %s\n", f.Backup)
			}
		}
	}
	if r.AlreadyCurrent {
		fmt.Fprintln(w, "无需迁移：ngm.json 与 ngm.lock 均已是 v3。")
	}
	if r.DryRun {
		fmt.Fprintln(w, "dry-run：没有写入任何文件，也没有创建备份。")
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintf(w, "skipped %d 条（需手工处理）:\n", len(r.Skipped))
		for _, s := range r.Skipped {
			where := s.File
			if s.Entry != "" {
				where += " " + s.Entry
			}
			fmt.Fprintf(w, "  - %s: %s\n", where, s.Reason)
			if s.Fix != "" {
				fmt.Fprintf(w, "      → %s\n", s.Fix)
			}
		}
	}
	if r.NeedsManual {
		fmt.Fprintln(w, "迁移中止：有条目无法安全映射，未写入任何文件；请按上面的 fix 逐条重装后重试。")
	}
}

// runMigrate 处理 `ngm migrate`。
func runMigrate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("migrate", stderr)
	dirFlag := fs.String("dir", ".", "project directory")
	dryRun := fs.Bool("dry-run", false, "report changes without writing files")
	jsonOut := fs.Bool("json", false, "machine-readable report")
	c := newCmdContext(ctx, args, stdout, stderr, fs)
	c.usage(migrateUsage)
	if !c.parse([]flagSpec{
		{Name: "dir"}, {Name: "dry-run", Bool: true}, {Name: "json", Bool: true},
	}, jsonOut) {
		return 3
	}
	if fs.NArg() != 0 {
		return c.rejectExtraArgs(migrateUsage, "`ngm migrate` takes no positional arguments")
	}

	rep, err := planMigration(*dirFlag, *dryRun)
	if err != nil {
		return c.fail(err)
	}
	code := 0
	if rep.NeedsManual {
		code = errs.CodeConfigInvalid.ExitCode()
	}
	if *jsonOut {
		return c.writeReport(rep, code)
	}
	rep.Render(stdout)
	return code
}

// planMigration 计算并完成迁移；dry-run 时只计算与输出。
func planMigration(dir string, dryRun bool) (*migrateReport, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir",
			"pass an absolute --dir, or run ngm from the directory you mean", err)
	}
	rep := &migrateReport{Dir: abs, DryRun: dryRun, Files: []migrateFileChange{}}

	mfPath := filepath.Join(abs, config.FileName)
	lfPath := lock.Find(abs)
	mfData, mfErr := os.ReadFile(mfPath)
	lfData, lfErr := os.ReadFile(lfPath)
	if mfErr != nil && !os.IsNotExist(mfErr) {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+mfPath, "check the file exists and is readable, then re-run migrate", mfErr)
	}
	if lfErr != nil && !os.IsNotExist(lfErr) {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "read "+lfPath, "check the file exists and is readable, then re-run migrate", lfErr)
	}
	if mfData == nil && lfData == nil {
		return nil, errs.New(errs.CodeConfigInvalid,
			"no ngm.json or ngm.lock found in "+abs,
			"run `ngm migrate --dir=<project>` pointing at a directory with a project manifest or lock")
	}

	allCurrent := true
	if mfData != nil {
		m, err := migrateManifest(mfData, mfPath)
		if err != nil {
			return nil, err
		}
		if m.AlreadyCurrent {
			rep.Files = append(rep.Files, migrateFileChange{
				Path: mfPath, From: "schemaVersion 3", To: "schemaVersion 3", Note: "无需迁移",
			})
		} else {
			allCurrent = false
			fc := migrateFileChange{
				Path: mfPath, From: fmt.Sprintf("schemaVersion %d", m.FromVersion),
				To:              fmt.Sprintf("schemaVersion %d", config.SchemaVersion),
				EntriesMigrated: m.Entries, data: m.Data, original: mfData,
			}
			rep.Files = append(rep.Files, fc)
		}
		for _, s := range m.Skipped {
			rep.Skipped = append(rep.Skipped, s)
		}
	}
	if lfData != nil {
		outcome, err := lock.MigrateV2(lfData, lfPath)
		if err != nil {
			return nil, err
		}
		if outcome.AlreadyCurrent {
			rep.Files = append(rep.Files, migrateFileChange{
				Path: lfPath, From: "lockfileVersion " + outcome.FromLockfileVersion,
				To: "lockfileVersion " + outcome.FromLockfileVersion, Note: "无需迁移",
			})
		} else {
			allCurrent = false
			data, err := outcome.File.Marshal()
			if err != nil {
				return nil, err
			}
			// 没有 skipped 时图边才可能完整，做一次完整校验，绝不写出读不回来的 lock。
			if len(outcome.Skipped) == 0 {
				if err := outcome.File.Validate(); err != nil {
					return nil, errs.Wrap(errs.CodeConfigInvalid,
						"refusing to migrate "+lfPath+" into an invalid v3 lock",
						"fix the reported field manually, then run `ngm migrate` again", err)
				}
			}
			fc := migrateFileChange{
				Path: lfPath, From: "lockfileVersion " + outcome.FromLockfileVersion,
				To:              "lockfileVersion " + lock.FileVersion,
				EntriesMigrated: len(outcome.File.Dependencies), data: data, original: lfData,
			}
			rep.Files = append(rep.Files, fc)
		}
		for _, s := range outcome.Skipped {
			rep.Skipped = append(rep.Skipped, migrateSkip{
				File: filepath.Base(lfPath), Index: s.Index, Entry: s.Entry,
				Reason: s.Reason, Fix: s.Fix,
			})
		}
	}
	rep.AlreadyCurrent = allCurrent
	if len(rep.Skipped) > 0 {
		rep.NeedsManual = true
		return rep, nil // 任何 skipped ⇒ 一个字节都不写
	}
	if dryRun {
		for i := range rep.Files {
			if rep.Files[i].data != nil {
				rep.Files[i].Backup = rep.Files[i].Path + ".bak"
			}
		}
		return rep, nil
	}
	for i := range rep.Files {
		fc := &rep.Files[i]
		if fc.data == nil {
			continue
		}
		backup := fc.Path + ".bak"
		if err := os.WriteFile(backup, fc.original, 0o644); err != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid, "back up "+fc.Path,
				"check directory permissions, then run `ngm migrate` again", err)
		}
		if err := atomicfile.Write(fc.Path, fc.data); err != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid, "write migrated "+fc.Path,
				"the original is preserved at "+backup, err)
		}
		fc.Backup = backup
	}
	return rep, nil
}

// manifestMigration 是项目清单的迁移结果。
type manifestMigration struct {
	AlreadyCurrent bool
	FromVersion    int
	Entries        int
	Data           []byte
	Skipped        []migrateSkip
}

var v2ManifestFields = map[string]bool{
	"schemaVersion": true, "name": true, "version": true, "runtime": true,
	"main": true, "types": true, "publish": true, "dependencies": true,
	"engines": true, "vendor": true, "supplyChain": true, "scripts": true,
}

var v2ManifestDepFields = map[string]bool{
	"name": true, "ref": true, "refType": true, "path": true,
	"sourcePath": true, "localRepo": true,
}

// migrateManifest 把 v1/v2 清单映射成 v3 清单。
//
// 清单字段与 v3 同名的部分原样保留；只改 schemaVersion，并对无法证明的条目
// 产出 skipped（本地来源字段在 v1/v2 里不可能有确定语义，禁止猜）。
func migrateManifest(data []byte, path string) (*manifestMigration, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse "+path+" for migration (is it valid JSON?)",
			"fix the JSON syntax in "+path+", then run `ngm migrate` again", err)
	}
	from := 0
	if b, ok := top["schemaVersion"]; ok {
		if err := json.Unmarshal(b, &from); err != nil {
			return nil, errs.New(errs.CodeConfigInvalid,
				"invalid schemaVersion in "+path, "set schemaVersion to 1, 2 or 3")
		}
	}
	if from == 0 || from == config.SchemaVersion {
		return &manifestMigration{AlreadyCurrent: true, FromVersion: from}, nil
	}
	if from != 1 && from != 2 {
		return nil, errs.New(errs.CodeConfigInvalid,
			fmt.Sprintf("migrate supports schemaVersion 1/2 only (got %d)", from),
			"this command maps v1/v2 → v3; a newer manifest needs a matching ngm build")
	}
	for k := range top {
		if !v2ManifestFields[k] {
			return nil, errs.New(errs.CodeConfigInvalid,
				"unknown top-level field "+k+" in "+path,
				"remove the field or regenerate the manifest; migrate does not guess unknown field semantics")
		}
	}
	out := &manifestMigration{FromVersion: from}
	if rawDeps, ok := top["dependencies"]; ok {
		var deps []map[string]json.RawMessage
		if err := json.Unmarshal(rawDeps, &deps); err != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid, "parse dependencies in "+path, "fix the dependencies array in ngm.json, then re-run migrate", err)
		}
		for i, fields := range deps {
			reason := mapV2ManifestDep(i, fields)
			if reason != "" {
				out.Skipped = append(out.Skipped, migrateSkip{
					File: filepath.Base(path), Index: i, Reason: reason,
					Fix: "fix the declaration in ngm.json (or re-add the dependency), then run `ngm migrate` again",
				})
				continue
			}
			out.Entries++
		}
		top["dependencies"] = mustJSON(deps)
	}
	top["schemaVersion"] = mustJSON(config.SchemaVersion)
	data, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "marshal migrated "+path, "report this as a bug: in-memory migration data could not be encoded", err)
	}
	out.Data = append(data, '\n')
	return out, nil
}

func mapV2ManifestDep(index int, fields map[string]json.RawMessage) string {
	label := fmt.Sprintf("dependencies[%d]", index)
	for k := range fields {
		if !v2ManifestDepFields[k] {
			return label + " contains unknown field " + k + "; its v3 meaning cannot be proven"
		}
	}
	var dep struct {
		Name       string `json:"name"`
		Ref        string `json:"ref"`
		RefType    string `json:"refType"`
		Path       string `json:"path"`
		SourcePath string `json:"sourcePath"`
		LocalRepo  string `json:"localRepo"`
	}
	if b := mustJSON(fields); json.Unmarshal(b, &dep) != nil {
		return label + " has a field of the wrong type"
	}
	if dep.SourcePath != "" || dep.LocalRepo != "" {
		return label + " (" + dep.Name + ") carries v3-only local-source fields; a v1/v2 manifest cannot prove them"
	}
	if dep.Name == "" || dep.Ref == "" {
		return label + " is missing name or ref"
	}
	if rt := resolve.RefType(dep.RefType); dep.RefType != "" && (!rt.IsValid() || rt.IsLocal()) {
		return label + " (" + dep.Name + ") has refType " + dep.RefType + ", which is not a git ref type"
	}
	if dep.Path != "" {
		if err := resolve.ValidateSubPath(dep.Path); err != nil {
			return label + " (" + dep.Name + ") has an unsafe path: " + err.Error()
		}
	}
	return ""
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("migrate: cannot marshal JSON value: " + err.Error())
	}
	return b
}
