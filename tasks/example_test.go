package tasks_test

import (
	"context"
	"fmt"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
	"github.com/ophymx/muxmix/tasks"
)

func ExampleTranscode() {
	ctx := context.Background()
	plan, err := tasks.Transcode(ctx, "in.mkv", "out.mp4", tasks.TranscodeOptions{
		Video: tasks.VideoRule{
			CopyCodecs: tasks.AllCodecs,
			Encode:     encode.Video{Codec: encode.H264, Quality: 22},
			MaxHeight:  1080,
		},
		Audio: tasks.AudioRule{CopyCodecs: tasks.AllCodecs, Languages: []string{"eng", "jpn"}},
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(plan) // one line per stream: what happened and why
}

func ExampleTools() {
	ctx := context.Background()
	// Detect what this machine can do once, at startup.
	sys, _, err := hwaccel.DetectCached(ctx, nil, "/var/cache/app/ffmpeg.json", 24*time.Hour, hwaccel.ProbeOptions{})
	if err != nil {
		fmt.Println(err)
		return
	}
	tools := &tasks.Tools{System: sys}

	// Plan, then run with a per-job progress callback.
	plan, err := tools.PlanTranscode(ctx, "in.mkv", "out.mp4", tasks.TranscodeOptions{HW: hwaccel.PreferHardware()})
	if err != nil {
		fmt.Println(err)
		return
	}
	err = plan.Run(ctx, tools, ffmpeg.OnProgress(func(p ffmpeg.Progress) {
		fmt.Printf("\r%3.0f%% eta %s", p.Fraction*100, p.ETA.Round(time.Second))
	}))
	fmt.Println(err)
}

func ExamplePackage() {
	ctx := context.Background()
	res, err := tasks.Package(ctx, "movie.mkv", "out/movie", tasks.PackageOptions{
		Format:         tasks.HLS,
		AudioLanguages: []string{"eng", "jpn"},
		Video:          encode.Video{Speed: encode.Fast},
		HW:             hwaccel.PreferHardware(),
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(res.Master, res.HW, res.Skipped)
}
