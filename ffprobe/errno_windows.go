package ffprobe

import "syscall"

// On Windows syscall.Errno holds Windows system error codes while ffprobe
// reports the C runtime's errno, so the two numberings are unrelated: 22
// means EINVAL to ffprobe but ERROR_BAD_COMMAND to syscall, and 110 means
// ETIMEDOUT to a Linux build but ERROR_OPEN_FAILED to syscall. Only the
// classic numbers that Linux, macOS and the Microsoft C runtime agree on
// are translated, plus the Microsoft values that differ; anything else
// stays an opaque number.
var windowsErrno = map[int32]syscall.Errno{
	1:   syscall.EPERM,
	2:   syscall.ENOENT,
	5:   syscall.EIO,
	9:   syscall.EBADF,
	11:  syscall.EAGAIN,
	12:  syscall.ENOMEM,
	13:  syscall.EACCES,
	17:  syscall.EEXIST,
	20:  syscall.ENOTDIR,
	21:  syscall.EISDIR,
	22:  syscall.EINVAL,
	28:  syscall.ENOSPC,
	32:  syscall.EPIPE,
	34:  syscall.ERANGE,
	138: syscall.ETIMEDOUT, // Microsoft C runtime
}

// errnoOf interprets an errno number ffprobe reported, or 0 when the
// number has no portable meaning.
func errnoOf(n int32) syscall.Errno { return windowsErrno[n] }

// errnoNumber is the inverse of errnoOf, or 0 when the errno is not one
// ffprobe would report by a number we recognise.
func errnoNumber(e syscall.Errno) int32 {
	for n, w := range windowsErrno {
		if w == e {
			return n
		}
	}
	return 0
}
