package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffprobe"
)

func TestFitSize(t *testing.T) {
	cases := []struct{ sw, sh, mw, mh, w, h int }{
		{1920, 1080, 320, 0, 320, 180},
		{1080, 1920, 320, 0, 320, 568},
		{1920, 1080, 0, 0, 1920, 1080},
		{1920, 1080, 4000, 4000, 1920, 1080},
		{1920, 1080, 640, 100, 178, 100},
		{0, 0, 160, 0, 160, 0},
	}
	for _, c := range cases {
		if w, h := fitSize(c.sw, c.sh, c.mw, c.mh); w != c.w || h != c.h {
			t.Errorf("fitSize(%d,%d,%d,%d) = %dx%d, want %dx%d", c.sw, c.sh, c.mw, c.mh, w, h, c.w, c.h)
		}
	}
	if scaleFilter(320, 0) != "scale=w=320:h=-2" || scaleFilter(0, 0) != "" || !strings.Contains(scaleFilter(320, 240), "force_original_aspect_ratio") {
		t.Error("scaleFilter")
	}
}

func TestWebVTT(t *testing.T) {
	tp := &TrickplayResult{Interval: 10 * time.Second, TileWidth: 160, TileHeight: 90, Columns: 2, Rows: 2, Count: 5, Duration: 45 * time.Second}
	vtt := tp.WebVTT("https://cdn/x/", "sheet-%03d.jpg")
	want := "WEBVTT\n\n" +
		"00:00:00.000 --> 00:00:10.000\nhttps://cdn/x/sheet-001.jpg#xywh=0,0,160,90\n\n" +
		"00:00:10.000 --> 00:00:20.000\nhttps://cdn/x/sheet-001.jpg#xywh=160,0,160,90\n\n" +
		"00:00:20.000 --> 00:00:30.000\nhttps://cdn/x/sheet-001.jpg#xywh=0,90,160,90\n\n" +
		"00:00:30.000 --> 00:00:40.000\nhttps://cdn/x/sheet-001.jpg#xywh=160,90,160,90\n\n" +
		"00:00:40.000 --> 00:00:45.000\nhttps://cdn/x/sheet-002.jpg#xywh=0,0,160,90\n\n"
	if vtt != want {
		t.Errorf("WebVTT:\n%s\nwant:\n%s", vtt, want)
	}
	if s, x, y := tp.TileRect(4); s != 1 || x != 0 || y != 0 {
		t.Errorf("TileRect(4) = %d %d %d", s, x, y)
	}
	if vttTime(3723456*time.Millisecond) != "01:02:03.456" {
		t.Error("vttTime")
	}
}

// ─── live ──────────────────────────────────────────────────────────────────

func media(name string) string {
	return filepath.Join("..", "ffprobe", "testdata", "media", name)
}

func requireTools(t *testing.T) {
	t.Helper()
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if err := ffprobe.ValidateInstall(); err != nil {
		t.Skip("ffprobe not installed")
	}
}

func probeImage(t *testing.T, path string) (w, h int, codec string) {
	t.Helper()
	res, err := ffprobe.Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	v := res.VideoStream()
	if v == nil {
		t.Fatalf("%s: no video stream", path)
	}
	w, h = v.Resolution()
	return w, h, v.CodecName
}

func TestThumbnailLive(t *testing.T) {
	requireTools(t)
	ctx := context.Background()
	dir := t.TempDir()

	at, err := Thumbnail(ctx, media("basic.mp4"), filepath.Join(dir, "a.jpg"), ThumbnailOptions{Width: 24})
	if err != nil {
		t.Fatal(err)
	}
	if at != 100*time.Millisecond { // 10% of 1s
		t.Errorf("at = %v", at)
	}
	if w, h, codec := probeImage(t, filepath.Join(dir, "a.jpg")); w != 24 || h != 24 || codec != "mjpeg" {
		t.Errorf("jpg = %dx%d %s", w, h, codec)
	}
	if _, err := Thumbnail(ctx, media("basic.mp4"), filepath.Join(dir, "b.png"), ThumbnailOptions{At: 500 * time.Millisecond, Smart: true}); err != nil {
		t.Fatal(err)
	}
	if w, _, codec := probeImage(t, filepath.Join(dir, "b.png")); w != 32 || codec != "png" {
		t.Errorf("png = %d %s", w, codec)
	}
	if _, err := Thumbnail(ctx, media("basic.mp4"), filepath.Join(dir, "c.webp"), ThumbnailOptions{Width: 16, Height: 8}); err != nil {
		t.Fatal(err)
	}
	if w, h, _ := probeImage(t, filepath.Join(dir, "c.webp")); w != 8 || h != 8 {
		t.Errorf("webp fit = %dx%d", w, h)
	}
	// Rotated input: display size is portrait, so a width bound applies to the short side.
	if _, err := Thumbnail(ctx, media("rotated.mp4"), filepath.Join(dir, "r.jpg"), ThumbnailOptions{Width: 16}); err != nil {
		t.Fatal(err)
	}
	if _, err := Thumbnail(ctx, media("audio.flac"), filepath.Join(dir, "x.jpg"), ThumbnailOptions{}); err == nil {
		t.Error("audio-only input should fail")
	}
}

func TestThumbnailsLive(t *testing.T) {
	requireTools(t)
	ctx := context.Background()
	dir := t.TempDir()
	files, err := Thumbnails(ctx, media("basic.mp4"), filepath.Join(dir, "t-%02d.jpg"), ThumbnailsOptions{Count: 4, Width: 16})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 || files[0].Time != 125*time.Millisecond || files[3].Time != 875*time.Millisecond {
		t.Errorf("files = %+v", files)
	}
	for _, f := range files {
		if _, err := os.Stat(f.Path); err != nil {
			t.Errorf("missing %s", f.Path)
		}
	}
	files, err = Thumbnails(ctx, media("basic.mp4"), filepath.Join(dir, "i-%02d.png"), ThumbnailsOptions{Interval: 400 * time.Millisecond})
	if err != nil || len(files) != 3 || files[2].Time != 800*time.Millisecond {
		t.Errorf("interval files = %+v %v", files, err)
	}
}

func TestTrickplayLive(t *testing.T) {
	requireTools(t)
	ctx := context.Background()
	dir := t.TempDir()
	tp, err := Trickplay(ctx, media("basic.mp4"), dir, TrickplayOptions{Interval: 250 * time.Millisecond, TileWidth: 16, Columns: 2, Rows: 2, BaseURL: "/v/"})
	if err != nil {
		t.Fatal(err)
	}
	if tp.Count != 4 || len(tp.Sheets) != 1 || tp.TileWidth != 16 || tp.TileHeight != 16 {
		t.Errorf("trickplay = %+v", tp)
	}
	if w, h, _ := probeImage(t, tp.Sheets[0]); w != 32 || h != 32 {
		t.Errorf("sheet = %dx%d", w, h)
	}
	vtt, err := os.ReadFile(tp.VTT)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(vtt), "WEBVTT") || strings.Count(string(vtt), "-->") != 4 || !strings.Contains(string(vtt), "/v/sheet-001.jpg#xywh=16,16,16,16") {
		t.Errorf("vtt:\n%s", vtt)
	}
	// Defaults: 1s of video and a 1s minimum interval gives one tile.
	tp, err = Trickplay(ctx, media("basic.mp4"), filepath.Join(dir, "d"), TrickplayOptions{})
	if err != nil || tp.Count != 1 || tp.Interval != time.Second || tp.TileWidth != 160 {
		t.Errorf("defaults = %+v %v", tp, err)
	}
}

func TestPreviewLive(t *testing.T) {
	requireTools(t)
	ctx := context.Background()
	dir := t.TempDir()
	for _, name := range []string{"p.mp4", "p.gif", "p.webp"} {
		out := filepath.Join(dir, name)
		if err := Preview(ctx, media("basic.mp4"), out, PreviewOptions{Duration: 500 * time.Millisecond, Width: 16, Audio: true}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "p.webp" {
			// ffmpeg's WebP decoder does not read animations, so ffprobe
			// reports no size; check the container instead.
			b, err := os.ReadFile(out)
			if err != nil || len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" || !strings.Contains(string(b), "ANIM") {
				t.Errorf("webp header = %q %v", b[:min(len(b), 12)], err)
			}
			continue
		}
		res, err := ffprobe.Probe(ctx, out)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if w, _ := res.VideoStream().Resolution(); w != 16 {
			t.Errorf("%s width = %d", name, w)
		}
		if name == "p.mp4" && (res.AudioStream() == nil || res.VideoStream().CodecName != "h264") {
			t.Errorf("mp4 streams = %+v", res.Streams)
		}
	}
}

func TestWaveformLive(t *testing.T) {
	requireTools(t)
	out := filepath.Join(t.TempDir(), "w.png")
	if err := Waveform(context.Background(), media("basic.mp4"), out, WaveformOptions{Width: 200, Height: 50}); err != nil {
		t.Fatal(err)
	}
	if w, h, codec := probeImage(t, out); w != 200 || h != 50 || codec != "png" {
		t.Errorf("waveform = %dx%d %s", w, h, codec)
	}
	if err := Waveform(context.Background(), media("raw.h264"), filepath.Join(t.TempDir(), "x.png"), WaveformOptions{}); err == nil {
		t.Error("video-only input should fail")
	}
}
