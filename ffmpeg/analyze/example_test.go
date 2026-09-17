package analyze_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/analyze"
)

func ExampleSilence() {
	ctx := context.Background()
	gaps, err := analyze.Silence(ctx, "talk.wav", analyze.SilenceOptions{NoiseDB: -40, MinDuration: time.Second})
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, g := range gaps {
		fmt.Println(g.Start, g.End, g.Open)
	}
}

func ExampleLoudnorm() {
	ctx := context.Background()
	targets := analyze.DefaultLoudnormTargets
	stats, err := analyze.Loudnorm(ctx, "in.wav", targets)
	if err != nil {
		fmt.Println(err)
		return
	}
	cmd := ffmpeg.NewCommand().Input("in.wav").
		Output("out.wav", ffmpeg.AudioFilter(stats.SecondPass(targets)))
	_, err = ffmpeg.Run(ctx, cmd)
	fmt.Println(err)
}

func ExampleAnalyzer() {
	a := &analyze.Analyzer{Runner: ffmpeg.New(ffmpeg.WithBinary("/opt/ffmpeg/bin/ffmpeg"))}
	crop, err := a.CropDetect(context.Background(), "movie.mkv", analyze.CropOptions{},
		ffmpeg.Seek(10*time.Minute), ffmpeg.Duration(30*time.Second))
	if errors.Is(err, analyze.ErrNoResult) {
		fmt.Println("cropdetect logged nothing")
		return
	}
	if err == nil {
		fmt.Println(crop.Filter())
	}
}

func ExampleParseSilence() {
	log := "[silencedetect @ 0x1] silence_start: 1.5\n[silencedetect @ 0x1] silence_end: 3.25 | silence_duration: 1.75\n"
	for _, g := range analyze.ParseSilence(log) {
		fmt.Println(g.Start, g.End, g.Duration())
	}
	// Output: 1.5s 3.25s 1.75s
}
