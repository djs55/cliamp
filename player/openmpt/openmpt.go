// Package openmpt is a minimal purego binding to libopenmpt, used to decode
// tracker module files (.mod, .s3m, .xm, .it, and other formats libopenmpt
// supports).
//
// It loads the system's libopenmpt shared library at runtime via purego —
// no CGo, no build-time SDK, no static linking. This keeps `go install` and
// cross-compiled release builds working exactly as they do today. If the
// library isn't installed, Available reports false and Open returns
// ErrUnavailable so callers can degrade gracefully, the same way this
// codebase already treats ffmpeg and yt-dlp as optional runtime
// dependencies.
//
// libopenmpt's C API promises long-term ABI/API stability, which is what
// makes a dynamically-loaded binding like this viable — the small set of
// functions bound here (module create/destroy/read/seek/duration) has been
// stable since libopenmpt 0.3.
package openmpt

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ErrUnavailable indicates the libopenmpt shared library could not be
// loaded (not installed, or not found on the platform's library search
// path).
var ErrUnavailable = errors.New("libopenmpt: shared library not found")

var (
	initOnce sync.Once
	initErr  error

	fnCreateFromMemory2          func(filedata []byte, filesize uintptr, logfunc uintptr, loguser unsafe.Pointer, errfunc uintptr, erruser unsafe.Pointer, errOut *int32, errMsgOut unsafe.Pointer, ctls unsafe.Pointer) uintptr
	fnDestroy                    func(mod uintptr)
	fnReadInterleavedFloatStereo func(mod uintptr, samplerate int32, count uintptr, interleaved []float32) uintptr
	fnGetDurationSeconds         func(mod uintptr) float64
	fnGetPositionSeconds         func(mod uintptr) float64
	fnSetPositionSeconds         func(mod uintptr, seconds float64) float64

	// logFunc and errFunc point at libopenmpt's own no-op log/error
	// handlers (resolved by address, never called from Go). Passing NULL
	// for these instead falls back to libopenmpt's *default* handlers,
	// which write straight to stderr — fine for a CLI tool, but it would
	// garble cliamp's full-screen TUI the moment someone opens a
	// corrupt or unrecognized module file. Zero is still a safe fallback
	// if resolving either symbol fails.
	logFunc uintptr
	errFunc uintptr
)

// ensureInit loads libopenmpt and resolves the symbols we need, exactly
// once. Safe to call from multiple goroutines.
func ensureInit() error {
	initOnce.Do(func() {
		handle, err := loadLibrary()
		if err != nil {
			initErr = fmt.Errorf("%w: %v", ErrUnavailable, err)
			return
		}
		purego.RegisterLibFunc(&fnCreateFromMemory2, handle, "openmpt_module_create_from_memory2")
		purego.RegisterLibFunc(&fnDestroy, handle, "openmpt_module_destroy")
		purego.RegisterLibFunc(&fnReadInterleavedFloatStereo, handle, "openmpt_module_read_interleaved_float_stereo")
		purego.RegisterLibFunc(&fnGetDurationSeconds, handle, "openmpt_module_get_duration_seconds")
		purego.RegisterLibFunc(&fnGetPositionSeconds, handle, "openmpt_module_get_position_seconds")
		purego.RegisterLibFunc(&fnSetPositionSeconds, handle, "openmpt_module_set_position_seconds")

		if addr, err := dlsym(handle, "openmpt_log_func_silent"); err == nil {
			logFunc = addr
		}
		if addr, err := dlsym(handle, "openmpt_error_func_ignore"); err == nil {
			errFunc = addr
		}
	})
	return initErr
}

// Available reports whether libopenmpt was found and its symbols resolved.
// The underlying library load happens at most once no matter how many
// times Available or Open is called.
func Available() bool {
	return ensureInit() == nil
}

// Module is an open tracker module ready for rendering.
type Module struct {
	mu  sync.Mutex
	mod uintptr
}

// Open loads a module from its raw file bytes. Per libopenmpt's contract,
// data only needs to stay valid for the duration of this call.
func Open(data []byte) (*Module, error) {
	if err := ensureInit(); err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, errors.New("libopenmpt: empty file")
	}

	var errCode int32
	mod := fnCreateFromMemory2(data, uintptr(len(data)), logFunc, nil, errFunc, nil, &errCode, nil, nil)
	if mod == 0 {
		return nil, fmt.Errorf("libopenmpt: unrecognized or corrupt module file (error code %d)", errCode)
	}
	return &Module{mod: mod}, nil
}

// ReadInterleavedFloatStereo renders up to len(buf)/2 stereo frames into buf
// (interleaved L,R,L,R,...) at the given sample rate. It returns the number
// of frames actually rendered; 0 means the module has finished playing.
func (m *Module) ReadInterleavedFloatStereo(sampleRate int, buf []float32) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mod == 0 || len(buf) < 2 {
		return 0
	}
	count := len(buf) / 2
	n := fnReadInterleavedFloatStereo(m.mod, int32(sampleRate), uintptr(count), buf[:count*2])
	return int(n)
}

// DurationSeconds returns the module's estimated playback duration.
func (m *Module) DurationSeconds() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mod == 0 {
		return 0
	}
	return fnGetDurationSeconds(m.mod)
}

// PositionSeconds returns the current playback position.
func (m *Module) PositionSeconds() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mod == 0 {
		return 0
	}
	return fnGetPositionSeconds(m.mod)
}

// SetPositionSeconds seeks to the given position and returns the position
// actually reached (libopenmpt clamps out-of-range requests).
func (m *Module) SetPositionSeconds(seconds float64) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mod == 0 {
		return 0
	}
	return fnSetPositionSeconds(m.mod, seconds)
}

// Close releases the module's native resources. Safe to call more than
// once.
func (m *Module) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mod != 0 {
		fnDestroy(m.mod)
		m.mod = 0
	}
}
