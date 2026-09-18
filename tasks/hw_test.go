package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
)

// vaapiSystem is a machine with a working VAAPI render node and a build
// that has h264_vaapi, h264_nvenc (but no working CUDA), libx264 and libx265.
func vaapiSystem() *hwaccel.System {
	return &hwaccel.System{
		Caps: &caps.Set{
			Version: &ffmpeg.VersionInfo{Version: "7.1.5"},
			Encoders: []caps.Codec{
				{Name: "h264_vaapi", Type: caps.Video}, {Name: "h264_nvenc", Type: caps.Video},
				{Name: "libx264", Type: caps.Video}, {Name: "libx265", Type: caps.Video},
				{Name: "aac", Type: caps.Audio},
			},
			HWAccels: []string{"vaapi", "cuda"},
		},
		Probes: map[hwaccel.Kind][]hwaccel.ProbeResult{
			hwaccel.VAAPI: {{Kind: hwaccel.VAAPI, Device: "/dev/dri/renderD128", Available: true}},
			hwaccel.CUDA:  {{Kind: hwaccel.CUDA, Error: "Cannot load libcuda.so.1"}},
		},
		DetectedAt: time.Now(),
	}
}

func TestTranscodePlanHardware(t *testing.T) {
	info := captureInfo(t, "multi_mkv.basic.json")
	sys := vaapiSystem()

	plan, err := BuildTranscodePlan(info, sys, "multi.mkv", "out.mp4", TranscodeOptions{
		HW:    hwaccel.PreferHardware(),
		Video: VideoRule{MaxHeight: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := args(plan)
	for _, want := range []string{
		"-init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw -i multi.mkv",
		"-c:v:0 h264_vaapi",
		"-filter:v:0 scale=w=-2:h=16,format=nv12,hwupload",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args lack %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-pix_fmt") {
		t.Errorf("hardware plan must not force a pix_fmt:\n%s", got)
	}
	v := plan.Streams[0]
	if v.Action != Encode || v.Encoder != "h264_vaapi" || v.HW.Kind != hwaccel.VAAPI || v.HW.Device != "/dev/dri/renderD128" {
		t.Errorf("video stream = %+v", v)
	}
	if s := plan.String(); !strings.Contains(s, "→ h264_vaapi on /dev/dri/renderD128") {
		t.Errorf("String():\n%s", s)
	}

	// Without scaling the upload chain is still there.
	plan, err = BuildTranscodePlan(info, sys, "multi.mkv", "out.mp4", TranscodeOptions{HW: hwaccel.PreferHardware()})
	if err != nil {
		t.Fatal(err)
	}
	if got := args(plan); !strings.Contains(got, "-filter:v:0 format=nv12,hwupload") {
		t.Errorf("upload chain missing:\n%s", got)
	}

	// Prefer a backend that failed its probe: software, with the reason.
	plan, err = BuildTranscodePlan(info, sys, "multi.mkv", "out.mp4", TranscodeOptions{HW: hwaccel.PreferHardware(hwaccel.CUDA)})
	if err != nil {
		t.Fatal(err)
	}
	got = args(plan)
	if !strings.Contains(got, "-c:v:0 libx264") || !strings.Contains(got, "-pix_fmt:v:0 yuv420p") || strings.Contains(got, "init_hw_device") {
		t.Errorf("software fallback args:\n%s", got)
	}
	if r := plan.Streams[0].Reason; !strings.Contains(r, "software: cuda: probe failed: Cannot load libcuda.so.1") {
		t.Errorf("reason = %q", r)
	}
	if plan.Streams[0].HW.Hardware() {
		t.Error("fallback stream should be software")
	}

	// Require it and it is an error.
	if _, err := BuildTranscodePlan(info, sys, "multi.mkv", "out.mp4", TranscodeOptions{HW: hwaccel.RequireHardware(hwaccel.CUDA)}); err == nil || !strings.Contains(err.Error(), "required for h264") {
		t.Errorf("require cuda = %v", err)
	}

	// Codec the backend cannot encode: hevc has no vaapi encoder here.
	plan, err = BuildTranscodePlan(info, sys, "multi.mkv", "out.mkv", TranscodeOptions{
		HW:    hwaccel.PreferHardware(),
		Video: VideoRule{Encode: encode.Video{Codec: encode.HEVC, Encoder: "libx265"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := args(plan); !strings.Contains(got, "-c:v:0 libx265") || strings.Contains(got, "hwupload") {
		t.Errorf("explicit encoder must win:\n%s", got)
	}

	// An explicit encode.Video.HW with no System is trusted.
	plan, err = BuildTranscodePlan(info, nil, "multi.mkv", "out.mp4", TranscodeOptions{
		Video: VideoRule{Encode: encode.Video{Codec: encode.H264, HW: hwaccel.CUDA}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got = args(plan)
	if !strings.HasPrefix(got, "-init_hw_device cuda=hw -filter_hw_device hw ") || !strings.Contains(got, "-c:v:0 h264_nvenc") || !strings.Contains(got, "hwupload_cuda") {
		t.Errorf("explicit HW args:\n%s", got)
	}
	// With a System, the same request is checked against it.
	if _, err := BuildTranscodePlan(info, sys, "multi.mkv", "out.mp4", TranscodeOptions{
		Video: VideoRule{Encode: encode.Video{Codec: encode.H264, HW: hwaccel.CUDA}},
	}); err == nil {
		t.Error("explicit CUDA on a machine where the probe failed should error")
	}
}

func TestPackagePlanHardware(t *testing.T) {
	info := captureInfo(t, "multi_mkv.basic.json")
	sys := vaapiSystem()
	res, err := BuildPackagePlan(info, sys, "in.mkv", "out", PackageOptions{
		HW:         hwaccel.PreferHardware(),
		Renditions: []Rendition{{Height: 32, Bitrate: "200k"}, {Height: 16, Bitrate: "100k", Video: &encode.Video{Profile: "main"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(res.Command.Args(), " ")
	for _, want := range []string{
		"-init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw",
		"[v0]scale=w=-2:h=32,format=nv12,hwupload[v0out]",
		"[v1]scale=w=-2:h=16,format=nv12,hwupload[v1out]",
		"-c:v:0 h264_vaapi",
		"-c:v:1 h264_vaapi",
		"-profile:v:1 main",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args lack %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-pix_fmt") {
		t.Errorf("hardware ladder must not force a pix_fmt:\n%s", got)
	}
	if res.HW.Kind != hwaccel.VAAPI || res.HWReason != "" {
		t.Errorf("result hw = %+v %q", res.HW, res.HWReason)
	}

	res, err = BuildPackagePlan(info, sys, "in.mkv", "out", PackageOptions{
		HW:         hwaccel.PreferHardware(hwaccel.CUDA),
		Renditions: []Rendition{{Height: 32, Bitrate: "200k"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Command.Args(), " "); !strings.Contains(got, "-c:v:0 libx264") || !strings.Contains(got, "-pix_fmt:v:0 yuv420p") {
		t.Errorf("fallback args:\n%s", got)
	}
	if res.HW.Hardware() || !strings.Contains(res.HWReason, "cuda: probe failed") {
		t.Errorf("fallback = %+v %q", res.HW, res.HWReason)
	}
}

func TestMergeVideo(t *testing.T) {
	base := encode.Video{Codec: encode.HEVC, Encoder: "hevc_vaapi", HW: hwaccel.VAAPI, Quality: 24, Speed: encode.Fast, Profile: "main"}
	v := mergeVideo(base, encode.Video{Profile: "main10"})
	if v.Encoder != "hevc_vaapi" || v.HW != hwaccel.VAAPI || v.Quality != 24 || v.Speed != encode.Fast || v.Profile != "main10" {
		t.Errorf("merged = %+v", v)
	}
	v = mergeVideo(base, encode.Video{Codec: encode.H264})
	if v.Encoder != "" || v.HW != hwaccel.None || v.Codec != encode.H264 || v.Quality != 24 {
		t.Errorf("own codec = %+v", v)
	}
}

// tenBitSystem is a machine whose VAAPI device encodes HEVC at 10 bits
// but H.264 only at 8, as a Tiger Lake iGPU does.
func tenBitSystem() *hwaccel.System {
	sys := vaapiSystem()
	sys.Caps.Encoders = append(sys.Caps.Encoders, caps.Codec{Name: "hevc_vaapi", Type: caps.Video})
	sys.Probes[hwaccel.VAAPI] = []hwaccel.ProbeResult{{
		Kind: hwaccel.VAAPI, Device: "/dev/dri/renderD128", Available: true,
		Encoders: []hwaccel.EncoderProbe{
			{Codec: "h264", Encoder: "h264_vaapi", Works: true, Formats: []string{hwaccel.NV12}},
			{Codec: "hevc", Encoder: "hevc_vaapi", Works: true, Formats: []string{hwaccel.NV12, hwaccel.P010}},
		},
	}}
	return sys
}

// TestTranscodePlanKeepsSourceDepth checks that a 10-bit source is
// uploaded as p010 when the device encodes it, and flattened to nv12 when
// it does not -- the upload format is the only thing standing between a
// Main 10 source and an 8-bit output.
func TestTranscodePlanKeepsSourceDepth(t *testing.T) {
	info := captureInfo(t, "hdr_mp4.basic.json")
	sys := tenBitSystem()

	plan, err := BuildTranscodePlan(info, sys, "hdr.mp4", "out.mp4", TranscodeOptions{
		HW:    hwaccel.PreferHardware(),
		Video: VideoRule{Encode: encode.Video{Codec: encode.HEVC}, MaxHeight: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := args(plan); !strings.Contains(got, "-filter:v:0 scale=w=-2:h=16,format=p010,hwupload") {
		t.Errorf("10-bit source should upload as p010:\n%s", got)
	}
	if sel := plan.Streams[0].HW; sel.Format != hwaccel.P010 {
		t.Errorf("planned selection format = %q", sel.Format)
	}

	// The same source to H.264, which this device only encodes 8-bit.
	plan, err = BuildTranscodePlan(info, sys, "hdr.mp4", "out.mp4", TranscodeOptions{
		HW:    hwaccel.PreferHardware(),
		Video: VideoRule{Encode: encode.Video{Codec: encode.H264}, MaxHeight: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := args(plan); !strings.Contains(got, "format=nv12,hwupload") {
		t.Errorf("a device that only encodes 8-bit h264 must upload nv12:\n%s", got)
	}

	// An 8-bit source is unaffected.
	plan, err = BuildTranscodePlan(captureInfo(t, "multi_mkv.basic.json"), sys, "multi.mkv", "out.mp4", TranscodeOptions{
		HW:    hwaccel.PreferHardware(),
		Video: VideoRule{Encode: encode.Video{Codec: encode.HEVC}, MaxHeight: 16},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := args(plan); !strings.Contains(got, "format=nv12,hwupload") {
		t.Errorf("an 8-bit source must still upload nv12:\n%s", got)
	}
}
