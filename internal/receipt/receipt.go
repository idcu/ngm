// Package receipt 定义构建回执（ngm.build.json）的 schema 与确定性序列化。
//
// 依据 docs/specs/ecosystem-roadmap-2026-11-24/spec.md §3.1–3.3 与 ADR-057：
//   - 回执是**旁车文件**，不进 ngm.lock；
//   - 不写时间戳、不写绝对路径，同输入两次字节一致；
//   - 它证明"用了什么引擎、按什么参数、产出了什么字节"，**不**承诺可复现构建认证。
package receipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/idcu/ngm/internal/core/atomicfile"
)

const (
	// Version 是回执 schema 版本。未来字段增删必须显式升级并给出兼容策略。
	Version = 1
	// FileName 是旁车回执的文件名（永远写在项目根，与 ngm.lock 并列但不混入）。
	FileName = "ngm.build.json"

	// SourceBuilt 表示本次产物由引擎现场构建。
	SourceBuilt = "built"
	// SourceCache 表示本次产物来自本地缓存。
	SourceCache = "cache"
)

// Input 是一份参与构建的源文件摘要。
type Input struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// Engine 是实际产出产物的引擎身份。
//
// Digest 是**工具文件字节**的摘要（sha256:…），不是路径——路径在不同机器上不同，
// 不能作为可复核的身份。Version 是探测/声明的版本，仅作人读说明。
type Engine struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// Output 是回执记录的产物。
type Output struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// Receipt 是 ngm.build.json 的顶层形状。
//
// 字段顺序即磁盘字节顺序；新增字段一律追加在末尾，避免破坏既有字节契约。
// Source 记录本次产物是现场构建还是缓存命中（spec §3.3 要求可解释）。
type Receipt struct {
	ReceiptVersion int      `json:"receiptVersion"`
	Inputs         []Input  `json:"inputs"`
	Engine         Engine   `json:"engine"`
	Argv           []string `json:"argv"`
	MappingsDigest string   `json:"mappingsDigest"`
	Output         Output   `json:"output"`
	Source         string   `json:"source"`
}

// StdoutPath 是产物走 stdout（没有 --outfile）时 output.path 的取值。
// verify 遇到它时不能重算磁盘摘要，按完整性失败处理。
const StdoutPath = "<stdout>"

// Digest 返回 ngm 统一的摘要字符串：sha256:<64 hex>。
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// DigestFile 返回文件字节的摘要。读不了文件就是错误，绝不返回空摘要冒充成功。
func DigestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s for digest: %w", path, err)
	}
	return Digest(data), nil
}

// Read 读取并校验一份回执。版本不符视为配置错误，不静默接受。
func Read(path string) (*Receipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read receipt %s: %w", path, err)
	}
	var r Receipt
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse receipt %s: %w", path, err)
	}
	if r.ReceiptVersion != Version {
		return nil, fmt.Errorf("unsupported receiptVersion %d in %s (expected %d)", r.ReceiptVersion, path, Version)
	}
	return &r, nil
}

// Write 以确定性字节写回执：固定字段顺序 + 两空格缩进 + 末尾换行。
//
// 先写临时文件再 rename，避免半份 JSON 被 CI 读到。回执不含时间戳与绝对路径，
// 因此同输入两次调用产出完全相同的字节（Source 改变时除外——那是可解释的命中信号）。
func Write(path string, r *Receipt) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode receipt: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(path, data); err != nil {
		return fmt.Errorf("write receipt %s: %w", path, err)
	}
	return nil
}

// CacheKey 计算构建缓存键。
//
// 键的成分（spec §3.3）：输入 digest 列表 + engine digest + argv + 平台三元组 +
// mappingsDigest。输入按 path 排序、argv 按字典序排序，保证 map 遍历不抖动。
// 任何一项变化都会得到不同的键；平台三元组由调用方传入，便于测试"改平台即 miss"。
func (r *Receipt) CacheKey(platform string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "receipt-v%d\n", Version)
	inputs := append([]Input(nil), r.Inputs...)
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Path < inputs[j].Path })
	for _, in := range inputs {
		fmt.Fprintf(&b, "input\t%s\t%s\n", in.Path, in.Digest)
	}
	fmt.Fprintf(&b, "engine\t%s\n", r.Engine.Digest)
	argv := append([]string(nil), r.Argv...)
	sort.Strings(argv)
	for _, a := range argv {
		fmt.Fprintf(&b, "argv\t%s\n", a)
	}
	fmt.Fprintf(&b, "platform\t%s\n", platform)
	fmt.Fprintf(&b, "mappings\t%s\n", r.MappingsDigest)
	return Digest([]byte(b.String()))
}
