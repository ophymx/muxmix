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

func TestDefaultLadder(t *testing.T) {
	if l := DefaultLadder(1080); len(l) != 4 || l[0].Name != "1080p" || l[3].Height != 360 {
		t.Errorf("1080 = %+v", l)
	}
	if l := DefaultLadder(720); len(l) != 3 || l[0].Name != "720p" {
		t.Errorf("720 = %+v", l)
	}
	if l := DefaultLadder(2160); len(l) != 4 {
		t.Errorf("2160 = %+v", l)
	}
	if l := DefaultLadder(240); len(l) != 1 || l[0].Height != 240 || l[0].Name != "240p" {
		t.Errorf("240 = %+v", l)
	}
	if scaleRate("2800k", 1.07) != "2996k" || scaleRate("5M", 1.5) != "8M" {
		t.Error("scaleRate")
	}
}

func TestBuildPackagePlanHLS(t *testing.T) {
	info := captureInfo(t, "multi_mkv.basic.json") // 32x32, 25 fps, aac eng default + opus deu
	res, err := BuildPackagePlan(info, "in.mkv", "out", PackageOptions{
		Renditions:      []Rendition{{Name: "hi", Height: 32, Bitrate: "100k"}, {Name: "lo", Height: 16, Bitrate: "50k", MaxRate: "60k", BufSize: "90k"}},
		SegmentDuration: 2 * time.Second,
		Video:           encode.Video{Speed: encode.Fastest},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(res.Command.Args(), " ")
	for _, want := range []string{
		"-filter_complex [0:v:0]split=2[v0][v1];[v0]scale=w=-2:h=32[v0out];[v1]scale=w=-2:h=16[v1out]",
		"-map [v0out] -c:v:0 libx264 -b:v:0 100k -maxrate:v:0 107k -bufsize:v:0 150k -preset:v:0 veryfast -pix_fmt:v:0 yuv420p -g:v:0 50 -keyint_min:v:0 50 -sc_threshold:v:0 0",
		"-map [v1out] -c:v:1 libx264 -b:v:1 50k -maxrate:v:1 60k -bufsize:v:1 90k",
		"-map 0:1 -c:a:0 aac -b:a:0 128k",
		"-force_key_frames expr:gte(t,n_forced*2)",
		"-f hls -hls_time 2 -hls_playlist_type vod -hls_list_size 0 -hls_flags independent_segments -hls_segment_type fmp4 -master_pl_name master.m3u8",
		"-var_stream_map a:0,agroup:audio,name:audio-eng,language:eng,default:yes v:0,agroup:audio,name:hi v:1,agroup:audio,name:lo",
		"-hls_segment_filename out/%v/seg_%05d.m4s out/%v/index.m3u8",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("args missing %q:\n%s", want, a)
		}
	}
	if res.Master != filepath.Join("out", "master.m3u8") || len(res.Renditions) != 2 || res.Renditions[1].Playlist != filepath.Join("out", "lo", "index.m3u8") || res.Renditions[1].Width != 16 {
		t.Errorf("result = %+v", res)
	}
	if len(res.Audio) != 1 || res.Audio[0].Language != "eng" || res.Audio[0].Playlist != filepath.Join("out", "audio-eng", "index.m3u8") {
		t.Errorf("audio = %+v", res.Audio)
	}

	// Two audio languages, MPEG-TS segments.
	res, err = BuildPackagePlan(info, "in.mkv", "out", PackageOptions{
		Segments: MPEGTS, AudioLanguages: []string{"deu", "eng"},
	})
	if err != nil {
		t.Fatal(err)
	}
	a = strings.Join(res.Command.Args(), " ")
	if !strings.Contains(a, "-map 0:2 -c:a:0 aac -b:a:0 128k -map 0:1 -c:a:1 aac -b:a:1 128k") ||
		!strings.Contains(a, "-var_stream_map a:1,agroup:audio,name:audio-eng,language:eng a:0,agroup:audio,name:audio-deu,language:deu,default:yes v:0,agroup:audio,name:32p") ||
		!strings.Contains(a, "seg_%05d.ts") {
		t.Errorf("multi-audio args:\n%s", a)
	}
	if len(res.Renditions) != 1 || res.Renditions[0].Name != "32p" { // default ladder for a 32-line source
		t.Errorf("renditions = %+v", res.Renditions)
	}
}

func TestBuildPackagePlanDASH(t *testing.T) {
	info := captureInfo(t, "basic_mp4.basic.json")
	res, err := BuildPackagePlan(info, "in.mp4", "out", PackageOptions{Format: CMAF})
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(res.Command.Args(), " ")
	for _, want := range []string{
		"-f dash -seg_duration 6 -use_template 1 -use_timeline 1 -init_seg_name init-$RepresentationID$.m4s -media_seg_name chunk-$RepresentationID$-$Number%05d$.m4s -adaptation_sets id=0,streams=v id=1,streams=a -hls_playlist 1 -hls_master_name master.m3u8 out/manifest.mpd",
	} {
		if !strings.Contains(a, want) {
			t.Errorf("args missing %q:\n%s", want, a)
		}
	}
	if res.Manifest != filepath.Join("out", "manifest.mpd") || res.Master != filepath.Join("out", "master.m3u8") {
		t.Errorf("result = %+v", res)
	}
	if _, err := BuildPackagePlan(info, "in.mp4", "out", PackageOptions{Format: DASH, Segments: MPEGTS}); err == nil {
		t.Error("DASH with TS should fail")
	}
	// Video-only source: no audio group.
	raw := captureInfo(t, "raw_h264.basic.json")
	res, err = BuildPackagePlan(raw, "in.h264", "out", PackageOptions{Renditions: []Rendition{{Height: 32, Bitrate: "100k"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a := strings.Join(res.Command.Args(), " "); !strings.Contains(a, "-var_stream_map v:0,name:32p") || strings.Contains(a, "agroup") {
		t.Errorf("video-only args:\n%s", a)
	}
}

func TestPackageLive(t *testing.T) {
	requireTools(t)
	ctx := context.Background()
	ladder := []Rendition{{Name: "hi", Height: 32, Bitrate: "100k"}, {Name: "lo", Height: 16, Bitrate: "50k"}}
	fast := encode.Video{Speed: encode.Fastest}

	dir := filepath.Join(t.TempDir(), "hls")
	res, err := Package(ctx, media("multi.mkv"), dir, PackageOptions{Renditions: ladder, Video: fast, SegmentDuration: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("%v\n%s", err, res.Command)
	}
	master, err := os.ReadFile(res.Master)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(master), "#EXT-X-MEDIA:TYPE=AUDIO") || strings.Count(string(master), "#EXT-X-STREAM-INF") != 2 || !strings.Contains(string(master), "hi/index.m3u8") {
		t.Errorf("master:\n%s", master)
	}
	for _, r := range res.Renditions {
		pl, err := os.ReadFile(r.Playlist)
		if err != nil || !strings.Contains(string(pl), "#EXT-X-MAP") || strings.Count(string(pl), ".m4s") < 2 {
			t.Errorf("%s playlist:\n%s %v", r.Name, pl, err)
		}
	}
	if _, err := os.Stat(res.Audio[0].Playlist); err != nil {
		t.Error(err)
	}

	dir = filepath.Join(t.TempDir(), "ts")
	res, err = Package(ctx, media("multi.mkv"), dir, PackageOptions{Renditions: ladder[:1], Segments: MPEGTS, Video: fast, SegmentDuration: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("%v\n%s", err, res.Command)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "hi", "seg_*.ts")); len(files) < 2 {
		t.Errorf("ts segments = %v", files)
	}

	dir = filepath.Join(t.TempDir(), "cmaf")
	res, err = Package(ctx, media("multi.kmv"), dir, PackageOptions{Format: CMAF})
	if err == nil {
		t.Error("expected error for missing input")
	}
	res, err = Package(ctx, media("multi.mkv"), dir, PackageOptions{Format: CMAF, Renditions: ladder, Video: fast, SegmentDuration: 500 * time.Millisecond})
	if err != nil {
		t.Fatalf("%v\n%s", err, res.Command)
	}
	mpd, err := os.ReadFile(res.Manifest)
	if err != nil || strings.Count(string(mpd), "<Representation ") != 3 {
		t.Errorf("mpd: %v\n%s", err, mpd)
	}
	if _, err := os.Stat(res.Master); err != nil {
		t.Errorf("cmaf hls master: %v", err)
	}
	if _, err := os.Stat(res.Renditions[0].Playlist); err != nil {
		t.Errorf("cmaf media playlist: %v", err)
	}
}
