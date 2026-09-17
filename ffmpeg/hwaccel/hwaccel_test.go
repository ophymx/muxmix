package hwaccel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

func writeFakeFFmpeg(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-ffmpeg.sh")
	script := "#!/usr/bin/env bash\nset -euo pipefail\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("failed to write fake ffmpeg script: %v", err)
	}
	return path
}

func TestParseHardwareAccelerators(t *testing.T) {
	got := ParseHardwareAccelerators(`Hardware acceleration methods:
cuda
vaapi
qsv
vulkan
videotoolbox
`)
	want := []Kind{CUDA, QSV, VAAPI, VideoToolbox}
	if !slices.Equal(got, want) {
		t.Fatalf("ParseHardwareAccelerators() = %v, want %v", got, want)
	}
}

func TestParseVideoEncoders(t *testing.T) {
	encoders := ParseVideoEncoders(`Encoders:
 V....D h264_vaapi           H.264/AVC (VAAPI)
 V....D hevc_nvenc           NVIDIA NVENC hevc encoder
 A..... aac                  AAC (Advanced Audio Coding)
`)
	if !encoders["h264_vaapi"] {
		t.Fatal("expected h264_vaapi to be detected")
	}
	if !encoders["hevc_nvenc"] {
		t.Fatal("expected hevc_nvenc to be detected")
	}
	if encoders["aac"] {
		t.Fatal("did not expect audio encoder to be treated as video encoder")
	}
}

func TestDetect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}

	fake := writeFakeFFmpeg(t, `
if [[ "${1:-}" == "-hide_banner" ]]; then
  shift
fi

case "${1:-}" in
  -hwaccels)
    cat <<'EOF'
Hardware acceleration methods:
vaapi
cuda
EOF
    ;;
  -encoders)
    cat <<'EOF'
Encoders:
 V....D h264_vaapi           H.264/AVC (VAAPI)
 V....D hevc_nvenc           NVIDIA NVENC hevc encoder
EOF
    ;;
  *)
    echo "unexpected args: $*" >&2
    exit 1
    ;;
esac
`)

	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	support, err := Detect(context.Background(), runner)
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if !support.Has(VAAPI) {
		t.Fatal("expected vaapi support")
	}
	if !support.Has(CUDA) {
		t.Fatal("expected cuda support")
	}
	if !support.SupportsCodec(VAAPI, "h264") {
		t.Fatal("expected h264 vaapi encoder support")
	}
	if !support.SupportsCodec(CUDA, "hevc") {
		t.Fatal("expected hevc nvenc encoder support")
	}
	if support.SupportsCodec(QSV, "h264") {
		t.Fatal("did not expect qsv h264 support")
	}
}

func TestBuildEncodeArgs(t *testing.T) {
	// The VAAPI device only defaults on Linux; pass it explicitly so the
	// test is platform-independent.
	args, err := BuildEncodeArgs(VAAPI, "h264", "/dev/dri/renderD128", "scale=1280:-2")
	if err != nil {
		t.Fatalf("BuildEncodeArgs() error = %v", err)
	}
	want := []string{
		"-vaapi_device", "/dev/dri/renderD128",
		"-vf", "scale=1280:-2,format=nv12,hwupload",
		"-c:v", "h264_vaapi",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("BuildEncodeArgs() = %v, want %v", args, want)
	}

	cudaArgs, err := BuildEncodeArgs(CUDA, "hevc", "0", "fps=30")
	if err != nil {
		t.Fatalf("BuildEncodeArgs() cuda error = %v", err)
	}
	cudaWant := []string{
		"-hwaccel", "cuda",
		"-hwaccel_device", "0",
		"-vf", "fps=30,hwupload_cuda",
		"-c:v", "hevc_nvenc",
	}
	if !slices.Equal(cudaArgs, cudaWant) {
		t.Fatalf("BuildEncodeArgs() cuda = %v, want %v", cudaArgs, cudaWant)
	}
}

func TestBuildVideoCodecRejectsUnsupportedCodec(t *testing.T) {
	if _, err := BuildVideoCodec(VideoToolbox, "vp9"); err == nil {
		t.Fatal("expected unsupported codec error")
	}
}

func TestDefaultProbeArgs(t *testing.T) {
	args, err := DefaultProbeArgs(VAAPI, "/dev/dri/renderD128")
	if err != nil {
		t.Fatalf("DefaultProbeArgs() error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-init_hw_device vaapi=probe:/dev/dri/renderD128") {
		t.Fatalf("DefaultProbeArgs() = %v, missing vaapi init_hw_device", args)
	}
	if !strings.Contains(joined, "format=nv12,hwupload") {
		t.Fatalf("DefaultProbeArgs() = %v, missing vaapi upload filter", args)
	}
	if _, err := DefaultProbeArgs(VAAPI, ""); err == nil {
		t.Fatal("expected missing device error for vaapi probe args")
	}
}

func TestDetectSystem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}

	fake := writeFakeFFmpeg(t, `
if [[ "${1:-}" == "-hide_banner" ]]; then
  shift
fi

if [[ "${1:-}" == "-hwaccels" ]]; then
  cat <<'EOF'
Hardware acceleration methods:
vaapi
qsv
EOF
  exit 0
fi

if [[ "${1:-}" == "-encoders" ]]; then
  cat <<'EOF'
Encoders:
 V....D h264_vaapi           H.264/AVC (VAAPI)
 V....D h264_qsv             H.264/AVC (QSV)
EOF
  exit 0
fi

args="$*"
if [[ "$args" == *"-init_hw_device vaapi=probe:/dev/fake-renderD128"* ]]; then
  exit 0
fi

if [[ "$args" == *"-init_hw_device qsv=probe:/dev/fake-renderD129"* ]]; then
  echo "qsv runtime unavailable" >&2
  exit 1
fi

echo "unexpected args: $*" >&2
exit 1
`)

	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	system, err := DetectSystem(context.Background(), runner, ProbeOptions{
		Kinds: []Kind{QSV, VAAPI},
		Devices: map[Kind][]string{
			VAAPI: {"/dev/fake-renderD128"},
			QSV:   {"/dev/fake-renderD129"},
		},
	})
	if err != nil {
		t.Fatalf("DetectSystem() error = %v", err)
	}
	if !system.Available(VAAPI) {
		t.Fatal("expected vaapi runtime availability")
	}
	if system.Available(QSV) {
		t.Fatal("did not expect qsv runtime availability")
	}
	device, ok := system.Device(VAAPI)
	if !ok || device != "/dev/fake-renderD128" {
		t.Fatalf("Device(VAAPI) = (%q, %t), want (/dev/fake-renderD128, true)", device, ok)
	}
	if !system.SupportsCodec(VAAPI, "h264") {
		t.Fatal("expected vaapi h264 runtime support")
	}
	if system.SupportsCodec(QSV, "h264") {
		t.Fatal("did not expect qsv h264 runtime support")
	}
	kind, selectedDevice, err := system.Select("h264", QSV, VAAPI)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if kind != VAAPI || selectedDevice != "/dev/fake-renderD128" {
		t.Fatalf("Select() = (%q, %q), want (%q, %q)", kind, selectedDevice, VAAPI, "/dev/fake-renderD128")
	}
	if got := system.Probes[QSV][0].Error; !strings.Contains(got, "qsv runtime unavailable") {
		t.Fatalf("qsv probe error = %q, want stderr message", got)
	}
}

func TestProbeReportsRunnerFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}

	fake := writeFakeFFmpeg(t, `
echo "forced failure: $*" >&2
exit 2
`)

	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	probe := Probe(context.Background(), runner, CUDA, "0")
	if probe.Available {
		t.Fatal("expected probe failure")
	}
	if !strings.Contains(probe.Error, "forced failure") {
		t.Fatalf("Probe().Error = %q, want stderr message", probe.Error)
	}
	if len(probe.Args) == 0 {
		t.Fatal("expected probe args to be recorded")
	}
}

func TestSelectFailsWithoutRuntimeSupport(t *testing.T) {
	system := SystemSupport{
		Built: Support{
			Accels: map[Kind]bool{VAAPI: true},
			Encoders: map[string]bool{
				"h264_vaapi": true,
			},
		},
		Probes: map[Kind][]ProbeResult{
			VAAPI: {{Kind: VAAPI, Device: "/dev/fake", Error: "init failed"}},
		},
	}
	_, _, err := system.Select("h264", VAAPI)
	if err == nil {
		t.Fatal("expected Select() failure when runtime support is unavailable")
	}
	if got := fmt.Sprint(err); !strings.Contains(got, "no runtime hwaccel support") {
		t.Fatalf("Select() error = %q, want runtime support error", got)
	}
}
