package logtide

import (
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

const sdkPkgPrefix = "github.com/logtide-dev/logtide-sdk-go"

// NewStacktrace captures the current goroutine's call stack, stripping
// SDK-internal and Go runtime frames.
//
// skip controls how many additional frames above the SDK boundary are skipped.
// Pass 0 to start from the immediate caller of NewStacktrace.
func NewStacktrace(skip int) *Stacktrace {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(skip+2, pcs) // +2: runtime.Callers + NewStacktrace
	return buildStacktrace(pcs[:n])
}

// ExtractStacktrace attempts to extract a pre-existing stack trace from err.
// It supports errors wrapped with github.com/pkg/errors or any error that
// implements a StackTrace() method returning a slice of uintptr (or a named
// type with uintptr as its underlying element type). Falls back to capturing
// the current call stack.
func ExtractStacktrace(err error) *Stacktrace {
	for err != nil {
		if pcs := tryExtractPkgErrorsStack(err); pcs != nil {
			return buildStacktrace(pcs)
		}
		err = errors.Unwrap(err)
	}
	return NewStacktrace(1)
}

// tryExtractPkgErrorsStack uses reflection to call StackTrace() on err without
// importing github.com/pkg/errors. It handles both []uintptr and named slice
// types whose elements have an underlying uintptr kind (e.g. pkg/errors.Frame).
func tryExtractPkgErrorsStack(err error) []uintptr {
	rv := reflect.ValueOf(err)
	// Guard against a non-nil interface holding a nil concrete pointer:
	// calling a method on a nil receiver would panic inside StackTrace().
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil
	}
	m := rv.MethodByName("StackTrace")
	if !m.IsValid() {
		return nil
	}
	res := m.Call(nil)
	if len(res) != 1 || res[0].Kind() != reflect.Slice {
		return nil
	}
	val := res[0]
	pcs := make([]uintptr, val.Len())
	for i := 0; i < val.Len(); i++ {
		elem := val.Index(i)
		if elem.Kind() != reflect.Uintptr {
			return nil
		}
		pcs[i] = uintptr(elem.Uint())
	}
	return pcs
}

func buildStacktrace(pcs []uintptr) *Stacktrace {
	if len(pcs) == 0 {
		return &Stacktrace{}
	}

	frames := runtime.CallersFrames(pcs)
	var result []Frame
	for {
		f, more := frames.Next()
		if f.Function == "" {
			break
		}
		if !isSDKInternal(f.Function) && !isGoRuntime(f.Function) {
			result = append(result, Frame{
				Function: f.Function,
				Module:   moduleFromFunc(f.Function),
				Filename: filepath.Base(f.File),
				AbsPath:  f.File,
				Lineno:   f.Line,
				InApp:    isInApp(f.Function, f.File),
			})
		}
		if !more {
			break
		}
	}

	// Reverse so innermost frame is last (standard convention).
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}

	return &Stacktrace{Frames: result}
}

func isSDKInternal(fn string) bool {
	if !strings.HasPrefix(fn, sdkPkgPrefix) {
		return false
	}
	// Allow external test packages ("…_test.TestXxx") to appear in stack traces.
	// Internal SDK frames end with '.' (same package) or '/' (sub-package);
	// the external test package path has '_test' immediately after the prefix.
	if len(fn) > len(sdkPkgPrefix) {
		next := fn[len(sdkPkgPrefix)]
		return next == '.' || next == '/'
	}
	return true
}

func isGoRuntime(fn string) bool {
	return strings.HasPrefix(fn, "runtime.") ||
		strings.HasPrefix(fn, "testing.") ||
		strings.HasPrefix(fn, "reflect.")
}

func isInApp(fn, file string) bool {
	return !isGoRuntime(fn) &&
		!strings.Contains(file, "/vendor/") &&
		!strings.Contains(file, "go/pkg/mod/")
}

func moduleFromFunc(fn string) string {
	// "github.com/org/pkg.FuncName" → "github.com/org/pkg"
	if idx := strings.LastIndex(fn, "."); idx > 0 {
		pkg := fn[:idx]
		// strip method receiver: "(*Foo)" prefix
		if paren := strings.LastIndex(pkg, "("); paren > 0 {
			pkg = pkg[:paren-1]
		}
		return pkg
	}
	return fn
}
