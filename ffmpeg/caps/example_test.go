package caps_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
)

func ExampleDetect() {
	ctx := context.Background()
	set, err := caps.Detect(ctx, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(set.Version.Version, set.HasEncoder("libx264"), len(set.EncodersFor("hevc")))
}

func ExampleCheck() {
	ctx := context.Background()
	set, err := caps.Detect(ctx, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	cmd := ffmpeg.NewCommand().Input("in.mkv").Output("out.mp4", ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(-5))
	err = caps.Check(ctx, set, nil, cmd)
	var ce *caps.CheckError
	if errors.As(err, &ce) {
		for _, p := range ce.Problems {
			fmt.Println(p)
		}
	}
}

func ExampleParseCodecs() {
	codecs := caps.ParseCodecs("Encoders:\n V....D libx264              libx264 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10 (codec h264)\n A....D aac                  AAC (Advanced Audio Coding)\n")
	for _, c := range codecs {
		fmt.Println(c.Name, c.Type, c.CodecID)
	}
	// Output:
	// libx264 video h264
	// aac audio aac
}
