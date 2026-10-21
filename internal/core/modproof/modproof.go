// Package modproof is the language-neutral facade for Git module proof.
//
// Phase 0 of the extraction (ADR-033): it deliberately exposes only content
// identity and binding, not ngm's lock/vendor/CLI contracts. A Repo.Path points
// at a local object repository (mirror or module cache); fetching is the
// caller's responsibility for now.
package modproof

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/idcu/ngm/internal/core/errs"
	"github.com/idcu/ngm/internal/core/git"
)

// Repo is a Git object source. URL is descriptive; Path must be a local
// repository containing the requested commit/trees (mirror or module cache).
type Repo struct {
	URL  string
	Path string
}

// Proof is the content identity plus the Git Merkle binding of one module.
// RootDigest is set for a whole-repository proof; ModuleDigest for a sub-path.
type Proof struct {
	RootDigest   string
	ModuleDigest string
	SubtreeOID   string
}

// Status is a neutral verification outcome. The CLI maps it to exit codes.
type Status string

const (
	StatusOK          Status = "ok"
	StatusMismatch    Status = "mismatch"
	StatusOperational Status = "operational"
	StatusUnsupported Status = "unsupported"
)

// Check is one proof check.
type Check struct {
	Name   string
	Status Status
	Detail string
}

// Report is a neutral proof report. It intentionally has no exit code.
type Report struct {
	Status Status
	Checks []Check
}

// Kind classifies errors without exposing ngm's exit codes.
type Kind string

const (
	KindInvalid     Kind = "invalid"
	KindIntegrity   Kind = "integrity"
	KindOperational Kind = "operational"
	KindUnsupported Kind = "unsupported"
)

// Error is the facade's classified error.
type Error struct {
	Kind Kind
	Op   string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Op
	}
	return e.Op + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Prove computes the root or module proof for a local object repository.
func Prove(ctx context.Context, repo Repo, commit, subPath string) (Proof, error) {
	if strings.TrimSpace(repo.Path) == "" {
		return Proof{}, &Error{Kind: KindUnsupported, Op: "prove",
			Err: errors.New("Repo.Path is required (local mirror or module cache)")}
	}
	sub := strings.Trim(strings.TrimSpace(subPath), "/")

	subtree, err := git.TreeOIDAtPath(ctx, git.Options{}, repo.Path, commit, sub)
	if err != nil {
		return Proof{}, classify("resolve subtree", err)
	}

	if sub == "" {
		m, err := git.BuildArchiveMaterial(ctx, git.Options{}, repo.Path, commit)
		if err != nil {
			return Proof{}, classify("prove root", err)
		}
		return Proof{RootDigest: m.Digest, SubtreeOID: subtree}, nil
	}

	m, err := git.BuildModuleMaterial(ctx, git.Options{}, repo.Path, commit, sub)
	if err != nil {
		return Proof{}, classify("prove module", err)
	}
	return Proof{ModuleDigest: m.Digest, SubtreeOID: subtree}, nil
}

// Verify recomputes a proof and compares it with the expected values.
//
// A mismatch is a Report with StatusMismatch and a nil error. An operational
// failure (missing repository/commit, unreadable object) returns an error.
func Verify(ctx context.Context, repo Repo, commit, subPath string, want Proof) (Report, error) {
	got, err := Prove(ctx, repo, commit, subPath)
	if err != nil {
		return Report{Status: StatusOperational, Checks: []Check{{
			Name: "proof", Status: StatusOperational, Detail: err.Error(),
		}}}, err
	}

	sub := strings.Trim(strings.TrimSpace(subPath), "/")
	report := Report{Status: StatusOK}
	bindingOK := want.SubtreeOID != "" && got.SubtreeOID == want.SubtreeOID
	report.Checks = append(report.Checks, Check{
		Name: "binding", Status: statusFor(bindingOK),
		Detail: fmt.Sprintf("computed %s, expected %s", got.SubtreeOID, want.SubtreeOID),
	})

	var digestOK bool
	var expected, actual string
	if sub == "" {
		expected, actual = want.RootDigest, got.RootDigest
	} else {
		expected, actual = want.ModuleDigest, got.ModuleDigest
	}
	digestOK = expected != "" && expected == actual
	report.Checks = append(report.Checks, Check{
		Name: "digest", Status: statusFor(digestOK),
		Detail: fmt.Sprintf("computed %s, expected %s", actual, expected),
	})

	if !bindingOK || !digestOK {
		report.Status = StatusMismatch
	}
	return report, nil
}

func statusFor(ok bool) Status {
	if ok {
		return StatusOK
	}
	return StatusMismatch
}

func classify(op string, err error) error {
	kind := KindOperational
	var ne *errs.NgmError
	if errors.As(err, &ne) {
		switch ne.Code {
		case errs.CodeDigestMismatch:
			kind = KindIntegrity
		case errs.CodeConfigInvalid:
			kind = KindInvalid
		case errs.CodeGitFetch, errs.CodeInternal:
			kind = KindOperational
		}
	}
	return &Error{Kind: kind, Op: op, Err: err}
}
