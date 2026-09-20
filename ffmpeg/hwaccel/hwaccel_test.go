package hwaccel

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
)

// fakeFFmpeg writes a POSIX sh script that answers the caps listings with
// the given hwaccels and encoders, and the probes with probeCase (a sh
// "case" body matched against the whole argument string).
func fakeFFmpeg(t *testing.T, hwaccels, encoders, probeCase string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "-hide_banner" ]; then shift; fi
case "${1:-}" in
  -version) echo "ffmpeg version 7.1.5 Copyright (c) 2000-2025"; exit 0 ;;
  -hwaccels) echo "Hardware acceleration methods:"; printf '%s\n' ` + shQuote(hwaccels) + `; exit 0 ;;
  -encoders) echo "Encoders:"; printf '%s\n' ` + shQuote(encoders) + `; exit 0 ;;
  -decoders|-muxers|-demuxers|-filters|-pix_fmts|-sample_fmts|-bsfs|-protocols) exit 0 ;;
esac
args="$*"
case "$args" in
` + probeCase + `
esac
echo "unexpected args: $*" >&2
exit 1
`
	path := filepath.Join(t.TempDir(), "fake-ffmpeg.sh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

const vaapiQSVEncoders = ` V....D h264_vaapi           H.264/AVC (VAAPI)
 V....D h264_qsv             H.264/AVC (QSV)
 V....D libx264              libx264 H.264`

func TestDetect(t *testing.T) {
	fake := fakeFFmpeg(t, "vaapi\nqsv", vaapiQSVEncoders, `
  *"-init_hw_device vaapi=probe:/dev/fake-renderD128"*) exit 0 ;;
  *"-init_hw_device qsv=probe:/dev/fake-renderD129"*) echo "qsv runtime unavailable" >&2; exit 1 ;;
  *"-c:v h264_vaapi"*) exit 0 ;;
`)
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	opts := ProbeOptions{
		Kinds:   []Kind{QSV, VAAPI, CUDA},
		Devices: map[Kind][]string{VAAPI: {"/dev/fake-renderD128"}, QSV: {"/dev/fake-renderD129"}},
	}
	sys, err := Detect(context.Background(), runner, opts)
	if err != nil {
		t.Fatal(err)
	}
	if sys.Caps == nil || sys.Caps.Version.Version != "7.1.5" || !sys.Caps.HasEncoder("h264_vaapi") {
		t.Fatalf("caps not detected: %+v", sys.Caps)
	}
	if !sys.Built(VAAPI) || !sys.Built(QSV) || sys.Built(CUDA) {
		t.Error("Built")
	}
	if !sys.Available(VAAPI) || sys.Available(QSV) || sys.Available(CUDA) {
		t.Errorf("Available: %+v", sys.Probes)
	}
	if dev, ok := sys.Device(VAAPI); !ok || dev != "/dev/fake-renderD128" {
		t.Errorf("Device(VAAPI) = %q %v", dev, ok)
	}
	if !slices.Equal(sys.AvailableKinds(), []Kind{VAAPI}) {
		t.Errorf("AvailableKinds = %v", sys.AvailableKinds())
	}
	if !sys.SupportsCodec(VAAPI, "h264") || sys.SupportsCodec(QSV, "h264") || sys.SupportsCodec(VAAPI, "hevc") {
		t.Error("SupportsCodec")
	}
	if got := sys.Probes[QSV][0].Error; !strings.Contains(got, "qsv runtime unavailable") {
		t.Errorf("qsv probe error = %q", got)
	}
	if got := sys.Probes[CUDA][0].Error; !strings.Contains(got, "not built") {
		t.Errorf("cuda probe error = %q", got)
	}

	sel, err := sys.Select("h264", QSV, VAAPI)
	if err != nil {
		t.Fatal(err)
	}
	if sel.Kind != VAAPI || sel.Device != "/dev/fake-renderD128" || sel.Encoder != "h264_vaapi" || sel.Codec != "h264" {
		t.Errorf("Select = %+v", sel)
	}
	if sel.String() != "h264_vaapi on /dev/fake-renderD128" {
		t.Errorf("String = %q", sel)
	}

	_, err = sys.Select("hevc")
	var se *SelectError
	if !errors.As(err, &se) || se.Codec != "hevc" {
		t.Fatalf("Select(hevc) = %v", err)
	}
	for _, want := range []string{"vaapi: build lacks hevc_vaapi", "cuda: build lacks hevc_nvenc"} {
		if !strings.Contains(se.Detail(), want) {
			t.Errorf("detail %q lacks %q", se.Detail(), want)
		}
	}
	if _, err := sys.Select("h264", QSV); err == nil || !strings.Contains(err.Error(), "qsv: probe failed: qsv runtime unavailable") {
		t.Errorf("Select(h264, QSV) = %v", err)
	}
}

func TestDetectWithCapsNilSet(t *testing.T) {
	fake := fakeFFmpeg(t, "", "", `
  *"cuda=probe"*) exit 0 ;;
  *"-c:v av1_nvenc"*) echo "Codec not supported" >&2; exit 1 ;;
  *"-c:v "*) exit 0 ;;
`)
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	sys := DetectWithCaps(context.Background(), runner, nil, ProbeOptions{Kinds: []Kind{CUDA}})
	if !sys.Available(CUDA) || !sys.SupportsCodec(CUDA, "hevc") {
		t.Errorf("nil caps should probe every codec: %+v", sys.Probes)
	}
	if sys.SupportsCodec(CUDA, "av1") {
		t.Error("an encoder that failed its probe is not supported, caps or no caps")
	}
}

// TestEncoderProbeRejectsUnsupportedCodec covers the case the build
// capabilities cannot see: the encoder is compiled in and the device
// initialises, but the silicon does not implement that codec. Only
// running it says so, and Select has to believe the probe over the build.
func TestEncoderProbeRejectsUnsupportedCodec(t *testing.T) {
	const encoders = ` V....D h264_vaapi           H.264/AVC (VAAPI)
 V....D av1_vaapi            AV1 (VAAPI)
 V....D libsvtav1            SVT-AV1`
	fake := fakeFFmpeg(t, "vaapi", encoders, `
  *"vaapi=probe:/dev/fake"*) exit 0 ;;
  *"-c:v av1_vaapi"*) echo "[av1_vaapi @ 0x0] No usable encoding profile found." >&2; exit 218 ;;
  *"-c:v h264_vaapi"*) exit 0 ;;
`)
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	opts := ProbeOptions{Kinds: []Kind{VAAPI}, Devices: map[Kind][]string{VAAPI: {"/dev/fake"}}}
	sys, err := Detect(context.Background(), runner, opts)
	if err != nil {
		t.Fatal(err)
	}
	probe := sys.Probes[VAAPI][0]
	if len(probe.Encoders) != 2 {
		t.Fatalf("probed encoders = %+v", probe.Encoders)
	}
	if e, ok := probe.encoder("av1"); !ok || e.Works || e.Encoder != "av1_vaapi" {
		t.Errorf("av1 probe = %+v", e)
	}
	if e, ok := probe.encoder("h264"); !ok || !e.Works {
		t.Errorf("h264 probe = %+v", e)
	}
	if !sys.SupportsCodec(VAAPI, "h264") {
		t.Error("h264 encoded on the device and must stay selectable")
	}
	if sys.SupportsCodec(VAAPI, "av1") {
		t.Error("av1 is in the build but not in the silicon")
	}
	_, err = sys.Select("av1")
	if err == nil || !strings.Contains(err.Error(), "av1_vaapi did not encode on /dev/fake: [av1_vaapi @ 0x0] No usable encoding profile found.") {
		t.Errorf("Select(av1) = %v", err)
	}
	// The point of the probe: PreferHardware falls back instead of
	// building a command that dies mid-job.
	sel, reason, err := PreferHardware().Resolve(sys, "av1")
	if err != nil || sel.Hardware() || !strings.Contains(reason, "did not encode") {
		t.Errorf("Resolve(av1) = %+v %q %v", sel, reason, err)
	}
	if _, _, err := RequireHardware().Resolve(sys, "av1"); err == nil {
		t.Error("RequireHardware must fail for a codec the device cannot encode")
	}
}

func TestProbeOptionsNarrowEncoderProbes(t *testing.T) {
	const encoders = ` V....D h264_vaapi           H.264/AVC (VAAPI)
 V....D hevc_vaapi           H.265/HEVC (VAAPI)`
	probes := `
  *"vaapi=probe:/dev/fake"*) exit 0 ;;
  *"-c:v "*) exit 0 ;;
`
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fakeFFmpeg(t, "vaapi", encoders, probes)))
	base := ProbeOptions{Kinds: []Kind{VAAPI}, Devices: map[Kind][]string{VAAPI: {"/dev/fake"}}}

	narrowed := base
	narrowed.Codecs = []string{"hevc"}
	sys, err := Detect(context.Background(), runner, narrowed)
	if err != nil {
		t.Fatal(err)
	}
	if got := sys.Probes[VAAPI][0].Encoders; len(got) != 1 || got[0].Codec != "hevc" {
		t.Errorf("Codecs narrowing = %+v", got)
	}
	if !sys.SupportsCodec(VAAPI, "h264") {
		t.Error("a codec left out of Codecs is trusted, not rejected")
	}

	skipped := base
	skipped.NoEncoderProbe = true
	sys, err = Detect(context.Background(), runner, skipped)
	if err != nil {
		t.Fatal(err)
	}
	if got := sys.Probes[VAAPI][0].Encoders; len(got) != 0 {
		t.Errorf("NoEncoderProbe still probed: %+v", got)
	}
	if !sys.SupportsCodec(VAAPI, "hevc") {
		t.Error("without encoder probes the build's encoder list is trusted")
	}
}

func TestProbeEncoderArgs(t *testing.T) {
	fake := fakeFFmpeg(t, "", "", `
  *) echo "args: $*" >&2; exit 1 ;;
`)
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	for _, tc := range []struct {
		kind   Kind
		device string
		codec  string
		want   string
	}{
		{VAAPI, "/dev/fake", "h264", "-init_hw_device vaapi=hw:/dev/fake -filter_hw_device hw -f lavfi -i color=s=320x240:d=0.1 -vf format=nv12,hwupload -frames:v 1 -c:v h264_vaapi -f null -"},
		{CUDA, "", "hevc", "-init_hw_device cuda=hw -filter_hw_device hw -f lavfi -i color=s=320x240:d=0.1 -vf format=nv12,hwupload_cuda -frames:v 1 -c:v hevc_nvenc -f null -"},
	} {
		probe := ProbeEncoder(context.Background(), runner, tc.kind, tc.device, tc.codec)
		if probe.Works || !strings.Contains(probe.Error, tc.want) {
			t.Errorf("%s probe error = %q\nwant args %q", tc.kind, probe.Error, tc.want)
		}
	}
	if p := ProbeEncoder(context.Background(), runner, VAAPI, "/dev/fake", "prores"); p.Works || !strings.Contains(p.Error, "not supported") {
		t.Errorf("unmapped codec = %+v", p)
	}
}

// An encoder that takes software frames accepts a p010 upload without
// complaint and encodes it 8-bit, so a p010 probe that ran is only
// believed when the encoder lists a 10-bit format.
func TestProbeEncoderIgnoresConvertedDepth(t *testing.T) {
	fake := fakeFFmpeg(t, "", "", `
  "-h encoder=h264_videotoolbox") printf 'Encoder h264_videotoolbox [VT H.264]:\n    Supported pixel formats: videotoolbox_vld nv12 yuv420p\n'; exit 0 ;;
  "-h encoder=hevc_videotoolbox") printf 'Encoder hevc_videotoolbox [VT HEVC]:\n    Supported pixel formats: videotoolbox_vld nv12 yuv420p p010le\n'; exit 0 ;;
  *"-c:v h264_videotoolbox"*|*"-c:v hevc_videotoolbox"*|*"-c:v prores_videotoolbox"*) exit 0 ;;
`)
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	for codec, want := range map[string][]string{
		"h264": {NV12},
		"hevc": {NV12, P010},
		// No help to go on: the encode result stands.
		"prores": {NV12, P010},
	} {
		probe := ProbeEncoder(context.Background(), runner, VideoToolbox, "", codec)
		if !probe.Works || !slices.Equal(probe.Formats, want) {
			t.Errorf("%s: works=%v formats=%v, want %v (%s)", codec, probe.Works, probe.Formats, want, probe.Error)
		}
	}
}

func TestProbeReportsRunnerFailure(t *testing.T) {
	fake := fakeFFmpeg(t, "", "", `
  *) echo "forced failure: $*" >&2; exit 2 ;;
`)
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	probe := Probe(context.Background(), runner, CUDA, "0")
	if probe.Available || !strings.Contains(probe.Error, "forced failure") || len(probe.Args) == 0 {
		t.Errorf("probe = %+v", probe)
	}
	if p := Probe(context.Background(), runner, "nope", ""); p.Available || !strings.Contains(p.Error, "unknown") {
		t.Errorf("unknown kind = %+v", p)
	}
}

func handSystem() *System {
	return &System{
		Caps: &caps.Set{
			Version:  &baseffmpeg.VersionInfo{Version: "7.1.5"},
			Encoders: []caps.Codec{{Name: "h264_vaapi", Type: caps.Video}, {Name: "h264_nvenc", Type: caps.Video}, {Name: "libx264", Type: caps.Video}},
			HWAccels: []string{"vaapi", "cuda"},
		},
		Probes: map[Kind][]ProbeResult{
			VAAPI: {{
				Kind: VAAPI, Device: "/dev/dri/renderD128", Available: true,
				Encoders: []EncoderProbe{{Codec: "h264", Encoder: "h264_vaapi", Works: true, Formats: []string{NV12}}},
			}},
			CUDA: {{Kind: CUDA, Error: "Cannot load libcuda.so.1\nmore detail"}},
		},
		DetectedAt: time.Now(),
	}
}

func TestSelection(t *testing.T) {
	sys := handSystem()
	sel, err := sys.Select("h264")
	if err != nil {
		t.Fatal(err)
	}
	cmd := baseffmpeg.NewCommand().Input("in.mp4")
	sel.Apply(cmd)
	cmd.Output("out.mp4", sel.Opts("v:0", "scale=1280:-2")...)
	got := strings.Join(cmd.Args(), " ")
	want := "-init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw -i in.mp4 -filter:v:0 scale=1280:-2,format=nv12,hwupload -c:v h264_vaapi out.mp4"
	if got != want {
		t.Errorf("args\n got %s\nwant %s", got, want)
	}
	if f := sel.Filter("", "fps=30"); f != "fps=30,format=nv12,hwupload" {
		t.Errorf("Filter = %q", f)
	}

	var sw Selection
	if sw.Hardware() || sw.String() != "software" || sw.Filter("scale=1:1", "fps=30") != "scale=1:1,fps=30" {
		t.Errorf("software selection: %+v", sw)
	}
	cmd = baseffmpeg.NewCommand().Input("in.mp4")
	sw.Apply(cmd)
	cmd.Output("out.mp4", sw.Opts("v")...)
	if got := strings.Join(cmd.Args(), " "); got != "-i in.mp4 out.mp4" {
		t.Errorf("software args = %s", got)
	}

	cuda := Selection{Kind: CUDA, Encoder: "hevc_nvenc", Codec: "hevc"}
	if cuda.String() != "hevc_nvenc (cuda)" {
		t.Errorf("String = %q", cuda)
	}
	cmd = baseffmpeg.NewCommand()
	cuda.Apply(cmd)
	if got := strings.Join(cmd.Args(), " "); got != "-init_hw_device cuda=hw -filter_hw_device hw" {
		t.Errorf("cuda global = %s", got)
	}
}

func TestPolicy(t *testing.T) {
	sys := handSystem()

	sel, reason, err := Policy{}.Resolve(sys, "h264")
	if err != nil || sel.Hardware() || reason != "" {
		t.Errorf("software policy: %+v %q %v", sel, reason, err)
	}
	sel, reason, err = PreferHardware().Resolve(sys, "h264")
	if err != nil || sel.Kind != VAAPI || reason != "" {
		t.Errorf("prefer h264: %+v %q %v", sel, reason, err)
	}
	sel, reason, err = PreferHardware(CUDA).Resolve(sys, "h264")
	if err != nil || sel.Hardware() || !strings.Contains(reason, "cuda: probe failed: Cannot load libcuda.so.1") || strings.Contains(reason, "more detail") {
		t.Errorf("prefer cuda: %+v %q %v", sel, reason, err)
	}
	sel, reason, err = PreferHardware().Resolve(nil, "h264")
	if err != nil || sel.Hardware() || reason == "" {
		t.Errorf("prefer nil sys: %+v %q %v", sel, reason, err)
	}
	if _, _, err = RequireHardware(CUDA).Resolve(sys, "h264"); err == nil || !strings.Contains(err.Error(), "required for h264") {
		t.Errorf("require cuda: %v", err)
	}
	if _, _, err = RequireHardware().Resolve(nil, "h264"); err == nil {
		t.Error("require with nil sys should fail")
	}
	if sel, _, err := RequireHardware().Resolve(sys, "H265"); err == nil || sel.Codec != "hevc" {
		t.Errorf("require hevc: %+v %v", sel, err)
	}
}

type fakeBackend struct{}

func (fakeBackend) Kind() Kind                            { return "topaz" }
func (fakeBackend) DefaultDevices() []string              { return []string{"gpu0"} }
func (fakeBackend) ProbeArgs(d string) ([]string, error)  { return []string{"-probe", d}, nil }
func (fakeBackend) DeviceArgs(d string) ([]string, error) { return []string{"-topaz_device", d}, nil }
func (fakeBackend) Filter(format string, extra ...string) (string, error) {
	if format == "" {
		return joinFilters(extra, "topazupload"), nil
	}
	return joinFilters(extra, "topazupload="+format), nil
}
func (fakeBackend) VideoCodec(codec string) (string, error) {
	return codecTable{"h264": "h264_topaz"}.encoder(codec)
}

// unregisterForTest drops a kind and the aliases pointing at it, so a test
// that registers a backend leaves the global registry as it found it and
// can run twice (go test -count=2).
func unregisterForTest(kind Kind) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.backends, kind)
	registry.order = slices.DeleteFunc(registry.order, func(k Kind) bool { return k == kind })
	maps.DeleteFunc(registry.aliases, func(_ string, k Kind) bool { return k == kind })
}

func TestRegisterBackend(t *testing.T) {
	Register(fakeBackend{})
	t.Cleanup(func() { unregisterForTest("topaz") })
	RegisterAlias("tpz", "topaz")
	if NormalizeKind("TPZ") != "topaz" || NormalizeKind("topaz") != "topaz" || NormalizeKind("unknown") != None || NormalizeKind("auto") != None {
		t.Error("NormalizeKind")
	}
	if !slices.Contains(Kinds(), Kind("topaz")) || DefaultDevices("topaz")[0] != "gpu0" {
		t.Error("registry")
	}
	sel := Selection{Kind: "topaz", Device: "gpu0", Encoder: "h264_topaz", Codec: "h264"}
	cmd := baseffmpeg.NewCommand()
	sel.Apply(cmd)
	cmd.Output("o", sel.Opts("v", "scale=1280:-2")...)
	if got := strings.Join(cmd.Args(), " "); got != "-topaz_device gpu0 -filter:v scale=1280:-2,topazupload -c:v h264_topaz o" {
		t.Errorf("args = %s", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("duplicate Register should panic")
		}
	}()
	Register(fakeBackend{})
}

// TestPreserveDepth covers the other half of what an encoder probe
// learns: a device that encodes a codec at all may still only encode it
// 8-bit, and uploading a 10-bit source as nv12 throws away depth the
// hardware could have kept.
func TestPreserveDepth(t *testing.T) {
	sys := &System{
		Caps: &caps.Set{
			Version: &baseffmpeg.VersionInfo{Version: "7.1.5"},
			Encoders: []caps.Codec{
				{Name: "h264_vaapi", Type: caps.Video}, {Name: "hevc_vaapi", Type: caps.Video},
			},
			HWAccels: []string{"vaapi"},
		},
		Probes: map[Kind][]ProbeResult{
			VAAPI: {{Kind: VAAPI, Device: "/dev/fake", Available: true, Encoders: []EncoderProbe{
				// As on a Tiger Lake iGPU: HEVC encodes 10-bit, H.264 does not.
				{Codec: "h264", Encoder: "h264_vaapi", Works: true, Formats: []string{NV12}},
				{Codec: "hevc", Encoder: "hevc_vaapi", Works: true, Formats: []string{NV12, P010}},
			}}},
		},
		DetectedAt: time.Now(),
	}

	hevc, err := sys.Select("hevc")
	if err != nil {
		t.Fatal(err)
	}
	deep := sys.PreserveDepth(hevc, "yuv420p10le")
	if deep.Format != P010 {
		t.Errorf("10-bit source on a p010 device = %q, want p010", deep.Format)
	}
	if got := deep.Filter("scale=1280:-2"); got != "scale=1280:-2,format=p010,hwupload" {
		t.Errorf("filter = %q", got)
	}
	if got := deep.String(); got != "hevc_vaapi p010 on /dev/fake" {
		t.Errorf("String = %q", got)
	}
	if got := sys.PreserveDepth(hevc, "yuv420p").Format; got != "" {
		t.Errorf("8-bit source must stay 8-bit, got %q", got)
	}
	if got := sys.PreserveDepth(hevc, "yuv422p10le").Format; got != "" {
		t.Errorf("a layout the pipeline does not carry is left alone, got %q", got)
	}

	h264, err := sys.Select("h264")
	if err != nil {
		t.Fatal(err)
	}
	// h264 never encoded p010 on this device, so the 10-bit source is
	// flattened -- and said to be flattened, rather than left to the
	// backend's default, which for CUDA is the source's own format.
	if got := sys.PreserveDepth(h264, "yuv420p10le"); got.Format != NV12 {
		t.Errorf("h264 should flatten to nv12 explicitly; got %q", got.Format)
	}
	if got := h264.Filter(); got != "format=nv12,hwupload" {
		t.Errorf("default upload = %q", got)
	}

	// A System with no encoder probes has no evidence, so it stays 8-bit.
	if got := handSystem().PreserveDepth(Selection{Kind: VAAPI, Encoder: "hevc_vaapi", Codec: "hevc"}, "yuv420p10le"); got.Format != "" {
		t.Errorf("unprobed System = %q", got.Format)
	}
	var sw Selection
	if got := sys.PreserveDepth(sw, "yuv420p10le"); got.Format != "" {
		t.Error("software selection must not gain an upload format")
	}
}

func TestVideoEncoder(t *testing.T) {
	if c, err := VideoEncoder(None, "H265"); err != nil || c != "hevc" {
		t.Errorf("None = %q %v", c, err)
	}
	if c, err := VideoEncoder(CUDA, "hevc"); err != nil || c != "hevc_nvenc" {
		t.Errorf("CUDA = %q %v", c, err)
	}
	if _, err := VideoEncoder(VideoToolbox, "vp9"); err == nil {
		t.Error("unsupported codec should error")
	}
	if _, err := VideoEncoder("nope", "h264"); err == nil {
		t.Error("unknown kind should error")
	}
}

func TestBuiltinArgs(t *testing.T) {
	cases := []struct {
		sel  Selection
		want string
	}{
		{Selection{Kind: VAAPI, Device: "/dev/dri/renderD128", Encoder: "h264_vaapi"},
			"-init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw -filter:v scale=1280:-2,format=nv12,hwupload -c:v h264_vaapi o"},
		{Selection{Kind: CUDA, Encoder: "hevc_nvenc"},
			"-init_hw_device cuda=hw -filter_hw_device hw -filter:v scale=1280:-2,hwupload_cuda -c:v hevc_nvenc o"},
		{Selection{Kind: QSV, Device: "/dev/dri/renderD128", Encoder: "h264_qsv"},
			"-init_hw_device qsv=hw:/dev/dri/renderD128 -filter_hw_device hw -filter:v scale=1280:-2,format=nv12,hwupload -c:v h264_qsv o"},
		{Selection{Kind: VideoToolbox, Encoder: "h264_videotoolbox"},
			"-filter:v scale=1280:-2 -c:v h264_videotoolbox o"},
	}
	for _, c := range cases {
		cmd := baseffmpeg.NewCommand()
		c.sel.Apply(cmd)
		cmd.Output("o", c.sel.Opts("v", "scale=1280:-2")...)
		if got := strings.Join(cmd.Args(), " "); got != c.want {
			t.Errorf("%s\n got %s\nwant %s", c.sel.Kind, got, c.want)
		}
	}
}

func TestProbeArgs(t *testing.T) {
	b, _ := Lookup(VAAPI)
	if _, err := b.ProbeArgs(""); err == nil {
		t.Error("vaapi probe without device should error")
	}
	args, err := b.ProbeArgs("/dev/dri/renderD128")
	if err != nil || !slices.Contains(args, "vaapi=probe:/dev/dri/renderD128") || !slices.Contains(args, "format=nv12,hwupload") {
		t.Errorf("vaapi probe = %v %v", args, err)
	}
	b, _ = Lookup(CUDA)
	if args, _ := b.ProbeArgs("1"); !slices.Contains(args, "cuda=probe:1") || !slices.Contains(args, "hwupload_cuda") {
		t.Errorf("cuda probe = %v", args)
	}
}
