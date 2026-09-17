package ffprobe

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"syscall"
)

var (
	// ErrFFProbeNotFound indicates that ffprobe is not installed or not in PATH.
	ErrFFProbeNotFound = errors.New("ffprobe not found")

	// ErrInvalidData is returned when ffprobe cannot make sense of the input
	// (AVERROR_INVALIDDATA). It is the usual error for unsupported files.
	ErrInvalidData = errors.New("invalid data found when processing input")

	// ErrDemuxerNotFound is returned when no demuxer handles the input
	// (AVERROR_DEMUXER_NOT_FOUND).
	ErrDemuxerNotFound = errors.New("demuxer not found")

	// ErrProtocolNotFound is returned for an unsupported URL scheme
	// (AVERROR_PROTOCOL_NOT_FOUND).
	ErrProtocolNotFound = errors.New("protocol not found")

	// ErrEOF is returned when the input ends before ffprobe finds anything to
	// report (AVERROR_EOF), for example on an empty file.
	ErrEOF = errors.New("end of file")
)

// AVError is an FFmpeg error code as ffprobe prints it in the "error"
// section. Negative POSIX errno values (-2 for ENOENT, -110 for ETIMEDOUT)
// report system errors; the FFERRTAG constants below are FFmpeg's own.
// It decodes from a JSON number or a numeric string.
type AVError int32

// FFmpeg's own error codes (libavutil/error.h). Each is the negated
// little-endian packing of a four-byte tag.
const (
	AVErrorBSFNotFound         AVError = -(0xF8 | 'B'<<8 | 'S'<<16 | 'F'<<24) // AVERROR_BSF_NOT_FOUND
	AVErrorBug                 AVError = -('B' | 'U'<<8 | 'G'<<16 | '!'<<24)  // AVERROR_BUG
	AVErrorBufferTooSmall      AVError = -('B' | 'U'<<8 | 'F'<<16 | 'S'<<24)  // AVERROR_BUFFER_TOO_SMALL
	AVErrorDecoderNotFound     AVError = -(0xF8 | 'D'<<8 | 'E'<<16 | 'C'<<24) // AVERROR_DECODER_NOT_FOUND
	AVErrorDemuxerNotFound     AVError = -(0xF8 | 'D'<<8 | 'E'<<16 | 'M'<<24) // AVERROR_DEMUXER_NOT_FOUND
	AVErrorEncoderNotFound     AVError = -(0xF8 | 'E'<<8 | 'N'<<16 | 'C'<<24) // AVERROR_ENCODER_NOT_FOUND
	AVErrorEOF                 AVError = -('E' | 'O'<<8 | 'F'<<16 | ' '<<24)  // AVERROR_EOF
	AVErrorExit                AVError = -('E' | 'X'<<8 | 'I'<<16 | 'T'<<24)  // AVERROR_EXIT
	AVErrorExternal            AVError = -('E' | 'X'<<8 | 'T'<<16 | ' '<<24)  // AVERROR_EXTERNAL
	AVErrorFilterNotFound      AVError = -(0xF8 | 'F'<<8 | 'I'<<16 | 'L'<<24) // AVERROR_FILTER_NOT_FOUND
	AVErrorInvalidData         AVError = -('I' | 'N'<<8 | 'D'<<16 | 'A'<<24)  // AVERROR_INVALIDDATA
	AVErrorMuxerNotFound       AVError = -(0xF8 | 'M'<<8 | 'U'<<16 | 'X'<<24) // AVERROR_MUXER_NOT_FOUND
	AVErrorOptionNotFound      AVError = -(0xF8 | 'O'<<8 | 'P'<<16 | 'T'<<24) // AVERROR_OPTION_NOT_FOUND
	AVErrorPatchWelcome        AVError = -('P' | 'A'<<8 | 'W'<<16 | 'E'<<24)  // AVERROR_PATCHWELCOME
	AVErrorProtocolNotFound    AVError = -(0xF8 | 'P'<<8 | 'R'<<16 | 'O'<<24) // AVERROR_PROTOCOL_NOT_FOUND
	AVErrorStreamNotFound      AVError = -(0xF8 | 'S'<<8 | 'T'<<16 | 'R'<<24) // AVERROR_STREAM_NOT_FOUND
	AVErrorBug2                AVError = -('B' | 'U'<<8 | 'G'<<16 | ' '<<24)  // AVERROR_BUG2
	AVErrorUnknown             AVError = -('U' | 'N'<<8 | 'K'<<16 | 'N'<<24)  // AVERROR_UNKNOWN
	AVErrorExperimental        AVError = -0x2bb2afa8                          // AVERROR_EXPERIMENTAL
	AVErrorInputChanged        AVError = -0x636e6701                          // AVERROR_INPUT_CHANGED
	AVErrorOutputChanged       AVError = -0x636e6702                          // AVERROR_OUTPUT_CHANGED
	AVErrorHTTPBadRequest      AVError = -(0xF8 | '4'<<8 | '0'<<16 | '0'<<24) // AVERROR_HTTP_BAD_REQUEST
	AVErrorHTTPUnauthorized    AVError = -(0xF8 | '4'<<8 | '0'<<16 | '1'<<24) // AVERROR_HTTP_UNAUTHORIZED
	AVErrorHTTPForbidden       AVError = -(0xF8 | '4'<<8 | '0'<<16 | '3'<<24) // AVERROR_HTTP_FORBIDDEN
	AVErrorHTTPNotFound        AVError = -(0xF8 | '4'<<8 | '0'<<16 | '4'<<24) // AVERROR_HTTP_NOT_FOUND
	AVErrorHTTPTooManyRequests AVError = -(0xF8 | '4'<<8 | '2'<<16 | '9'<<24) // AVERROR_HTTP_TOO_MANY_REQUESTS
	AVErrorHTTPOther4xx        AVError = -(0xF8 | '4'<<8 | 'X'<<16 | 'X'<<24) // AVERROR_HTTP_OTHER_4XX
	AVErrorHTTPServerError     AVError = -(0xF8 | '5'<<8 | 'X'<<16 | 'X'<<24) // AVERROR_HTTP_SERVER_ERROR
)

var avErrorNames = map[AVError]string{
	AVErrorBSFNotFound:         "AVERROR_BSF_NOT_FOUND",
	AVErrorBug:                 "AVERROR_BUG",
	AVErrorBufferTooSmall:      "AVERROR_BUFFER_TOO_SMALL",
	AVErrorDecoderNotFound:     "AVERROR_DECODER_NOT_FOUND",
	AVErrorDemuxerNotFound:     "AVERROR_DEMUXER_NOT_FOUND",
	AVErrorEncoderNotFound:     "AVERROR_ENCODER_NOT_FOUND",
	AVErrorEOF:                 "AVERROR_EOF",
	AVErrorExit:                "AVERROR_EXIT",
	AVErrorExternal:            "AVERROR_EXTERNAL",
	AVErrorFilterNotFound:      "AVERROR_FILTER_NOT_FOUND",
	AVErrorInvalidData:         "AVERROR_INVALIDDATA",
	AVErrorMuxerNotFound:       "AVERROR_MUXER_NOT_FOUND",
	AVErrorOptionNotFound:      "AVERROR_OPTION_NOT_FOUND",
	AVErrorPatchWelcome:        "AVERROR_PATCHWELCOME",
	AVErrorProtocolNotFound:    "AVERROR_PROTOCOL_NOT_FOUND",
	AVErrorStreamNotFound:      "AVERROR_STREAM_NOT_FOUND",
	AVErrorBug2:                "AVERROR_BUG2",
	AVErrorUnknown:             "AVERROR_UNKNOWN",
	AVErrorExperimental:        "AVERROR_EXPERIMENTAL",
	AVErrorInputChanged:        "AVERROR_INPUT_CHANGED",
	AVErrorOutputChanged:       "AVERROR_OUTPUT_CHANGED",
	AVErrorHTTPBadRequest:      "AVERROR_HTTP_BAD_REQUEST",
	AVErrorHTTPUnauthorized:    "AVERROR_HTTP_UNAUTHORIZED",
	AVErrorHTTPForbidden:       "AVERROR_HTTP_FORBIDDEN",
	AVErrorHTTPNotFound:        "AVERROR_HTTP_NOT_FOUND",
	AVErrorHTTPTooManyRequests: "AVERROR_HTTP_TOO_MANY_REQUESTS",
	AVErrorHTTPOther4xx:        "AVERROR_HTTP_OTHER_4XX",
	AVErrorHTTPServerError:     "AVERROR_HTTP_SERVER_ERROR",
}

// Sentinel errors for the codes callers most often branch on.
var avErrorSentinels = map[AVError]error{
	AVErrorInvalidData:      ErrInvalidData,
	AVErrorDemuxerNotFound:  ErrDemuxerNotFound,
	AVErrorProtocolNotFound: ErrProtocolNotFound,
	AVErrorEOF:              ErrEOF,
}

// IsErrno reports whether the code is a negated POSIX errno rather than an
// FFmpeg tag. FFmpeg's own tags all lie far below any errno.
func (e AVError) IsErrno() bool { return e < 0 && e > -4096 }

// Errno returns the POSIX errno the code wraps, or 0 for FFmpeg's own
// codes. The number is the POSIX value on every platform, because
// ffprobe reports it from the C library it was built against.
func (e AVError) Errno() syscall.Errno {
	if !e.IsErrno() {
		return 0
	}
	return syscall.Errno(-e)
}

// String returns the FFmpeg constant name ("AVERROR_INVALIDDATA"), the
// errno description for system errors, or the bare number.
func (e AVError) String() string {
	if n, ok := avErrorNames[e]; ok {
		return n
	}
	if e.IsErrno() {
		return fmt.Sprintf("errno %d (%s)", -e, e.Errno().Error())
	}
	return strconv.Itoa(int(e))
}

// Error implements error so an AVError constant can be an errors.Is
// target: errors.Is(err, ffprobe.AVErrorEOF).
func (e AVError) Error() string { return "ffprobe: " + e.String() }

// Is lets an errno-valued AVError match the same targets a syscall.Errno
// would: fs.ErrNotExist, fs.ErrPermission, syscall.ETIMEDOUT and so on.
func (e AVError) Is(target error) bool {
	if t, ok := target.(AVError); ok {
		return e == t
	}
	if s, ok := avErrorSentinels[e]; ok && target == s {
		return true
	}
	if !e.IsErrno() {
		return false
	}
	// The ENOENT and EACCES/EPERM numbers are portable even where
	// syscall.Errno uses a different numbering.
	switch -e {
	case 2: // ENOENT
		if target == fs.ErrNotExist {
			return true
		}
	case 1, 13: // EPERM, EACCES
		if target == fs.ErrPermission {
			return true
		}
	}
	return errors.Is(e.Errno(), target)
}

// MarshalJSON emits the code as a JSON number, ffprobe style.
func (e AVError) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt(nil, int64(e), 10), nil
}

// UnmarshalJSON accepts a number or a numeric string.
func (e *AVError) UnmarshalJSON(data []byte) error {
	var i Int
	if err := i.UnmarshalJSON(data); err != nil {
		return err
	}
	*e = AVError(i.Int64())
	return nil
}

// Error implements the error interface so a ProbeError can be returned
// directly from Probe.
func (e *ProbeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("ffprobe: %s (%s)", e.Message, e.Code.String())
}

// Is maps ffprobe's error code onto Go's error vocabulary so callers can
// test errors.Is(err, ffprobe.ErrInvalidData), errors.Is(err,
// ffprobe.AVErrorEOF), or any target a syscall.Errno matches:
// fs.ErrNotExist, fs.ErrPermission, syscall.ETIMEDOUT and so on.
func (e *ProbeError) Is(target error) bool {
	if e == nil {
		return false
	}
	return e.Code.Is(target)
}

// IsUnsupported reports whether err means ffprobe could not parse the input.
func IsUnsupported(err error) bool {
	return errors.Is(err, ErrInvalidData) || errors.Is(err, ErrDemuxerNotFound)
}

// ExitError is returned when ffprobe exits unsuccessfully without printing a
// structured error section, for example on a bad command-line option.
type ExitError struct {
	ExitCode int      // ffprobe's exit status
	Stderr   string   // everything ffprobe logged, trimmed
	Args     []string // the arguments ffprobe was run with
	Cause    error    // the *exec.ExitError (or other failure) from os/exec
}

// Error reports the exit code followed by ffprobe's stderr when there is
// any, for example "ffprobe exited with code 1: Unrecognized option".
func (e *ExitError) Error() string {
	msg := fmt.Sprintf("ffprobe exited with code %d", e.ExitCode)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

// Unwrap returns the underlying os/exec error so errors.As(err,
// *exec.ExitError) and errors.Is(err, exec.ErrNotFound) keep working.
func (e *ExitError) Unwrap() error { return e.Cause }
