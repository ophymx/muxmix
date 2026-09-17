package encode_test

import (
	"fmt"
	"strings"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/encode"
)

func ExampleVideo_OptsFor() {
	v := encode.Video{Codec: encode.H264, Quality: 22, Speed: encode.Fast, Profile: "high"}
	for _, enc := range []string{"libx264", "h264_nvenc", "libsvtav1"} {
		out := ffmpeg.NewOutput("o.mp4", v.OptsFor(enc)...)
		fmt.Println(strings.Join(out.Options.Args(), " "))
	}
	// Output:
	// -c:v libx264 -crf 22 -preset fast -profile:v high
	// -c:v h264_nvenc -rc vbr -cq 22 -b:v 0 -preset p2 -profile:v high
	// -c:v libsvtav1 -crf 27 -preset 9 -profile:v high
}

func ExampleVideo_Opts() {
	// With a caps.Set the encoder is chosen from what the build has; nil
	// takes the first preference.
	v := encode.Video{Codec: encode.AV1, Quality: 30}
	a := encode.Audio{Codec: encode.Opus, Bitrate: "128k"}
	cmd := ffmpeg.NewCommand().Input("in.mkv").
		Output("out.webm", append(v.MustOpts(nil), a.MustOpts(nil)...)...)
	fmt.Println(cmd.Args())
	// Output: [-i in.mkv -c:v libsvtav1 -crf 37 -c:a libopus -b:a 128k out.webm]
}
