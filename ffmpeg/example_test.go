package ffmpeg_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
)

func ExampleNewCommand() {
	cmd := ffmpeg.NewCommand().
		Input("in.mkv", ffmpeg.Seek(90*time.Second)).
		Output("out.mp4",
			ffmpeg.Map("0:v:0"), ffmpeg.Map("0:a:m:language:eng"),
			ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(20), ffmpeg.Preset("slow"),
			ffmpeg.AudioCodec("aac"), ffmpeg.BitRate("a", "160k"),
			ffmpeg.MovFlags("+faststart"))
	fmt.Println(cmd)
	// Output: ffmpeg -ss 00:01:30.00 -i in.mkv -map 0:v:0 -map 0:a:m:language:eng -c:v libx264 -crf 20 -preset slow -c:a aac -b:a 160k -movflags +faststart out.mp4
}

func ExampleCommand_Validate() {
	cmd := ffmpeg.NewCommand().
		Input("in.mkv").
		Output("out.mp4", ffmpeg.FilterComplexString("[0:v]scale=1280:-2[v]"))
	err := cmd.Validate()
	fmt.Println(errors.Is(err, ffmpeg.ErrInvalidCommand), err)
	// Output: true ffmpeg: invalid command: output 0: -filter_complex is a global option
}

func ExampleRun() {
	ctx := context.Background()
	cmd := ffmpeg.NewCommand().Input("in.mkv").Output("out.mp4", ffmpeg.VideoCodec("libx264"), ffmpeg.AudioCodec("aac"))

	res, err := ffmpeg.Run(ctx, cmd,
		ffmpeg.TotalDuration(90*time.Minute), // from ffprobe; enables Fraction and ETA
		ffmpeg.OnProgress(func(p ffmpeg.Progress) {
			fmt.Printf("\r%3.0f%% %.1fx eta %s", p.Fraction*100, p.Speed, p.ETA.Round(time.Second))
		}))
	var ffErr *ffmpeg.Error
	if errors.As(err, &ffErr) {
		fmt.Println(ffErr.Result.LastLogLine()) // ffmpeg's own error line
		return
	}
	fmt.Println(res.Duration, res.Progress.Frame)
}

func ExampleTwoPass() {
	ctx := context.Background()
	cmd := ffmpeg.NewCommand().Input("in.mkv").
		Output("out.mp4", ffmpeg.VideoCodec("libx264"), ffmpeg.BitRate("v", "2500k"), ffmpeg.AudioCodec("aac"))
	res, err := ffmpeg.TwoPass(ctx, cmd, ffmpeg.TwoPassOptions{
		Duration:   90 * time.Minute,
		OnProgress: func(p ffmpeg.TwoPassProgress) { fmt.Printf("pass %d %3.0f%%\n", p.Pass, p.Fraction*100) },
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(res.Second.ExitCode)
}

func ExampleParseVersion() {
	v, _ := ffmpeg.ParseVersion("ffmpeg version 7.1.5-0+deb13u1 Copyright (c) 2000-2025\nlibavcodec     61. 19.101 / 61. 19.101\n")
	fmt.Println(v.Major, v.Minor, v.Patch, v.AtLeast(7, 1), v.LibraryAtLeast("libavcodec", 61, 19))
	snap, _ := ffmpeg.ParseVersion("ffmpeg version N-118000-g1a2b3c4d5e Copyright (c) 2000-2025\n")
	fmt.Println(snap.Snapshot, snap.AtLeast(8, 0))
	// Output:
	// 7 1 5 true true
	// true true
}

func ExampleNewConcat() {
	c := ffmpeg.NewConcat("part1.mp4", "part2.mp4")
	fmt.Print(c)
	// Output:
	// ffconcat version 1.0
	// file 'part1.mp4'
	// file 'part2.mp4'
}
