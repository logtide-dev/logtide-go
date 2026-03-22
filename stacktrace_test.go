package logtide_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	logtide "github.com/logtide-dev/logtide-sdk-go"
)

func TestNewStacktraceHasFrames(t *testing.T) {
	st := logtide.NewStacktrace(0)
	if st == nil {
		t.Fatal("NewStacktrace returned nil")
	}
	if len(st.Frames) == 0 {
		t.Error("expected at least one frame")
	}
}

func TestNewStacktraceStripsSDKFrames(t *testing.T) {
	st := logtide.NewStacktrace(0)
	for _, f := range st.Frames {
		if strings.HasPrefix(f.Module, "github.com/logtide-dev/logtide-sdk-go") &&
			!strings.HasPrefix(f.Module, "github.com/logtide-dev/logtide-sdk-go_test") {
			t.Errorf("SDK-internal frame leaked: %s %s", f.Module, f.Function)
		}
	}
}

func TestNewStacktraceFrameFields(t *testing.T) {
	st := logtide.NewStacktrace(0)
	if len(st.Frames) == 0 {
		t.Skip("no frames captured")
	}
	// Innermost frame (last in slice) should be in this test.
	inner := st.Frames[len(st.Frames)-1]
	if inner.Function == "" {
		t.Error("frame Function should not be empty")
	}
	if inner.Lineno <= 0 {
		t.Errorf("frame Lineno = %d, want > 0", inner.Lineno)
	}
}

func TestExtractStacktraceFallback(t *testing.T) {
	// A plain error has no StackTrace() method — ExtractStacktrace should fall
	// back to capturing the current call stack.
	err := errors.New("plain error")
	st := logtide.ExtractStacktrace(err)
	if st == nil {
		t.Fatal("ExtractStacktrace returned nil")
	}
	if len(st.Frames) == 0 {
		t.Error("expected frames from fallback capture")
	}
}

type pkgErrorsLike struct {
	msg string
	pcs []uintptr
}

func (e *pkgErrorsLike) Error() string      { return e.msg }
func (e *pkgErrorsLike) StackTrace() []uintptr { return e.pcs }

func TestExtractStacktraceFromErrorWithStack(t *testing.T) {
	// An error that implements StackTrace() []uintptr (pkg/errors-compatible).
	// We pass an empty slice so the stacktrace will have no frames,
	// but it should not fall back to the current stack.
	wrapped := &pkgErrorsLike{msg: "wrapped", pcs: []uintptr{}}
	st := logtide.ExtractStacktrace(wrapped)
	if st == nil {
		t.Fatal("ExtractStacktrace returned nil")
	}
	// Since pcs is empty, Frames should be empty (not fallback frames).
	if len(st.Frames) != 0 {
		t.Errorf("expected 0 frames for empty StackTrace(), got %d", len(st.Frames))
	}
}

func TestExtractStacktraceUnwrapsChain(t *testing.T) {
	inner := &pkgErrorsLike{msg: "inner", pcs: []uintptr{}}
	outer := fmt.Errorf("outer: %w", inner) // stdlib wrapped error
	// ExtractStacktrace should unwrap and find inner's StackTrace.
	st := logtide.ExtractStacktrace(outer)
	if st == nil {
		t.Fatal("ExtractStacktrace returned nil")
	}
}
