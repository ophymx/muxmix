package hwaccel_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
)

func ExampleDetectCached() {
	ctx := context.Background()
	sys, fromCache, err := hwaccel.DetectCached(ctx, nil, "/var/cache/app/ffmpeg.json", 24*time.Hour, hwaccel.ProbeOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(fromCache, sys.AvailableKinds())
	for kind, probes := range sys.Probes {
		if !sys.Available(kind) {
			fmt.Println(kind, "unavailable:", probes[0].Error)
		}
	}
}

// machine is a hand-built System: VAAPI works on one render node, CUDA
// was compiled in but its probe failed.
func machine() *hwaccel.System {
	return &hwaccel.System{
		Caps: &caps.Set{
			Encoders: []caps.Codec{{Name: "h264_vaapi", Type: caps.Video}, {Name: "h264_nvenc", Type: caps.Video}},
			HWAccels: []string{"vaapi", "cuda"},
		},
		Probes: map[hwaccel.Kind][]hwaccel.ProbeResult{
			hwaccel.VAAPI: {{Kind: hwaccel.VAAPI, Device: "/dev/dri/renderD128", Available: true}},
			hwaccel.CUDA:  {{Kind: hwaccel.CUDA, Error: "Cannot load libcuda.so.1"}},
		},
	}
}

func ExampleSystem_Select() {
	sys := machine()
	sel, err := sys.Select("h264", hwaccel.CUDA, hwaccel.VAAPI)
	fmt.Println(sel, err)

	cmd := ffmpeg.NewCommand().Input("in.mkv")
	sel.Apply(cmd)
	cmd.Output("out.mp4", sel.Opts("v:0", "scale=1280:-2")...)
	fmt.Println(strings.Join(cmd.Args(), " "))

	_, err = sys.Select("hevc", hwaccel.VAAPI, hwaccel.CUDA)
	fmt.Println(err)
	// Output:
	// h264_vaapi on /dev/dri/renderD128 <nil>
	// -init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw -i in.mkv -filter:v:0 scale=1280:-2,format=nv12,hwupload -c:v h264_vaapi out.mp4
	// hwaccel: no usable backend for hevc: vaapi: build lacks hevc_vaapi; cuda: build lacks hevc_nvenc
}

func ExamplePolicy_Resolve() {
	sys := machine()
	sel, reason, err := hwaccel.PreferHardware(hwaccel.CUDA).Resolve(sys, "h264")
	fmt.Println(sel, "|", reason, "|", err)
	_, _, err = hwaccel.RequireHardware(hwaccel.CUDA).Resolve(sys, "h264")
	fmt.Println(err)
	// Output:
	// software | cuda: probe failed: Cannot load libcuda.so.1 | <nil>
	// hwaccel: hardware encoding required for h264 but unavailable: cuda: probe failed: Cannot load libcuda.so.1
}
