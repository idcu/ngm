package resolve

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/errs"
)

// TestV102DuplicateRootsAreAConflict 钉住解析层也不接受"最后一条赢"。
//
// config.Validate 已在入口拒绝重复声明；这里覆盖绕过它的调用方
// （测试、库用法、未来的其它清单格式）：ResolveGraph 必须自己报冲突。
func TestV102DuplicateRootsAreAConflict(t *testing.T) {
	roots := []DepSpec{
		{Name: "github:acme/lib", Ref: "v1.0.0", RefType: RefTypeTag},
		{Name: "github:acme/lib", Ref: "v2.0.0", RefType: RefTypeTag},
	}
	_, err := ResolveGraph(context.Background(), roots, GraphOptions{
		EnsureMirror: func(context.Context, Canonical) (string, error) {
			return "", errors.New("the duplicate check must run before any mirror access")
		},
	})
	if err == nil {
		t.Fatal("duplicate roots must be a conflict")
	}
	var ne *errs.NgmError
	if !errors.As(err, &ne) || ne.Code != errs.CodeConfigInvalid {
		t.Fatalf("error = %#v, want CodeConfigInvalid", err)
	}
	for _, want := range []string{"conflicting root declarations", "v1.0.0", "v2.0.0"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must mention %q: %v", want, err)
		}
	}
}
