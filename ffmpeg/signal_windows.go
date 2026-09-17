//go:build windows

package ffmpeg

import "os"

// interrupt stops ffmpeg. Windows has no SIGINT for non-console children,
// so the process is killed.
func interrupt(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}

// supportsExtraFiles reports whether a child can inherit a pipe as fd 3.
// exec.Cmd.ExtraFiles is not supported on Windows.
func supportsExtraFiles() bool { return false }
