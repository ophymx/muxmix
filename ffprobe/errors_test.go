package ffprobe

import (
	"encoding/json"
	"errors"
	"io/fs"
	"syscall"
	"testing"
)

// fferrtag mirrors FFERRTAG from libavutil/error.h.
func fferrtag(a, b, c, d byte) AVError {
	return -AVError(uint32(a) | uint32(b)<<8 | uint32(c)<<16 | uint32(d)<<24)
}

func TestAVErrorConstants(t *testing.T) {
	for _, tc := range []struct {
		got  AVError
		want AVError
		name string
	}{
		{AVErrorInvalidData, fferrtag('I', 'N', 'D', 'A'), "AVERROR_INVALIDDATA"},
		{AVErrorDemuxerNotFound, fferrtag(0xF8, 'D', 'E', 'M'), "AVERROR_DEMUXER_NOT_FOUND"},
		{AVErrorProtocolNotFound, fferrtag(0xF8, 'P', 'R', 'O'), "AVERROR_PROTOCOL_NOT_FOUND"},
		{AVErrorEOF, fferrtag('E', 'O', 'F', ' '), "AVERROR_EOF"},
		{AVErrorHTTPNotFound, fferrtag(0xF8, '4', '0', '4'), "AVERROR_HTTP_NOT_FOUND"},
		{AVErrorUnknown, fferrtag('U', 'N', 'K', 'N'), "AVERROR_UNKNOWN"},
	} {
		if tc.got != tc.want || tc.got.String() != tc.name {
			t.Errorf("%s = %d (%s), want %d", tc.name, tc.got, tc.got, tc.want)
		}
		if tc.got.IsErrno() {
			t.Errorf("%s reported as errno", tc.name)
		}
	}
	// The values ffprobe actually prints for the captured errors.
	if AVErrorInvalidData != -1094995529 || AVErrorEOF != -541478725 {
		t.Errorf("INVALIDDATA=%d EOF=%d", AVErrorInvalidData, AVErrorEOF)
	}
	if e := AVError(-110); !e.IsErrno() || e.Errno() != syscall.ETIMEDOUT {
		t.Errorf("-110: errno=%v isErrno=%v", e.Errno(), e.IsErrno())
	}
}

func TestProbeErrorIs(t *testing.T) {
	mk := func(code AVError) error { return &ProbeError{Code: code, Message: "x"} }
	for _, tc := range []struct {
		code   AVError
		target error
		want   bool
	}{
		{-2, fs.ErrNotExist, true},
		{-2, syscall.ENOENT, true},
		{-2, fs.ErrPermission, false},
		{-13, fs.ErrPermission, true},
		{-1, fs.ErrPermission, true},
		{-110, syscall.ETIMEDOUT, true},
		{-110, fs.ErrNotExist, false},
		{-22, syscall.EINVAL, true},
		{AVErrorInvalidData, ErrInvalidData, true},
		{AVErrorInvalidData, AVErrorInvalidData, true},
		{AVErrorInvalidData, ErrEOF, false},
		{AVErrorDemuxerNotFound, ErrDemuxerNotFound, true},
		{AVErrorProtocolNotFound, ErrProtocolNotFound, true},
		{AVErrorEOF, ErrEOF, true},
		{AVErrorEOF, AVErrorEOF, true},
		{AVErrorHTTPNotFound, AVErrorHTTPNotFound, true},
		{AVErrorHTTPNotFound, fs.ErrNotExist, false},
	} {
		if got := errors.Is(mk(tc.code), tc.target); got != tc.want {
			t.Errorf("errors.Is(code %s, %v) = %v, want %v", tc.code, tc.target, got, tc.want)
		}
	}
	if !IsUnsupported(mk(AVErrorInvalidData)) || !IsUnsupported(mk(AVErrorDemuxerNotFound)) || IsUnsupported(mk(-2)) {
		t.Error("IsUnsupported")
	}
	var pe *ProbeError
	if err := mk(-2); !errors.As(err, &pe) || pe.Code != -2 || err.Error() != "ffprobe: x (errno 2 ("+syscall.ENOENT.Error()+"))" {
		t.Errorf("ProbeError = %v", err)
	}
}

func TestAVErrorJSON(t *testing.T) {
	var pe ProbeError
	for _, src := range []string{`{"code":-2,"string":"No such file"}`, `{"code":"-2","string":"No such file"}`} {
		if err := json.Unmarshal([]byte(src), &pe); err != nil || pe.Code != -2 {
			t.Errorf("%s: code=%d err=%v", src, pe.Code, err)
		}
	}
	out, _ := json.Marshal(pe)
	if string(out) != `{"code":-2,"string":"No such file"}` {
		t.Errorf("marshal = %s", out)
	}
}
