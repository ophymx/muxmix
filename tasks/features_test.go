package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg/encode"
)

func TestPlanFilters(t *testing.T) {
	info := captureInfo(t, "multi_mkv.basic.json")
	plan, err := BuildTranscodePlan(info, nil, "multi.mkv", "out.mp4", TranscodeOptions{
		Video: VideoRule{Filters: []string{"crop=16:16", "hqdn3d"}, MaxHeight: 8},
		Audio: AudioRule{Filters: []string{"loudnorm=I=-16:TP=-1.5:LRA=11"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := args(plan)
	for _, want := range []string{
		"-filter:v:0 crop=16:16,hqdn3d,scale=w=-2:h=8",
		"-filter:a:0 loudnorm=I=-16:TP=-1.5:LRA=11",
		"-filter:a:1 loudnorm=I=-16:TP=-1.5:LRA=11",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args lack %q:\n%s", want, got)
		}
	}
	if plan.TwoPass {
		t.Error("quality encode must not plan two passes")
	}

	plan, err = BuildTranscodePlan(info, nil, "multi.mkv", "out.mp4", TranscodeOptions{
		TwoPass: true,
		Video:   VideoRule{Encode: encode.Video{Codec: encode.H264, Bitrate: "1M"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.TwoPass {
		t.Error("bitrate encode with TwoPass should plan two passes")
	}
}

func TestPackagePlanSkipsUpscale(t *testing.T) {
	info := captureInfo(t, "multi_mkv.basic.json") // 32 lines tall
	res, err := BuildPackagePlan(info, nil, "in.mkv", "out", PackageOptions{
		Renditions: []Rendition{{Height: 1080, Bitrate: "5M"}, {Name: "tiny", Height: 16, Bitrate: "100k"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renditions) != 1 || res.Renditions[0].Name != "tiny" || len(res.Skipped) != 1 || res.Skipped[0] != "1080p" {
		t.Errorf("renditions = %+v skipped = %v", res.Renditions, res.Skipped)
	}
	if strings.Contains(strings.Join(res.Command.Args(), " "), "h=1080") {
		t.Error("skipped rung still in the command")
	}
	if res.Duration != info.Duration {
		t.Errorf("Duration = %v", res.Duration)
	}
	if _, err := BuildPackagePlan(info, nil, "in.mkv", "out", PackageOptions{Renditions: []Rendition{{Height: 1080, Bitrate: "5M"}}}); err == nil {
		t.Error("all rungs taller than the source should error")
	}
}

func TestOutputDirCreated(t *testing.T) {
	requireTools(t)
	out := filepath.Join(t.TempDir(), "a", "b", "thumb.jpg")
	if _, err := Thumbnail(context.Background(), media("basic.mp4"), out, ThumbnailOptions{Width: 16}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Error(err)
	}
}
