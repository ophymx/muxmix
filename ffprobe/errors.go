package ffprobe

import (
	"errors"
	"fmt"
	"io/fs"
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

	// ErrUnsupportedFile is kept as an alias of ErrInvalidData.
	ErrUnsupportedFile = ErrInvalidData
)

// AVERROR codes ffprobe reports in its "error" section. Negative errno values
// are POSIX errors; the others are FFmpeg's own FFERRTAG constants.
const (
	codeENOENT           = -2
	codeEACCES           = -13
	codeEINVAL           = -22
	codeInvalidData      = -1094995529 // FFERRTAG('I','N','D','A')
	codeDemuxerNotFound  = -1296385272 // FFERRTAG(0xF8,'D','E','M')
	codeProtocolNotFound = -1330794744 // FFERRTAG(0xF8,'P','R','O')
	codeEOF              = -541478725  // FFERRTAG('E','O','F',' ')
)

// Error implements the error interface so a ProbeError can be returned
// directly from Probe.
func (e *ProbeError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("ffprobe: %s (code %s)", e.Message, e.Code)
}

// Is maps ffprobe's numeric error codes onto sentinel errors so callers can
// use errors.Is(err, ErrInvalidData) or errors.Is(err, fs.ErrNotExist).
func (e *ProbeError) Is(target error) bool {
	if e == nil {
		return false
	}
	switch e.Code.Int64() {
	case codeENOENT:
		return target == fs.ErrNotExist
	case codeEACCES:
		return target == fs.ErrPermission
	case codeInvalidData:
		return target == ErrInvalidData
	case codeDemuxerNotFound:
		return target == ErrDemuxerNotFound
	case codeProtocolNotFound:
		return target == ErrProtocolNotFound
	}
	return false
}

// IsUnsupported reports whether err means ffprobe could not parse the input.
func IsUnsupported(err error) bool {
	return errors.Is(err, ErrInvalidData) || errors.Is(err, ErrDemuxerNotFound)
}

// ExitError is returned when ffprobe exits unsuccessfully without printing a
// structured error section, for example on a bad command-line option.
type ExitError struct {
	ExitCode int
	Stderr   string
	Args     []string
	Cause    error
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("ffprobe exited with code %d", e.ExitCode)
	if e.Stderr != "" {
		msg += ": " + e.Stderr
	}
	return msg
}

func (e *ExitError) Unwrap() error { return e.Cause }
