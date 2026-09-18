package tasks_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
	"github.com/ophymx/muxmix/ffprobe"
	"github.com/ophymx/muxmix/tasks"
)

// As in ffmpeg/hwaccel, these tests drive the machine's real GPUs and are
// off unless MUXMIX_HWACCEL_TEST is set:
//
//	MUXMIX_HWACCEL_TEST=1 go test ./tasks -run Hardware -v
const hwEnv = "MUXMIX_HWACCEL_TEST"

// hardwareTools detects the machine once and returns Tools using it,
// along with a codec the hardware encodes and one it does not.
func hardwareTools(t *testing.T) (tools *tasks.Tools, supported, unsupported encode.Codec) {
	t.Helper()
	if os.Getenv(hwEnv) == "" {
		t.Skipf("set %s=1 to test against this machine's hardware", hwEnv)
	}
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skipf("ffmpeg not usable: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	sys, err := hwaccel.Detect(ctx, nil, hwaccel.ProbeOptions{})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(sys.AvailableKinds()) == 0 {
		t.Skip("no hardware acceleration on this machine")
	}
	for _, codec := range []encode.Codec{encode.H264, encode.HEVC, encode.AV1, encode.VP9} {
		inHardware := false
		for _, kind := range sys.AvailableKinds() {
			inHardware = inHardware || sys.SupportsCodec(kind, string(codec))
		}
		// A codec is only a fair test of the fallback when no backend at
		// all encodes it and the build can still encode it in software.
		_, softwareErr := encode.ChooseEncoder(sys.Caps, codec, hwaccel.None)
		switch {
		case inHardware && supported == "":
			supported = codec
		case !inHardware && unsupported == "" && softwareErr == nil:
			unsupported = codec
		}
	}
	t.Logf("hardware kinds %v; supported codec %q; unsupported codec %q", sys.AvailableKinds(), supported, unsupported)
	return &tasks.Tools{System: sys}, supported, unsupported
}

func hardwareSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.mp4")
	args := []string{"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=s=640x360:r=25:d=2",
		"-f", "lavfi", "-i", "sine=f=440:d=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", path}
	if _, err := ffmpeg.RunArgs(t.Context(), args); err != nil {
		t.Fatalf("make source: %v", err)
	}
	return path
}

// TestHardwareTranscodePolicies runs the three policies end to end on the
// real machine. The one that matters is PreferHardware for a codec the
// GPU cannot encode: the plan has to come back software rather than build
// a command that dies partway through the job.
func TestHardwareTranscodePolicies(t *testing.T) {
	tools, supported, unsupported := hardwareTools(t)
	src := hardwareSource(t)
	dir := t.TempDir()

	run := func(t *testing.T, name string, codec encode.Codec, policy hwaccel.Policy) *tasks.TranscodePlan {
		t.Helper()
		out := filepath.Join(dir, name+".mkv")
		plan, err := tools.Transcode(t.Context(), src, out, tasks.TranscodeOptions{
			HW:    policy,
			Video: tasks.VideoRule{Encode: encode.Video{Codec: codec}, MaxHeight: 180},
			Audio: tasks.AudioRule{Drop: true},
		})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		res, err := ffprobe.Probe(t.Context(), out)
		if err != nil {
			t.Fatalf("%s: probe output: %v", name, err)
		}
		v := res.VideoStream()
		if v == nil {
			t.Fatalf("%s: no video in the output", name)
		}
		if _, h := v.Resolution(); h != 180 {
			t.Errorf("%s: output height %d, want 180", name, h)
		}
		t.Logf("%s: %s-> %s", name, plan, v.CodecName)
		return plan
	}

	if supported == "" {
		t.Skip("no codec this machine encodes in hardware")
	}
	t.Run("prefer-supported", func(t *testing.T) {
		plan := run(t, "prefer-supported", supported, hwaccel.PreferHardware())
		if v := plan.Streams[0]; !v.HW.Hardware() {
			t.Errorf("%s is supported in hardware but the plan is %s", supported, v.Encoder)
		}
	})
	t.Run("require-supported", func(t *testing.T) {
		plan := run(t, "require-supported", supported, hwaccel.RequireHardware())
		if v := plan.Streams[0]; !v.HW.Hardware() {
			t.Errorf("RequireHardware planned %s", v.Encoder)
		}
	})

	if unsupported == "" {
		t.Skip("this machine's hardware covers every codec tested")
	}
	t.Run("prefer-unsupported-falls-back", func(t *testing.T) {
		plan := run(t, "prefer-unsupported", unsupported, hwaccel.PreferHardware())
		v := plan.Streams[0]
		if v.HW.Hardware() {
			t.Fatalf("%s is not encodable on this hardware but the plan chose %s", unsupported, v.HW)
		}
		if v.Reason == "" {
			t.Error("a software fallback must say why hardware was not used")
		}
	})
	t.Run("require-unsupported-fails-before-running", func(t *testing.T) {
		out := filepath.Join(dir, "require-unsupported.mkv")
		_, err := tools.Transcode(t.Context(), src, out, tasks.TranscodeOptions{
			HW:    hwaccel.RequireHardware(),
			Video: tasks.VideoRule{Encode: encode.Video{Codec: unsupported}, MaxHeight: 180},
			Audio: tasks.AudioRule{Drop: true},
		})
		if err == nil {
			t.Fatal("RequireHardware must fail for a codec the hardware cannot encode")
		}
		if _, statErr := os.Stat(out); statErr == nil {
			t.Error("the job must fail before writing an output")
		}
		t.Logf("correctly refused: %v", err)
	})
}
