package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/core/atomicfile"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
	"github.com/idcu/ngm/internal/core/resolve"
)

const modulesUsage = `ngm modules — inspect a module collection

USAGE:
  ngm modules list [--dir=<dir>] [--json] [--limit=<n>]
  ngm modules validate [--dir=<dir>] [--json] [--strict] [--index=<file>]
  ngm modules index [--dir=<dir>] [--out=<file>] [--dry-run] [--json] [--releases=false] [--shards] [--branch=<name>]

SUBCOMMANDS:
  list       list nested modules (subdirectories containing ngm.json)
  validate   check module manifests, duplicate names, and (with --strict) versions
  index      write a deterministic ngm.collection.json from the module manifests

FLAGS:
  --dir      project directory (default: .)
  --json     machine-readable output on stdout
  --limit    maximum modules to list (default: 1000)
  --strict   require name/version and semver-like version
  --index    compare the scanned modules against an existing collection index
  --out      index path (default: ngm.collection.json)
  --dry-run  print the index instead of writing it
  --releases include version→commit mappings from local tags (default: true)
  --shards   write releases/<module>.json and leave releases out of the root index
  --branch   also update a local index branch (default: ngm-index; local only, no push)

NOTES:
  The repository-root ngm.json is the project manifest, not a collection module.
  Nested ngm.json files are the modules. This command does not resolve dependencies
  and does not touch the network.

EXIT CODES:
  0  the listing/validation completed
  3  configuration or usage error; validate also uses 3 when a module is invalid
`

type moduleInfo struct {
	Name    string `json:"name,omitempty"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
	// Publish 是逐模块发布开关（ADR-037）：nil 跟随全局 --releases；
	// 显式 true/false 完全覆盖全局，不被全局总开关短路。
	Publish *bool `json:"publish,omitempty"`
	// Err 是该模块清单的解析错误（list 会如实标出；validate 让它变成失败）。
	Err string `json:"error,omitempty"`
}

type modulesListReport struct {
	Version   int          `json:"version"`
	Modules   []moduleInfo `json:"modules"`
	Truncated bool         `json:"truncated,omitempty"`
	Limit     int          `json:"limit"`
}

type modulesValidateReport struct {
	Version int          `json:"version"`
	OK      bool         `json:"ok"`
	Modules []moduleInfo `json:"modules"`
	Errors  []string     `json:"errors,omitempty"`
}

type collectionIndex struct {
	SchemaVersion int                                     `json:"schemaVersion"`
	Source        *collectionSource                       `json:"source,omitempty"`
	ReleaseDir    string                                  `json:"releaseDir,omitempty"`
	Modules       []collectionModule                      `json:"modules"`
	Releases      map[string]map[string]collectionRelease `json:"releases,omitempty"`
}

type collectionSource struct {
	// Repo 是远端仓库 slug（无法从 remote URL 推断时省略）。
	Repo string `json:"repo,omitempty"`
	// Branch 是生成索引时的分支名；detached HEAD 时省略。
	Branch string `json:"branch,omitempty"`
	// Commit 是生成索引时的代码 commit；它把索引锚到代码状态。
	Commit string `json:"commit"`
}

type collectionRelease struct {
	Tag    string `json:"tag,omitempty"`
	Commit string `json:"commit"`
}

type collectionModule struct {
	Name    string `json:"name,omitempty"`
	Path    string `json:"path"`
	Version string `json:"version,omitempty"`
}

type modulesIndexReport struct {
	Version      int             `json:"version"`
	Out          string          `json:"out"`
	DryRun       bool            `json:"dryRun,omitempty"`
	Branch       string          `json:"branch,omitempty"`
	BranchCommit string          `json:"branchCommit,omitempty"`
	ReleaseFiles []string        `json:"releaseFiles,omitempty"`
	Index        collectionIndex `json:"index"`
}

type releaseShard struct {
	SchemaVersion int                          `json:"schemaVersion"`
	Module        string                       `json:"module"`
	Releases      map[string]collectionRelease `json:"releases"`
}

var semverLike = regexp.MustCompile(`^v?\d+\.\d+\.\d+([-+][0-9A-Za-z.-]+)?$`)

// runModules 处理 `ngm modules <list|validate>`。
func runModules(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, modulesUsage)
		return 3
	}
	switch args[0] {
	case "list":
		return runModulesList(ctx, args[1:], stdout, stderr)
	case "validate":
		return runModulesValidate(ctx, args[1:], stdout, stderr)
	case "index":
		return runModulesIndex(ctx, args[1:], stdout, stderr)
	default:
		fmt.Fprint(stderr, modulesUsage)
		return 3
	}
}

func runModulesList(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("modules list", stderr)
	dir := fs.String("dir", ".", "project directory")
	jsonOut := fs.Bool("json", false, "machine-readable output")
	limit := fs.Int("limit", 1000, "maximum modules to list")
	fs.Usage = func() { fmt.Fprint(stderr, modulesUsage) }
	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "json", Bool: true}, {Name: "limit"},
	})); err != nil {
		return 3
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, modulesUsage)
		return 3
	}
	if *limit <= 0 {
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"--limit must be a positive integer",
			"use a positive bound, or omit --limit (default 1000)"))
	}

	mods, err := scanModules(*dir)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	rep := modulesListReport{Version: 1, Modules: mods, Limit: *limit}
	if len(rep.Modules) > *limit {
		rep.Modules = rep.Modules[:*limit]
		rep.Truncated = true
	}
	if *jsonOut {
		return writeModulesJSON(stdout, stderr, rep)
	}
	for _, m := range rep.Modules {
		switch {
		case m.Err != "":
			fmt.Fprintf(stdout, "%s\tERROR: %s\n", m.Path, m.Err)
		case m.Version != "":
			fmt.Fprintf(stdout, "%s\t%s@%s\n", m.Path, displayName(m), m.Version)
		default:
			fmt.Fprintf(stdout, "%s\t%s\n", m.Path, displayName(m))
		}
	}
	if rep.Truncated {
		fmt.Fprintf(stdout, "\n(showing first %d modules; use --limit to raise the bound)\n", *limit)
	}
	return 0
}

func runModulesValidate(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("modules validate", stderr)
	dir := fs.String("dir", ".", "project directory")
	jsonOut := fs.Bool("json", false, "machine-readable output")
	strict := fs.Bool("strict", false, "require name/version and semver-like version")
	indexPath := fs.String("index", "", "compare against an existing collection index")
	fs.Usage = func() { fmt.Fprint(stderr, modulesUsage) }
	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "json", Bool: true}, {Name: "strict", Bool: true}, {Name: "index"},
	})); err != nil {
		return 3
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, modulesUsage)
		return 3
	}

	mods, err := scanModules(*dir)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	errsList := validateModules(mods, *strict)
	if *indexPath != "" {
		indexErrs, ierr := validateIndexAgainstModules(*dir, *indexPath, mods)
		if ierr != nil {
			return runErr(ctx, stdout, stderr, ierr)
		}
		errsList = append(errsList, indexErrs...)
		sort.Strings(errsList)
	}
	rep := modulesValidateReport{
		Version: 1,
		OK:      len(errsList) == 0,
		Modules: mods,
		Errors:  errsList,
	}
	if *jsonOut {
		writeErr := writeModulesJSON(stdout, stderr, rep)
		if writeErr != 0 {
			return writeErr
		}
	} else {
		for _, e := range errsList {
			fmt.Fprintf(stdout, "✗ %s\n", e)
		}
		if len(errsList) == 0 {
			fmt.Fprintf(stdout, "OK: %d module(s)\n", len(mods))
		}
	}
	if len(errsList) > 0 {
		return 3
	}
	return 0
}

func runModulesIndex(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("modules index", stderr)
	dir := fs.String("dir", ".", "project directory")
	out := fs.String("out", "ngm.collection.json", "index path")
	dryRun := fs.Bool("dry-run", false, "print the index instead of writing it")
	jsonOut := fs.Bool("json", false, "machine-readable output")
	releases := fs.Bool("releases", true, "include version→commit mappings from local tags")
	shards := fs.Bool("shards", false, "write releases/<module>.json instead of inline releases")
	branch := fs.String("branch", "ngm-index", "local index branch to update (empty disables)")
	fs.Usage = func() { fmt.Fprint(stderr, modulesUsage) }
	if err := fs.Parse(normalizeArgs(args, []flagSpec{
		{Name: "dir"}, {Name: "out"}, {Name: "dry-run", Bool: true},
		{Name: "json", Bool: true}, {Name: "releases", Bool: true}, {Name: "shards", Bool: true},
		{Name: "branch"},
	})); err != nil {
		return 3
	}
	branchSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "branch" {
			branchSet = true
		}
	})
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, modulesUsage)
		return 3
	}

	mods, err := scanModules(*dir)
	if err != nil {
		return runErr(ctx, stdout, stderr, err)
	}
	if problems := validateModules(mods, false); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintf(stderr, "✗ %s\n", p)
		}
		return runErr(ctx, stdout, stderr, errs.New(
			errs.CodeConfigInvalid,
			"cannot write a collection index with invalid modules",
			"fix the module manifests, then re-run `ngm modules index`"))
	}

	idx := collectionIndex{SchemaVersion: 1, Modules: make([]collectionModule, 0, len(mods))}
	for _, m := range mods {
		idx.Modules = append(idx.Modules, collectionModule{Name: m.Name, Path: m.Path, Version: m.Version})
	}
	var releaseFiles map[string][]byte
	releaseMods := modulesForRelease(mods, *releases)
	if len(releaseMods) > 0 {
		rel, rerr := collectReleases(ctx, *dir, releaseMods)
		if rerr != nil {
			return runErr(ctx, stdout, stderr, rerr)
		}
		if *shards {
			releaseFiles, rerr = buildReleaseShards(rel)
			if rerr != nil {
				return runErr(ctx, stdout, stderr, rerr)
			}
			if len(releaseFiles) > 0 {
				idx.ReleaseDir = "releases"
			}
		} else {
			idx.Releases = rel
		}
	}
	src, serr := collectSource(ctx, *dir)
	if serr != nil {
		return runErr(ctx, stdout, stderr, serr)
	}
	idx.Source = src
	data, merr := json.MarshalIndent(idx, "", "  ")
	if merr != nil {
		fmt.Fprintf(stderr, "internal error: marshal collection index: %v\n", merr)
		return 6
	}
	data = append(data, '\n')

	relOut := *out
	absOut := relOut
	if !filepath.IsAbs(absOut) {
		absOut = filepath.Join(*dir, absOut)
	}
	releasePaths := sortedByteMapKeys(releaseFiles)

	if *dryRun {
		if _, werr := stdout.Write(data); werr != nil {
			fmt.Fprintf(stderr, "cannot write index: %v\n", werr)
			return 6
		}
		if len(releaseFiles) > 0 {
			fmt.Fprintf(stderr, "dry-run: %d release shard(s) would be written under %s\n",
				len(releaseFiles), filepath.Join(filepath.Dir(absOut), "releases"))
		}
		return 0
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o755); err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			"create index directory for "+absOut, "check directory permissions", err))
	}
	if err := atomicfile.Write(absOut, data); err != nil {
		return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
			"write collection index "+absOut, "check free disk space and permissions", err))
	}
	for _, relPath := range releasePaths {
		abs := filepath.Join(filepath.Dir(absOut), filepath.FromSlash(relPath))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
				"create release shard directory", "check directory permissions", err))
		}
		if err := atomicfile.Write(abs, releaseFiles[relPath]); err != nil {
			return runErr(ctx, stdout, stderr, errs.Wrap(errs.CodeConfigInvalid,
				"write release shard "+abs, "check free disk space and permissions", err))
		}
	}

	branchFiles := map[string][]byte{"ngm.collection.json": data}
	for relPath, content := range releaseFiles {
		branchFiles[relPath] = content
	}
	var branchCommit string
	if *branch != "" {
		if !isGitDir(ctx, *dir) {
			if branchSet {
				return runErr(ctx, stdout, stderr, errs.New(
					errs.CodeConfigInvalid,
					"--branch requires a Git repository",
					"run inside a Git repo, or pass --branch= to disable the index branch"))
			}
		} else {
			branchCommit, err = writeIndexBranch(ctx, *dir, *branch, branchFiles)
			if err != nil {
				return runErr(ctx, stdout, stderr, err)
			}
		}
	}
	if *jsonOut {
		return writeModulesJSON(stdout, stderr, modulesIndexReport{
			Version: 1, Out: relOut, Branch: *branch, BranchCommit: branchCommit,
			ReleaseFiles: releasePaths, Index: idx,
		})
	}
	if branchCommit != "" {
		fmt.Fprintf(stdout, "wrote %s (%d modules) + %d release shard(s); updated branch %s at %s\n",
			relOut, len(idx.Modules), len(releaseFiles), *branch, git.ShortSHA(branchCommit))
		return 0
	}
	fmt.Fprintf(stdout, "wrote %s (%d modules) + %d release shard(s)\n",
		relOut, len(idx.Modules), len(releaseFiles))
	return 0
}

func buildReleaseShards(rel map[string]map[string]collectionRelease) (map[string][]byte, error) {
	if len(rel) == 0 {
		return nil, nil
	}
	out := make(map[string][]byte, len(rel))
	for module, versions := range rel {
		data, err := json.MarshalIndent(releaseShard{
			SchemaVersion: 1,
			Module:        module,
			Releases:      versions,
		}, "", "  ")
		if err != nil {
			return nil, errs.Wrap(errs.CodeConfigInvalid, "marshal release shard for "+module,
				"this is an internal error; report the module path", err)
		}
		out[filepath.ToSlash(filepath.Join("releases", module+".json"))] = append(data, '\n')
	}
	return out, nil
}

func sortedByteMapKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// modulesForRelease 计算本次索引要收集 release 的模块集合。
//
// 逐模块 Publish 是**完全覆盖**：显式 true/false 直接决定，不再与
// 全局 --releases 做 AND/OR（ADR-037）。nil 才跟随全局默认。
func modulesForRelease(mods []moduleInfo, global bool) []moduleInfo {
	out := make([]moduleInfo, 0, len(mods))
	for _, m := range mods {
		enabled := global
		if m.Publish != nil {
			enabled = *m.Publish
		}
		if enabled {
			out = append(out, m)
		}
	}
	return out
}

// collectReleases 从本地 Git tags 收集 `<modulePath>/v<version>` → commit。
//
// 不是 Git 仓库时返回 nil（索引仍可生成）；Git 命令失败则报错，不静默写出空映射。
func collectReleases(ctx context.Context, dir string, mods []moduleInfo) (map[string]map[string]collectionRelease, error) {
	gitOpts := git.Options{Dir: dir}
	if _, err := git.Run(ctx, gitOpts, "rev-parse", "--git-dir"); err != nil {
		return nil, nil // 普通目录：没有 releases，不是错误
	}
	res, err := git.Run(ctx, gitOpts, "tag", "--list")
	if err != nil {
		return nil, err
	}
	tags := strings.Fields(string(res.Stdout))
	byPath := make(map[string]map[string]collectionRelease)
	for _, tag := range tags {
		for _, m := range mods {
			prefix := m.Path + "/v"
			if !strings.HasPrefix(tag, prefix) {
				continue
			}
			version := strings.TrimPrefix(tag, prefix)
			if !semverLike.MatchString(version) {
				continue
			}
			commit, rerr := git.RevParse(ctx, gitOpts, dir, tag)
			if rerr != nil {
				return nil, errs.Wrap(errs.CodeConfigInvalid,
					"resolve module release tag "+tag,
					"check that the tag points at a commit in this repository", rerr)
			}
			if byPath[m.Path] == nil {
				byPath[m.Path] = map[string]collectionRelease{}
			}
			byPath[m.Path][version] = collectionRelease{Tag: tag, Commit: commit}
			break
		}
	}
	if len(byPath) == 0 {
		return nil, nil
	}
	return byPath, nil
}

// isGitDir 报告 dir 是否位于一个 Git 仓库内。
func isGitDir(ctx context.Context, dir string) bool {
	res, err := git.RunAllowFailure(ctx, git.Options{Dir: dir}, "rev-parse", "--git-dir")
	return err == nil && res.ExitCode == 0
}

// collectSource 把索引锚到代码 commit；不是 Git 仓库或无 HEAD 时省略。
func collectSource(ctx context.Context, dir string) (*collectionSource, error) {
	if !isGitDir(ctx, dir) {
		return nil, nil
	}
	gitOpts := git.Options{Dir: dir}
	commit, err := git.RevParse(ctx, gitOpts, dir, "HEAD")
	if err != nil {
		// 空仓库（还没有 commit）可以生成索引，但没有可锚定的源码状态。
		return nil, nil
	}
	src := &collectionSource{Commit: commit}
	if res, rerr := git.Run(ctx, gitOpts, "rev-parse", "--abbrev-ref", "HEAD"); rerr == nil {
		if b := strings.TrimSpace(string(res.Stdout)); b != "" && b != "HEAD" {
			src.Branch = b
		}
	}
	if res, rerr := git.Run(ctx, gitOpts, "remote", "get-url", "origin"); rerr == nil {
		url := strings.TrimSpace(string(res.Stdout))
		if slug, perr := resolve.ParseSlug(url); perr == nil {
			src.Repo = slug.Slug()
		}
	}
	return src, nil
}

// writeIndexBranch 在本地创建/更新一个只含索引文件的树（根索引 + 可选分片）。
//
// 它**不推送**：分支发布由 CI/用户显式完成。commit message 记录代码 commit，
// 让“索引快照 ↔ 代码状态”的关系可查。批量写入用 `hash-object --stdin-paths`
// 与 `update-index --index-info`，避免为每个分片启动一个 git 子进程。
func writeIndexBranch(ctx context.Context, dir, branch string, files map[string][]byte) (string, error) {
	gitOpts := git.Options{Dir: dir}
	if res, err := git.RunAllowFailure(ctx, gitOpts, "check-ref-format", "--branch", branch); err != nil || res.ExitCode != 0 {
		return "", errs.New(errs.CodeConfigInvalid,
			"invalid branch name "+branch,
			"use a valid Git branch name, e.g. ngm-index")
	}
	if len(files) == 0 {
		return "", errs.New(errs.CodeConfigInvalid,
			"index branch would be empty",
			"this is an internal error; report the empty index file set")
	}

	paths := make([]string, 0, len(files))
	for rel := range files {
		if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
			return "", errs.New(errs.CodeConfigInvalid,
				"invalid index file path "+strconv.Quote(rel),
				"index paths must be repository-relative POSIX paths")
		}
		for _, part := range strings.Split(rel, "/") {
			if part == "" || part == "." || part == ".." {
				return "", errs.New(errs.CodeConfigInvalid,
					"invalid index file path "+strconv.Quote(rel),
					"index paths must not contain empty, `.` or `..` components")
			}
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)

	tmpRoot, err := os.MkdirTemp("", "ngm-index-tree-*")
	if err != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid, "create temp index tree",
			"check the temp directory", err)
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()

	absPaths := make([]string, 0, len(paths))
	for _, rel := range paths {
		abs := filepath.Join(tmpRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", errs.Wrap(errs.CodeConfigInvalid, "create temp index directory",
				"check the temp directory", err)
		}
		if err := os.WriteFile(abs, files[rel], 0o644); err != nil {
			return "", errs.Wrap(errs.CodeConfigInvalid, "write temp index file",
				"check free disk space", err)
		}
		absPaths = append(absPaths, abs)
	}

	// 一次 hash-object --stdin-paths：文件数从 N 次 spawn 降到 1 次。
	hashRes, err := git.RunWithStdin(ctx, gitOpts,
		[]byte(strings.Join(absPaths, "\n")+"\n"),
		"hash-object", "-w", "--stdin-paths")
	if err != nil {
		return "", errs.Wrap(errs.CodeGitFetch, "hash index blobs",
			"check repository permissions", err)
	}
	blobLines := strings.Split(strings.TrimSpace(string(hashRes.Stdout)), "\n")
	if len(blobLines) != len(paths) {
		return "", errs.New(errs.CodeGitFetch,
			fmt.Sprintf("hash-object returned %d blobs for %d files", len(blobLines), len(paths)),
			"this is an internal error; report it")
	}

	tmpIndex, err := os.CreateTemp("", "ngm-collection-index-*.idx")
	if err != nil {
		return "", errs.Wrap(errs.CodeConfigInvalid, "create temp index",
			"check the temp directory", err)
	}
	tmpIndexName := tmpIndex.Name()
	tmpIndex.Close()
	defer func() { _ = os.Remove(tmpIndexName) }()

	indexOpts := git.Options{Dir: dir, ExtraEnv: []string{"GIT_INDEX_FILE=" + tmpIndexName}}
	if _, err := git.Run(ctx, indexOpts, "read-tree", "--empty"); err != nil {
		return "", errs.Wrap(errs.CodeGitFetch, "prepare the index tree",
			"check repository permissions", err)
	}
	var info strings.Builder
	for i, rel := range paths {
		info.WriteString("100644 blob ")
		info.WriteString(blobLines[i])
		info.WriteByte('\t')
		info.WriteString(rel)
		info.WriteByte('\n')
	}
	if _, err := git.RunWithStdin(ctx, indexOpts, []byte(info.String()),
		"update-index", "--index-info"); err != nil {
		return "", errs.Wrap(errs.CodeGitFetch, "stage the index files",
			"check repository permissions", err)
	}
	treeRes, err := git.Run(ctx, indexOpts, "write-tree")
	if err != nil {
		return "", errs.Wrap(errs.CodeGitFetch, "write the index tree",
			"check repository permissions", err)
	}
	tree := strings.TrimSpace(string(treeRes.Stdout))

	parent := ""
	if res, perr := git.RunAllowFailure(ctx, gitOpts, "rev-parse", "--verify",
		"refs/heads/"+branch); perr == nil && res.ExitCode == 0 {
		parent = strings.TrimSpace(string(res.Stdout))
	}

	msg := "ngm index"
	if src, _ := collectSource(ctx, dir); src != nil && src.Commit != "" {
		msg += ": " + src.Commit
	}
	commitArgs := []string{"commit-tree", tree, "-m", msg}
	if parent != "" {
		commitArgs = append(commitArgs, "-p", parent)
	}
	commitRes, err := git.Run(ctx, gitOpts, commitArgs...)
	if err != nil {
		return "", errs.Wrap(errs.CodeGitFetch, "create the index commit",
			"check that git user.name and user.email are configured", err)
	}
	commit := strings.TrimSpace(string(commitRes.Stdout))

	ref := "refs/heads/" + branch
	if parent != "" {
		_, err = git.Run(ctx, gitOpts, "update-ref", ref, commit, parent)
	} else {
		_, err = git.Run(ctx, gitOpts, "update-ref", ref, commit)
	}
	if err != nil {
		return "", errs.Wrap(errs.CodeGitFetch, "update the index branch "+ref,
			"check repository permissions", err)
	}
	return commit, nil
}

// validateIndexAgainstModules 防止索引与目录漂移：多一个、少一个、字段不一致都报。
func validateIndexAgainstModules(dir, indexPath string, mods []moduleInfo) ([]string, error) {
	absIndex := indexPath
	if !filepath.IsAbs(absIndex) {
		absIndex = filepath.Join(dir, absIndex)
	}
	data, err := os.ReadFile(absIndex)
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"read collection index "+absIndex,
			"run `ngm modules index` to create it, or pass --index with an existing file", err)
	}
	var idx collectionIndex
	if uerr := json.Unmarshal(data, &idx); uerr != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid,
			"parse collection index "+absIndex,
			"the index is not valid JSON; re-run `ngm modules index`", uerr)
	}
	if idx.SchemaVersion != 1 {
		return nil, errs.New(errs.CodeConfigInvalid,
			"collection index has unsupported schemaVersion "+strconv.Itoa(idx.SchemaVersion),
			"this build writes and reads schemaVersion 1")
	}

	indexed := make(map[string]collectionModule, len(idx.Modules))
	for _, m := range idx.Modules {
		if _, dup := indexed[m.Path]; dup {
			return nil, errs.New(errs.CodeConfigInvalid,
				"collection index has duplicate module path "+m.Path,
				"re-run `ngm modules index`; the generated index is sorted and path-unique")
		}
		indexed[m.Path] = m
	}
	seen := make(map[string]bool, len(mods))
	var problems []string
	for _, m := range mods {
		seen[m.Path] = true
		im, ok := indexed[m.Path]
		if !ok {
			problems = append(problems, "index: "+m.Path+" is missing from the collection index")
			continue
		}
		if im.Name != m.Name || im.Version != m.Version {
			problems = append(problems, fmt.Sprintf(
				"index: %s disagrees with the manifest (index: name=%q version=%q; manifest: name=%q version=%q)",
				m.Path, im.Name, im.Version, m.Name, m.Version))
		}
	}
	var extra []string
	for path := range indexed {
		if !seen[path] {
			extra = append(extra, "index: "+path+" does not exist under "+dir)
		}
	}
	sort.Strings(extra)
	problems = append(problems, extra...)

	for path := range idx.Releases {
		if _, ok := indexed[path]; !ok {
			problems = append(problems, "index: releases reference unknown module path "+path)
		}
	}
	sort.Strings(problems)
	return problems, nil
}

// scanModules 找出 root 下嵌套的 ngm.json（排除 root 自身与依赖/构建目录）。
func scanModules(root string) ([]moduleInfo, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "resolve --dir "+root,
			"check the directory path", err)
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return nil, errs.New(errs.CodeConfigInvalid,
			"not a directory: "+root,
			"pass an existing project directory to --dir")
	}

	var out []moduleInfo
	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != abs && skipModuleWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "ngm.json" {
			return nil
		}
		rel, rerr := filepath.Rel(abs, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if rel == "ngm.json" {
			return nil // 根清单是项目本身，不是集合模块
		}
		m := moduleInfo{Path: filepath.ToSlash(filepath.Dir(rel))}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			m.Err = rerr.Error()
			out = append(out, m)
			return nil
		}
		var f struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Publish *bool  `json:"publish"`
		}
		if uerr := json.Unmarshal(data, &f); uerr != nil {
			m.Err = "invalid JSON: " + uerr.Error()
		} else {
			m.Name = f.Name
			m.Version = f.Version
			m.Publish = f.Publish
		}
		out = append(out, m)
		return nil
	})
	if walkErr != nil {
		return nil, errs.Wrap(errs.CodeConfigInvalid, "scan modules under "+root,
			"check directory permissions", walkErr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func skipModuleWalkDir(name string) bool {
	switch name {
	case ".git", ".ngm", "node_modules", "ngm.vendor", "vendor", "dist":
		return true
	default:
		return false
	}
}

func validateModules(mods []moduleInfo, strict bool) []string {
	var problems []string
	byName := map[string][]string{}
	for _, m := range mods {
		prefix := m.Path + ": "
		if m.Err != "" {
			problems = append(problems, prefix+m.Err)
			continue
		}
		if m.Name == "" {
			if strict {
				problems = append(problems, prefix+"missing `name`")
			}
		} else {
			byName[m.Name] = append(byName[m.Name], m.Path)
		}
		if strict {
			if m.Version == "" {
				problems = append(problems, prefix+"missing `version`")
			} else if !semverLike.MatchString(m.Version) {
				problems = append(problems, prefix+"version "+m.Version+" is not semver-like")
			}
		}
	}
	var dupes []string
	for name, paths := range byName {
		if len(paths) > 1 {
			dupes = append(dupes, fmt.Sprintf("duplicate module name %q: %v", name, paths))
		}
	}
	sort.Strings(dupes)
	problems = append(problems, dupes...)
	sort.Strings(problems)
	return problems
}

func displayName(m moduleInfo) string {
	if m.Name != "" {
		return m.Name
	}
	return "(unnamed)"
}

func writeModulesJSON(stdout, stderr io.Writer, v any) int {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "internal error: marshal modules report: %v\n", err)
		return 6
	}
	if _, err := stdout.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "cannot write modules report: %v\n", err)
		return 6
	}
	return 0
}
