package ffmpeg_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffprobe"
)

func TestPassCommand(t *testing.T) {
	cmd := ffmpeg.NewCommand().Input("in.mkv", ffmpeg.Seek(time.Second)).
		Output("out.mp4", ffmpeg.Map("0:v"), ffmpeg.Map("0:a"), ffmpeg.VideoCodec("libx264"), ffmpeg.BitRate("v", "2M"), ffmpeg.AudioCodec("aac"), ffmpeg.MovFlags("+faststart")).
		Output("out.mkv", ffmpeg.VideoCodec("libx265"), ffmpeg.X265Params("aq-mode=3"), ffmpeg.BitRate("v", "1M")).
		Output("audio.m4a", ffmpeg.NoVideo(), ffmpeg.AudioCodec("aac"))

	first := strings.Join(ffmpeg.PassCommand(cmd, 1, "/tmp/stats").Args(), " ")
	want1 := "-ss 00:00:01.00 -i in.mkv " +
		"-map 0:v -map 0:a -c:v libx264 -b:v 2M -c:a aac -pass 1 -passlogfile /tmp/stats-out0 -an -sn -dn -f null - " +
		"-c:v libx265 -x265-params aq-mode=3:pass=1:stats=/tmp/stats-out1.x265.log -b:v 1M -an -sn -dn -f null -"
	if first != want1 {
		t.Errorf("pass 1\n got %s\nwant %s", first, want1)
	}
	second := strings.Join(ffmpeg.PassCommand(cmd, 2, "/tmp/stats").Args(), " ")
	want2 := "-ss 00:00:01.00 -i in.mkv " +
		"-map 0:v -map 0:a -c:v libx264 -b:v 2M -c:a aac -movflags +faststart -pass 2 -passlogfile /tmp/stats-out0 out.mp4 " +
		"-c:v libx265 -x265-params aq-mode=3:pass=2:stats=/tmp/stats-out1.x265.log -b:v 1M out.mkv " +
		"-vn -c:a aac audio.m4a"
	if second != want2 {
		t.Errorf("pass 2\n got %s\nwant %s", second, want2)
	}

	single := ffmpeg.NewCommand().Input("in").Output("out.webm", ffmpeg.VideoCodec("libvpx-vp9"), ffmpeg.BitRate("v", "1M"))
	if got := strings.Join(ffmpeg.PassCommand(single, 2, "p").Args(), " "); got != "-i in -c:v libvpx-vp9 -b:v 1M -pass 2 -passlogfile p out.webm" {
		t.Errorf("single = %s", got)
	}
	copyOnly := ffmpeg.NewCommand().Input("in").Output("out.mp4", ffmpeg.CopyAll())
	if _, err := ffmpeg.TwoPass(context.Background(), copyOnly, ffmpeg.TwoPassOptions{}); err == nil {
		t.Error("expected error when nothing encodes video")
	}
}

func TestTwoPassLive(t *testing.T) {
	requireFFmpeg(t)
	ctx := context.Background()
	out := filepath.Join(t.TempDir(), "out.mp4")
	cmd := ffmpeg.NewCommand().
		Input("testsrc2=size=64x64:rate=25:duration=2", ffmpeg.Lavfi()).
		Input("sine=duration=2", ffmpeg.Lavfi()).
		Output(out, ffmpeg.Map("0:v"), ffmpeg.Map("1:a"),
			ffmpeg.VideoCodec("libx264"), ffmpeg.Preset("ultrafast"), ffmpeg.BitRate("v", "200k"), ffmpeg.PixFmt("yuv420p"),
			ffmpeg.AudioCodec("aac"))
	var passes []int
	res, err := ffmpeg.TwoPass(ctx, cmd, ffmpeg.TwoPassOptions{
		Duration: 2 * time.Second,
		OnProgress: func(p ffmpeg.TwoPassProgress) {
			if len(passes) == 0 || passes[len(passes)-1] != p.Pass {
				passes = append(passes, p.Pass)
			}
			if p.Fraction < 0 || p.Fraction > 1 || (p.Pass == 1 && p.Fraction > 0.5) {
				t.Errorf("fraction %v on pass %d", p.Fraction, p.Pass)
			}
		},
		RunOptions: []ffmpeg.RunOption{ffmpeg.ProgressInterval(50 * time.Millisecond)},
	})
	if err != nil {
		t.Fatalf("%v (first=%v second=%v)", err, res.First != nil, res.Second != nil)
	}
	if res.First == nil || res.Second == nil || !res.Second.Progress.Done {
		t.Errorf("results = %+v", res)
	}
	if len(passes) != 2 || passes[0] != 1 || passes[1] != 2 {
		t.Errorf("passes = %v", passes)
	}
	info, err := ffprobe.Probe(ctx, out)
	if err != nil || info.VideoStream() == nil || info.AudioStream() == nil {
		t.Errorf("output = %+v %v", info, err)
	}
	if logs, _ := filepath.Glob(filepath.Join(filepath.Dir(out), "*.log*")); len(logs) != 0 {
		t.Errorf("stats left behind: %v", logs)
	}
}
