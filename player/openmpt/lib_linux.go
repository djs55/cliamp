//go:build linux

package openmpt

import "github.com/ebitengine/purego"

// libraryCandidates are tried in order; distros package the SONAME
// (libopenmpt.so.0) but not always the unversioned dev symlink.
var libraryCandidates = []string{
	"libopenmpt.so.0",
	"libopenmpt.so",
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
