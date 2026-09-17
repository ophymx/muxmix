//go:build !windows

package ffmpeg

import (
	"os"
	"syscall"
)

// interrupt asks ffmpeg to stop gracefully; it finishes the current output
// and writes trailers before exiting.
func interrupt(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGINT)
}

// supportsExtraFiles reports whether a child can inherit a pipe as fd 3.
func supportsExtraFiles() bool { return true }
