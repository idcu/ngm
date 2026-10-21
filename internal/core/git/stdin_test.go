package git

import (
	"context"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/testutils"
)

// RunWithStdin 必须走同一条 newGitCommand 门禁/计数路径，且真的把 stdin 喂给 git。
func TestRunWithStdin(t *testing.T) {
	testutils.MustHaveGit(t)
	repo := testutils.NewGitRepo(t)

	res, err := RunWithStdin(context.Background(), Options{Dir: repo.Dir},
		[]byte("hello from stdin\n"), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatalf("RunWithStdin: %v", err)
	}
	sha := strings.TrimSpace(string(res.Stdout))
	if len(sha) != 40 {
		t.Fatalf("hash-object returned %q", sha)
	}

	cat, err := Run(context.Background(), Options{Dir: repo.Dir}, "cat-file", "-p", sha)
	if err != nil {
		t.Fatalf("cat-file: %v", err)
	}
	if string(cat.Stdout) != "hello from stdin\n" {
		t.Errorf("blob content = %q", cat.Stdout)
	}
}
