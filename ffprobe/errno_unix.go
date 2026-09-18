//go:build !windows

package ffprobe

import "syscall"

// errnoOf interprets an errno number ffprobe reported. On Unix the C
// library ffprobe was built against and syscall.Errno share one
// numbering, so the number carries over as it stands. The numbering is
// still per-platform: ETIMEDOUT is 110 on Linux and 60 on the BSDs, which
// is exactly why callers should compare against syscall constants rather
// than literals.
func errnoOf(n int32) syscall.Errno { return syscall.Errno(n) }

// errnoNumber is the inverse of errnoOf: the number ffprobe would print
// for this errno on this platform.
func errnoNumber(e syscall.Errno) int32 { return int32(e) }
