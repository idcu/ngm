package vendor

import (
	"context"
	"testing"

	"github.com/idcu/ngm/internal/core/digest"
	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
)

// v0.81：调用方传入 Material 时，Put 的 digest 校验必须由**内容**派生。
//
// v0.68 把 install 路径上重复的那 4 个 spawn 拿掉了（digest 此前算两遍），
// 代价是 Put 里那道 `material.Digest != meta.Digest` 变成了**同义反复**——
// install 的两个值都来自同一次 BuildArchiveMaterial（`dg := material.Digest`）。
// 于是"防止把 A 的内容写进 B 的槽位"这条 store 的硬承诺，在那条路径上
// 退化成了"调用方没拼错"。
//
// 修法要点：清单字节已经在手边，`digest.Digest(material.Manifest())` 是
// 纯 CPU——v0.68 省下的 spawn 一个都不会回来，保证却回来了。
//
// 牙齿：删掉 Put 里那个由内容派生的校验，下面第一个测试红。

func v081Material(t *testing.T, records []digest.Record, claimed string) *git.ArchiveMaterial {
	t.Helper()
	return &git.ArchiveMaterial{
		Commit:   "0123456789abcdef0123456789abcdef01234567",
		Digest:   claimed,
		Entries:  []git.TreeEntry{{Path: "a.txt", Mode: "100644", Type: "blob", SHA: "abc"}},
		Contents: [][]byte{[]byte("hello")},
		Records:  records,
	}
}

func TestV81PutRejectsInconsistentMaterial(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	records := []digest.Record{{Path: "a.txt", Mode: "100644", BlobSHA256: digest.HashBytes([]byte("hello"))}}
	// 声称一个与条目不符的 digest（此前这两个字段都由调用方填同一个值，
	// 因此这样的材料会被静默接受）。
	claimed := "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"

	store := newStore(t)
	_, err := store.Put(context.Background(), git.Options{}, "", Meta{
		Repo:     "github:org/dep",
		Commit:   commit,
		Digest:   claimed,
		Material: v081Material(t, records, claimed),
	})
	if err == nil {
		t.Fatal("声称与条目不符的 Material 必须被拒绝——只比对字段是同义反复")
	}
	var ne *errs.NgmError
	if e, ok := err.(*errs.NgmError); ok {
		ne = e
	}
	if ne == nil {
		t.Fatalf("应返回带码的 ngm 错误，got %T: %v", err, err)
	}
	if ne.Code != errs.CodeDigestMismatch {
		t.Errorf("码 = %v, want %v（这是完整性失败，不是配置问题）", ne.Code, errs.CodeDigestMismatch)
	}
}

// TestV81PutMaterialCheckIsDerivedFromContent 判据必须跟着**条目**走：
// 同一份声称值，换掉一条记录就应该翻案。这只可能在"由内容重新派生"时成立。
func TestV81PutMaterialCheckIsDerivedFromContent(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	// 正确的一份
	good := []digest.Record{{Path: "a.txt", Mode: "100644", BlobSHA256: digest.HashBytes([]byte("hello"))}}
	correct := digest.Digest(digest.BuildManifest(good))

	// 声称"正确"的 digest，但条目被换成了别的字节
	tampered := []digest.Record{{Path: "a.txt", Mode: "100644", BlobSHA256: digest.HashBytes([]byte("goodbye"))}}

	store := newStore(t)
	_, err := store.Put(context.Background(), git.Options{}, "", Meta{
		Repo:     "github:org/dep",
		Commit:   commit,
		Digest:   correct,
		Material: v081Material(t, tampered, correct),
	})
	if err == nil {
		t.Fatal("条目被换成别的字节、却声称原 digest，必须被拒绝")
	}
	var ne *errs.NgmError
	if e, ok := err.(*errs.NgmError); ok {
		ne = e
	}
	if ne == nil || ne.Code != errs.CodeDigestMismatch {
		t.Fatalf("want CodeDigestMismatch, got %v", err)
	}
}

// TestV81PutAcceptsConsistentMaterial 一致的材料仍然被接受（不误伤）。
func TestV81PutAcceptsConsistentMaterial(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	records := []digest.Record{{Path: "a.txt", Mode: "100644", BlobSHA256: digest.HashBytes([]byte("hello"))}}
	correct := digest.Digest(digest.BuildManifest(records))

	store := newStore(t)
	res, err := store.Put(context.Background(), git.Options{}, "", Meta{
		Repo:     "github:org/dep",
		Commit:   commit,
		Digest:   correct,
		Material: v081Material(t, records, correct),
	})
	if err != nil {
		t.Fatalf("一致的材料不得被拒绝: %v", err)
	}
	if !store.Has(correct) {
		t.Error("Put 之后 store 应命中该 digest")
	}
	if res.Path == "" {
		t.Error("PutResult.Path 不得为空")
	}
}
