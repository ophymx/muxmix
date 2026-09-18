package hwaccel_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
	"github.com/ophymx/muxmix/ffprobe"
)

// The tests in this file run against the machine's real GPUs, so they are
// off unless MUXMIX_HWACCEL_TEST is set. CI runners have no hardware
// acceleration and would skip every assertion that matters; a developer
// with a GPU runs:
//
//	MUXMIX_HWACCEL_TEST=1 go test ./ffmpeg/hwaccel -run Hardware -v
const hwEnv = "MUXMIX_HWACCEL_TEST"

func requireHardwareTest(t *testing.T) {
	t.Helper()
	if os.Getenv(hwEnv) == "" {
		t.Skipf("set %s=1 to test against this machine's hardware", hwEnv)
	}
	if err := baseffmpeg.ValidateInstall(); err != nil {
		t.Skipf("ffmpeg not usable: %v", err)
	}
}

// detectHardware detects once for every test in this file.
func detectHardware(t *testing.T) *hwaccel.System {
	t.Helper()
	requireHardwareTest(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	sys, err := hwaccel.Detect(ctx, nil, hwaccel.ProbeOptions{})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	for _, kind := range hwaccel.Kinds() {
		for _, p := range sys.Probes[kind] {
			t.Logf("%s %s: available=%v %s", kind, p.Device, p.Available, firstLine(p.Error))
			for _, e := range p.Encoders {
				t.Logf("    %-14s works=%v %s", e.Encoder, e.Works, firstLine(e.Error))
			}
		}
	}
	if len(sys.AvailableKinds()) == 0 {
		t.Skip("no hardware acceleration on this machine")
	}
	return sys
}

// source writes a clip big enough for every hardware encoder: NVENC
// rejects frames narrower than 145 pixels.
func source(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.mp4")
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=s=640x360:r=25:d=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", path}
	if _, err := baseffmpeg.RunArgs(t.Context(), args); err != nil {
		t.Fatalf("make source: %v", err)
	}
	return path
}

// TestHardwareSelectionEncodes is the cross-check the build capabilities
// cannot do on their own: for every backend and codec, what the System
// claims must match what the hardware actually does. A build can carry
// av1_vaapi on a chip with no AV1 encoder, so both directions matter --
// a claimed codec that fails to encode is a broken job, and a rejected
// codec that encodes fine is lost throughput.
func TestHardwareSelectionEncodes(t *testing.T) {
	sys := detectHardware(t)
	src := source(t)
	dir := t.TempDir()

	for _, kind := range hwaccel.Kinds() {
		if !sys.Available(kind) {
			continue
		}
		for _, codec := range hwaccel.BackendCodecs(kind) {
			encoder, err := hwaccel.VideoEncoder(kind, codec)
			if err != nil || !sys.HasEncoder(encoder) {
				continue
			}
			t.Run(string(kind)+"/"+codec, func(t *testing.T) {
				claimed := sys.SupportsCodec(kind, codec)
				out := filepath.Join(dir, string(kind)+"_"+codec+".mkv")
				sel, selErr := sys.Select(codec, kind)
				if claimed != (selErr == nil) {
					t.Fatalf("SupportsCodec=%v but Select=%v", claimed, selErr)
				}
				if !claimed {
					// Rejected: prove the rejection was earned by running
					// the encoder anyway.
					sel = hwaccel.Selection{Kind: kind, Codec: codec, Encoder: encoder}
					if device, ok := sys.Device(kind); ok {
						sel.Device = device
					}
				}
				err := encodeWith(t, sel, src, out)
				switch {
				case claimed && err != nil:
					t.Errorf("%s is offered for %s but did not encode: %v", encoder, codec, err)
				case !claimed && err == nil:
					t.Errorf("%s is rejected (%v) but encoded fine", encoder, selErr)
				case !claimed:
					t.Logf("correctly rejected: %v", selErr)
				}
			})
		}
	}
}

// encodeWith runs one real encode through the Selection's own command
// pieces and checks the output holds decodable video.
func encodeWith(t *testing.T, sel hwaccel.Selection, src, out string) error {
	t.Helper()
	cmd := baseffmpeg.NewCommand().
		GlobalOptions(baseffmpeg.Overwrite(), baseffmpeg.LogLevel("error")).
		Input(src)
	sel.Apply(cmd)
	opts := append([]baseffmpeg.Opt{baseffmpeg.Map("0:v:0"), baseffmpeg.NoAudio()},
		sel.Opts("v:0", "scale=320:-2")...)
	cmd.Output(out, opts...)
	if err := cmd.Validate(); err != nil {
		return err
	}
	if _, err := baseffmpeg.Run(t.Context(), cmd); err != nil {
		return err
	}
	res, err := ffprobe.Probe(t.Context(), out)
	if err != nil {
		return err
	}
	v := res.VideoStream()
	if v == nil {
		return errors.New("no video stream in the output")
	}
	if w, h := v.Resolution(); w != 320 || h != 180 {
		return fmt.Errorf("output is %dx%d, want the 320x180 the filter chain asked for", w, h)
	}
	t.Logf("%s -> %s %vx%v", sel, v.CodecName, v.Width, v.Height)
	return nil
}

// TestHardwareDetectCached checks the cache against the real host: a cold
// detection is written and reused, and a host whose devices changed is
// detected again.
func TestHardwareDetectCached(t *testing.T) {
	requireHardwareTest(t)
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "hw.json")

	cold, fromCache, err := hwaccel.DetectCached(ctx, nil, path, time.Hour, hwaccel.ProbeOptions{})
	if err != nil || fromCache {
		t.Fatalf("cold: fromCache=%v err=%v", fromCache, err)
	}
	warm, fromCache, err := hwaccel.DetectCached(ctx, nil, path, time.Hour, hwaccel.ProbeOptions{})
	if err != nil || !fromCache {
		t.Fatalf("warm: fromCache=%v err=%v", fromCache, err)
	}
	if got, want := warm.AvailableKinds(), cold.AvailableKinds(); len(got) != len(want) {
		t.Errorf("cached kinds %v, detected %v", got, want)
	}
	for _, kind := range cold.AvailableKinds() {
		for _, codec := range hwaccel.BackendCodecs(kind) {
			if cold.SupportsCodec(kind, codec) != warm.SupportsCodec(kind, codec) {
				t.Errorf("%s/%s survived the cache differently", kind, codec)
			}
		}
	}

	moved := hwaccel.ProbeOptions{Devices: map[hwaccel.Kind][]string{hwaccel.VAAPI: {"/dev/dri/does-not-exist"}}}
	if _, fromCache, err = hwaccel.DetectCached(ctx, nil, path, time.Hour, moved); err != nil || fromCache {
		t.Errorf("a changed device set must re-detect: fromCache=%v err=%v", fromCache, err)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// tenBitSource writes a 10-bit clip, the input that separates a device
// that keeps depth from one that flattens it.
func tenBitSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src10.mkv")
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=s=640x360:r=25:d=2",
		"-c:v", "libx265", "-x265-params", "log-level=none", "-pix_fmt", "yuv420p10le", path}
	if _, err := baseffmpeg.RunArgs(t.Context(), args); err != nil {
		t.Skipf("no 10-bit encoder to build a source: %v", err)
	}
	return path
}

// TestHardwareTenBitDepth cross-checks the probe's format verdicts the
// same way as its codec verdicts: an encoder the probe says encodes p010
// must come back 10-bit, and one it says does not must not be claimed.
// Without this the upload chain silently flattens a 10-bit master to
// 8-bit on hardware that could have kept it.
func TestHardwareTenBitDepth(t *testing.T) {
	sys := detectHardware(t)
	src := tenBitSource(t)
	dir := t.TempDir()
	tested := 0

	for _, kind := range sys.AvailableKinds() {
		device, _ := sys.Device(kind)
		for _, codec := range hwaccel.BackendCodecs(kind) {
			if !sys.SupportsCodec(kind, codec) {
				continue
			}
			sel, err := sys.Select(codec, kind)
			if err != nil {
				t.Fatalf("%s/%s: %v", kind, codec, err)
			}
			deep := sys.PreserveDepth(sel, "yuv420p10le")
			claimsDepth := deep.Format == hwaccel.P010
			t.Run(string(kind)+"/"+codec, func(t *testing.T) {
				out := filepath.Join(dir, string(kind)+"_"+codec+"_10bit.mkv")
				if err := encodeWith(t, deep, src, out); err != nil {
					if claimsDepth {
						t.Fatalf("%s on %s is offered for 10-bit but failed: %v", deep.Encoder, device, err)
					}
					t.Skipf("8-bit path failed for another reason: %v", err)
				}
				res, err := ffprobe.Probe(t.Context(), out)
				if err != nil {
					t.Fatal(err)
				}
				got := res.VideoStream().PixFmt
				deepOut := strings.Contains(got, "10")
				switch {
				case claimsDepth && !deepOut:
					t.Errorf("probe says %s encodes p010 but the output is %s", deep.Encoder, got)
				case !claimsDepth && deepOut:
					t.Errorf("%s was not offered p010 but kept 10 bits (%s); the probe is too strict", deep.Encoder, got)
				default:
					t.Logf("%s: 10-bit source -> %s (p010 offered: %v)", deep, got, claimsDepth)
				}
			})
			tested++
		}
	}
	if tested == 0 {
		t.Skip("no hardware encoder to test depth with")
	}
}
