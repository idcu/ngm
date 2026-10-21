package resolve_test

import (
	"strings"
	"testing"

	"github.com/idcu/ngm/internal/core/resolve"
)

func TestParseTarget(t *testing.T) {
	t.Run("repository only", func(t *testing.T) {
		got, err := resolve.ParseTarget("github:acme/modules")
		if err != nil {
			t.Fatalf("ParseTarget: %v", err)
		}
		if got.HasPath || got.SubPath != "" {
			t.Fatalf("got %+v, want no sub-path", got)
		}
		if got.Key() != "github:acme/modules" {
			t.Errorf("Key() = %q", got.Key())
		}
	})

	t.Run("path= form", func(t *testing.T) {
		got, err := resolve.ParseTarget("github:acme/modules#path=packages/utils")
		if err != nil {
			t.Fatalf("ParseTarget: %v", err)
		}
		if !got.HasPath || got.SubPath != "packages/utils" {
			t.Fatalf("got %+v, want packages/utils", got)
		}
		if got.Key() != "github:acme/modules#packages/utils" {
			t.Errorf("Key() = %q", got.Key())
		}
		if got.String() != "github:acme/modules#path=packages/utils" {
			t.Errorf("String() = %q", got.String())
		}
	})

	t.Run("legacy #sub form", func(t *testing.T) {
		got, err := resolve.ParseTarget("github:acme/modules#packages/utils")
		if err != nil {
			t.Fatalf("ParseTarget: %v", err)
		}
		if !got.HasPath || got.SubPath != "packages/utils" {
			t.Fatalf("got %+v, want packages/utils", got)
		}
	})

	t.Run("empty sub-path is refused", func(t *testing.T) {
		_, err := resolve.ParseTarget("github:acme/modules#path=")
		if err == nil || !strings.Contains(err.Error(), "empty sub-path") {
			t.Fatalf("expected empty sub-path error, got %v", err)
		}
	})

	t.Run("escaping sub-path is refused", func(t *testing.T) {
		_, err := resolve.ParseTarget("github:acme/modules#path=../outside")
		if err == nil {
			t.Fatal("expected ValidateSubPath error")
		}
	})
}
