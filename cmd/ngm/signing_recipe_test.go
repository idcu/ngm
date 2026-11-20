package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestV141SigningRecipeIsReproducible —— C 批判据（竞品全维度评估 C1）。
//
// ADR-014/027 定下"ngm 只产被签对象、不签名"，但那条路此前只在 ADR 的文字里，
// 没有一份能照着跑完的配方。本判据守 docs/guides/signing.md 这份配方：
//  1. 存在；
//  2. 命令序列完整：ngm attest → cosign attest-blob → cosign verify-blob-attestation；
//  3. 验证步骤带 certificate-identity / oidc-issuer（否则任何人都能造可接受的签名）；
//  4. 不含"让 ngm 持有/管理密钥"的措辞（那会违反 ADR-014/027）。
//
// 注意：本判据守的是**配方文档**，不冒充跑过签名——本机没有 cosign 是常态。
func TestV141SigningRecipeIsReproducible(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "guides", "signing.md"))
	if err != nil {
		t.Fatalf("签名配方文档缺失: %v", err)
	}
	doc := string(data)
	lower := strings.ToLower(doc)

	for _, seq := range []struct {
		name, needle string
	}{
		{"产出被签语句", "ngm attest"},
		{"cosign 签名", "cosign attest-blob"},
		{"cosign 验证", "cosign verify-blob-attestation"},
		{"钉死签名身份", "--certificate-identity"},
		{"钉死 OIDC issuer", "--certificate-oidc-issuer"},
	} {
		if !strings.Contains(lower, strings.ToLower(seq.needle)) {
			t.Errorf("配方缺 %s（%q）", seq.name, seq.needle)
		}
	}

	// 顺序：先产出、再签、再验（字符串位置递增）。
	iAttest := strings.Index(lower, "ngm attest")
	iSign := strings.Index(lower, "cosign attest-blob")
	iVerify := strings.Index(lower, "cosign verify-blob-attestation")
	if !(iAttest >= 0 && iSign > iAttest && iVerify > iSign) {
		t.Errorf("命令顺序不对：attest=%d sign=%d verify=%d（应为 attest < sign < verify）",
			iAttest, iSign, iVerify)
	}

	// 不含"ngm 自己执行签名"的命令形态；允许"让 ngm 自己签的改动违反 ADR"这类否定句。
	if strings.Contains(lower, "ngm sign-blob") || strings.Contains(lower, "ngm attest --sign") {
		t.Error("配方出现了 ngm 自签命令——违反 ADR-014/027")
	}
	// 必须明说 ngm 不签名/不管密钥（否定声明在场）。
	if !strings.Contains(lower, "不签名") || !strings.Contains(lower, "不管密钥") {
		t.Error("配方没有声明 ngm 不签名、不管密钥——读者会误以为 ngm 自己签")
	}
}
