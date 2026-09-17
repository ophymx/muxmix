package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestInfoReuse(t *testing.T) {
	// A caller-supplied Info is trusted without probing: a silent Info
	// makes Thumbnail refuse before any binary runs.
	ctx := context.Background()
	stub := &Info{Duration: time.Second}
	if _, err := Thumbnail(ctx, "missing.mp4", filepath.Join(t.TempDir(), "x.jpg"), ThumbnailOptions{Info: stub}); err == nil || !strings.Contains(err.Error(), "no video stream") {
		t.Errorf("Thumbnail with stub Info = %v", err)
	}
	if err := Waveform(ctx, "missing.mp4", filepath.Join(t.TempDir(), "x.png"), WaveformOptions{Info: stub}); err == nil || !strings.Contains(err.Error(), "no audio stream") {
		t.Errorf("Waveform with stub Info = %v", err)
	}
	info := captureInfo(t, "multi_mkv.basic.json")
	plan, err := Default.PlanTranscode(ctx, "missing.mkv", "out.mp4", TranscodeOptions{Info: info})
	if err != nil || plan.Info != info {
		t.Errorf("PlanTranscode with Info: %v", err)
	}
}

func TestPreScaleFiltersLive(t *testing.T) {
	requireTools(t)
	ctx := context.Background()
	dir := t.TempDir()
	// basic.mp4 is 32x32; keeping the left half gives 16x32, scaled to 24 wide -> 24x48.
	crop := []string{"crop=iw/2:ih:0:0"}
	if _, err := Thumbnail(ctx, media("basic.mp4"), filepath.Join(dir, "t.png"), ThumbnailOptions{Width: 24, Filters: crop}); err != nil {
		t.Fatal(err)
	}
	if w, h, _ := probeImage(t, filepath.Join(dir, "t.png")); w != 24 || h != 48 {
		t.Errorf("thumbnail = %dx%d, want 24x48", w, h)
	}
	tp, err := Trickplay(ctx, media("basic.mp4"), filepath.Join(dir, "tp"), TrickplayOptions{TileWidth: 16, Columns: 2, Rows: 2, Filters: crop})
	if err != nil {
		t.Fatal(err)
	}
	if tp.TileWidth != 16 || tp.TileHeight != 32 {
		t.Errorf("tile = %dx%d, want 16x32", tp.TileWidth, tp.TileHeight)
	}
	if err := Preview(ctx, media("basic.mp4"), filepath.Join(dir, "p.mp4"), PreviewOptions{Width: 16, Filters: crop, Duration: 200 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if w, h, _ := probeImage(t, filepath.Join(dir, "p.mp4")); w != 16 || h != 32 {
		t.Errorf("preview = %dx%d, want 16x32", w, h)
	}
}
