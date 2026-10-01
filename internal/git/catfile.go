package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/idcu/ngm/internal/errs"
)

// CatFileBatch 批量读取对象内容（`git cat-file --batch`）。
//
// 为什么需要批量协议：一个中等规模依赖有数百个文件，逐文件启动 git 子进程
// 会产生数百次进程创建（Windows 上尤其昂贵）。--batch 用单个进程完成全部读取。
//
// 协议（每行输入一个 object name）：
//
//	<sha> <type> <size>\n
//	<size 字节内容>\n
//
// 对象不存在时输出 `<name> missing\n`。
//
// 返回值与 shas 等长且顺序一致；任一对象缺失即整体失败（调用方已保证 SHA 来自
// 同一 commit 的 ls-tree，缺失说明 mirror 损坏）。
func CatFileBatch(ctx context.Context, opts Options, repoPath string, shas []string) ([][]byte, error) {
	if len(shas) == 0 {
		return nil, nil
	}

	// 与 Run / RunAllowFailure 共用同一个构造点（门禁 + 计数）——
	// 这里此前同样**没有** `run:git` 门禁，于是 digest 重放会在被禁止的配置下照跑。
	cmd, cerr := newGitCommand(ctx, opts, repoPath, "cat-file", "--batch")
	if cerr != nil {
		return nil, cerr
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, errs.Wrap(errs.CodeGitFetch, "cat-file: open stdin", "", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errs.Wrap(errs.CodeGitFetch, "cat-file: open stdout", "", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// 计数**不在这里**：`newGitCommand` 已经记过 1 次了（cat-file --batch 是长生命周期的
	// 子进程，一次 spawn 服务很多次读取——那个"一次"正是它记的）。
	//
	// 这里此前还留着 v0.5 A2 时期的一句 noteSpawn 调用（那时本函数自己 exec，需要自己记）。
	// C 组把构造收进 `newGitCommand` 之后，那一句变成了**对同一个进程记两次账**：
	// 每次 digest 重放都多算 1，于是"commit 2 次/依赖、tag 3 次/依赖"被测成 3 与 4。
	//
	// 为什么当时没被发现：门禁断言（`TestSpawn_GateOnEveryExit`）测的是"拒绝了几个"，
	// 计数偏高它看不出来；而唯一的读数只被 `t.Logf` 打印，不作断言。
	// 现在由 `TestV06VerifySpawnBudget`（预算）与 `TestSpawn_OnlyOneFileCountsSpawns`
	// （计账点唯一）两条一起守住：一条管"起了几个"，一条管"只能在一个地方记"。
	if err := cmd.Start(); err != nil {
		return nil, errs.Wrap(errs.CodeGitFetch, "cat-file: start git", NotInstalledHint, err)
	}

	// 写入请求。必须与读取并发进行：否则当请求量超过管道缓冲区时，
	// 子进程会阻塞在写 stdout、父进程阻塞在写 stdin，形成死锁。
	writeErr := make(chan error, 1)
	go func() {
		defer stdin.Close()
		w := bufio.NewWriter(stdin)
		for _, sha := range shas {
			if _, werr := w.WriteString(sha + "\n"); werr != nil {
				writeErr <- werr
				return
			}
		}
		writeErr <- w.Flush()
	}()

	out, readErr := readCatFileBatch(stdout, len(shas))

	// 无论读取是否成功都要收尾，避免泄漏进程
	waitErr := cmd.Wait()

	if readErr != nil {
		return nil, readErr
	}
	if werr := <-writeErr; werr != nil {
		return nil, errs.Wrap(errs.CodeGitFetch, "cat-file: write requests", "", werr)
	}
	if waitErr != nil {
		return nil, errs.New(errs.CodeGitFetch,
			fmt.Sprintf("cat-file --batch failed: %s", redact(trimStderr(stderrBuf.Bytes()), opts.Secrets)),
			"the local mirror may be corrupt; delete it and re-fetch")
	}
	return out, nil
}

// readCatFileBatch 按协议读取 n 个对象。
func readCatFileBatch(r io.Reader, n int) ([][]byte, error) {
	br := bufio.NewReaderSize(r, 64*1024)
	out := make([][]byte, 0, n)

	for i := 0; i < n; i++ {
		header, err := br.ReadString('\n')
		if err != nil {
			if err == io.EOF && header == "" {
				return nil, errs.New(errs.CodeGitFetch,
					fmt.Sprintf("cat-file: unexpected end of stream after %d/%d objects", i, n),
					"the local mirror may be truncated; delete it and re-fetch")
			}
			return nil, errs.Wrap(errs.CodeGitFetch, "cat-file: read header", "", err)
		}
		header = strings.TrimRight(header, "\r\n")

		parts := strings.SplitN(header, " ", 3)
		if len(parts) == 2 && parts[1] == "missing" {
			return nil, errs.New(errs.CodeGitFetch,
				fmt.Sprintf("cat-file: object %s is missing from the mirror", parts[0]),
				"the local mirror is incomplete; delete it and re-run to re-fetch")
		}
		if len(parts) != 3 {
			return nil, errs.New(errs.CodeGitFetch,
				fmt.Sprintf("cat-file: malformed header %q", header),
				"unexpected git version or corrupt mirror")
		}

		size, perr := strconv.ParseInt(parts[2], 10, 64)
		if perr != nil {
			return nil, errs.Wrap(errs.CodeGitFetch,
				fmt.Sprintf("cat-file: malformed size in %q", header), "", perr)
		}

		content := make([]byte, size)
		if _, rerr := io.ReadFull(br, content); rerr != nil {
			return nil, errs.Wrap(errs.CodeGitFetch,
				fmt.Sprintf("cat-file: read %d bytes of %s", size, parts[0]), "", rerr)
		}
		// 内容之后的单个 LF
		if _, rerr := br.ReadByte(); rerr != nil {
			return nil, errs.Wrap(errs.CodeGitFetch, "cat-file: read trailing LF", "", rerr)
		}
		out = append(out, content)
	}
	return out, nil
}

// GetBlob 读取单个对象内容（`git cat-file blob <sha>`）。
//
// 适用于条目很少的场景（如单文件校验）；批量场景请用 CatFileBatch。
func GetBlob(ctx context.Context, opts Options, repoPath, sha string) ([]byte, error) {
	o := opts
	o.Dir = repoPath
	res, err := Run(ctx, o, "cat-file", "blob", sha)
	if err != nil {
		return nil, err
	}
	return res.Stdout, nil
}
