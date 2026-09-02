//go:build darwin

package openmpt

import "github.com/ebitengine/purego"

// libraryCandidates covers the plain dylib name plus the Homebrew prefixes
// for Apple Silicon and Intel, since Homebrew's lib dir isn't always on the
// dynamic linker's default search path.
var libraryCandidates = []string{
	"libopenmpt.dylib",
	"/opt/homebrew/lib/libopenmpt.dylib",
	"/usr/local/lib/libopenmpt.dylib",
	"/usr/local/opt/libopenmpt/lib/libopenmpt.dylib",
}

func loadLibrary() (uintptr, error) {
	var lastErr error
	for _, name := range libraryCandidates {
		handle, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			return handle, nil
		}
		lastErr = err
	}
	return 0, lastErr
}

func dlsym(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}
