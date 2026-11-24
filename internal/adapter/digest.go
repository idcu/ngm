package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/idcu/ngm/internal/core/errs"
)

// sha256Digest 与 internal/receipt.Digest 同一口径：sha256:<64 hex>。
func sha256Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ToolDigest 返回可执行文件**字节**的摘要（sha256:…），不 spawn 任何进程。
//
// 为什么不用 sha256sum / shasum：那些工具在 Windows 上并不存在（spec §5 的风险表），
// 而且 spawn 外部命令会绕过 run: 闸门。Go 标准库读文件即可，三平台一致。
//
// program 是清单里的程序名/路径：先 LookPath 解析（相对名查 PATH，路径原样用），
// 再读文件字节。找不到时返回 CodeEngineNotFound（exit 5），hint 点名缺哪个工具——
// "缺引擎"与"产物不符"必须是两个可分辨的结论（E4/E5、ADR-039）。
func ToolDigest(program string) (string, error) {
	if program == "" {
		return "", errs.New(errs.CodeEngineNotFound,
			"engine tool is empty",
			"set `command` in ngm.engines.json to the engine executable")
	}
	path, err := exec.LookPath(program)
	if err != nil {
		return "", errs.New(errs.CodeEngineNotFound,
			"engine tool not found: "+program,
			"install "+program+" (or point `command` in ngm.engines.json at an installed executable), "+
				"then re-run the command")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errs.New(errs.CodeEngineNotFound,
			fmt.Sprintf("cannot read engine tool %s", program),
			"check that the executable at "+path+" is readable")
	}
	return sha256Digest(data), nil
}

// FileDigest 返回给定路径的字节摘要，路径相对 baseDir 解析。
//
// 用于 wasm 模块（command 指向本地 .wasm 文件）与 embed 自描述：两者都不是
// PATH 上的程序，但同样需要"工具字节"身份，而不是把路径写进回执。
func FileDigest(baseDir, path string) (string, error) {
	p := path
	if p != "" && !filepath.IsAbs(p) && baseDir != "" {
		p = filepath.Join(baseDir, p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", errs.New(errs.CodeEngineNotFound,
			fmt.Sprintf("engine file not found: %s", path),
			"point `command` in ngm.engines.json at an existing engine file")
	}
	return sha256Digest(data), nil
}
