package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffprobe"
)

// captureInfo loads a probe capture as the Info a plan is built from.
func captureInfo(t *testing.T, name string) *Info {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "ffprobe", "testdata", "probe", "7.1.5", name))
	if err != nil {
		t.Skip("no capture")
	}
	var res ffprobe.Result
	if err := json.Unmarshal(b, &res); err != nil {
		t.Fatal(err)
	}
	info := &Info{Probe: &res, Duration: res.Duration(), HasAudio: res.HasAudio()}
	if v := res.VideoStream(); v != nil {
		info.HasVideo = true
		info.Width, info.Height = v.Resolution()
	}
	return info
}

func args(p *TranscodePlan) string { return strings.Join(p.Command.Args(), " ") }

func TestPlanMatroskaToMP4(t *testing.T) {
	// multi.mkv: h264 video, aac eng (default), opus deu, subrip eng forced, attachment.
	info := captureInfo(t, "multi_mkv.basic.json")
	plan, err := BuildTranscodePlan(info, nil, "multi.mkv", "out.mp4", TranscodeOptions{
		Video: VideoRule{CopyCodecs: AllCodecs},
		Audio: AudioRule{CopyCodecs: AllCodecs},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "-map 0:0 -c:v:0 copy -map 0:1 -c:a:0 copy -map 0:2 -c:a:1 aac -b:a:1 160k -map 0:3 -c:s:0 mov_text -movflags +faststart out.mp4"
	if got := args(plan); !strings.HasSuffix(got, want) {
		t.Errorf("args\n got %s\nwant …%s", got, want)
	}
	kept := plan.Kept()
	if len(kept) != 4 || kept[0].Action != Copy || kept[2].Action != Encode || kept[2].Encoder != "aac" || kept[2].Language != "deu" || kept[3].Encoder != "mov_text" {
		t.Errorf("kept = %+v", kept)
	}
	if len(plan.Streams) != 5 || plan.Streams[4].Type != "attachment" || plan.Streams[4].Action != Drop {
		t.Errorf("streams = %+v", plan.Streams)
	}
	if s := plan.String(); !strings.Contains(s, "2 audio opus [deu]: encode → aac (opus not accepted by mp4)") {
		t.Errorf("String():\n%s", s)
	}
}

func TestPlanEncodeAndFilter(t *testing.T) {
	info := captureInfo(t, "multi_mkv.basic.json")
	plan, err := BuildTranscodePlan(info, nil, "multi.mkv", "out.mkv", TranscodeOptions{
		Video:        VideoRule{Encode: encode.Video{Codec: encode.HEVC, Quality: 24, Speed: encode.Fast}, MaxWidth: 16},
		Audio:        AudioRule{Languages: []string{"deu"}, CopyCodecs: AllCodecs},
		Subtitles:    SubtitleRule{Drop: true},
		DropChapters: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "-map 0:0 -c:v:0 libx265 -crf:v:0 24 -preset:v:0 fast -filter:v:0 scale=w=16:h=-2 -map 0:2 -c:a:0 copy -map_chapters -1 out.mkv"
	if got := args(plan); !strings.HasSuffix(got, want) {
		t.Errorf("args\n got %s\nwant …%s", got, want)
	}
	if plan.Streams[0].Reason != "scaled down" {
		t.Errorf("reason = %q", plan.Streams[0].Reason)
	}
}

func TestPlanDefaultsAndDrops(t *testing.T) {
	info := captureInfo(t, "cover_mp3.basic.json") // mp3 audio + png cover art
	plan, err := BuildTranscodePlan(info, nil, "in.mp3", "out.m4a", TranscodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := args(plan); !strings.HasSuffix(got, "-map 0:0 -c:a:0 aac -b:a:0 160k -movflags +faststart out.m4a") {
		t.Errorf("args = %s", got)
	}
	if plan.Streams[0].Type != "video" || plan.Streams[0].Reason != "cover art" {
		t.Errorf("cover = %+v", plan.Streams[0])
	}

	// Every stream dropped is an error.
	if _, err := BuildTranscodePlan(info, nil, "in.mp3", "out.mp4", TranscodeOptions{Video: VideoRule{Drop: true}, Audio: AudioRule{Drop: true}}); err == nil {
		t.Error("expected error when nothing is kept")
	}
	if _, err := BuildTranscodePlan(info, nil, "in.mp3", "out.xyz", TranscodeOptions{}); err == nil {
		t.Error("expected error for unknown container")
	}

	// WebM: copy allowed but nothing accepted, so everything is encoded.
	mkv := captureInfo(t, "multi_mkv.basic.json")
	plan, err = BuildTranscodePlan(mkv, nil, "in.mkv", "out.webm", TranscodeOptions{
		Video: VideoRule{CopyCodecs: AllCodecs, Encode: encode.Video{Codec: encode.VP9, Quality: 30}},
		Audio: AudioRule{CopyCodecs: AllCodecs, MainOnly: true, Encode: encode.Audio{Codec: encode.Opus, Bitrate: "96k"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := args(plan); !strings.Contains(got, "-c:v:0 libvpx-vp9 -crf:v:0 37 -b:v:0 0") || !strings.Contains(got, "-c:a:0 libopus -b:a:0 96k") || !strings.Contains(got, "-c:s:0 webvtt") {
		t.Errorf("webm args = %s", got)
	}
	if plan.Streams[2].Action != Drop || plan.Streams[2].Reason != "not the main audio stream" {
		t.Errorf("second audio = %+v", plan.Streams[2])
	}
}

func TestPerStream(t *testing.T) {
	var o ffmpeg.Options
	o.Add(ffmpeg.PerStream("a", 1, ffmpeg.AudioCodec("aac"), ffmpeg.BitRate("a", "128k"), ffmpeg.Channels(2)))
	if got := strings.Join(o.Args(), " "); got != "-c:a:1 aac -b:a:1 128k -ac:a:1 2" {
		t.Errorf("PerStream = %s", got)
	}
}

func TestTranscodeLive(t *testing.T) {
	requireTools(t)
	ctx := context.Background()
	out := filepath.Join(t.TempDir(), "out.mp4")
	plan, err := Transcode(ctx, media("multi.mkv"), out, TranscodeOptions{
		Video: VideoRule{CopyCodecs: AllCodecs},
		Audio: AudioRule{CopyCodecs: AllCodecs},
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, plan)
	}
	res, err := ffprobe.Probe(ctx, out)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Streams) != 4 || res.VideoStream().CodecName != "h264" || len(res.AudioStreams()) != 2 || res.AudioStreams()[1].CodecName != "aac" || res.AudioStreams()[1].Language() != "deu" || res.SubtitleStreams()[0].CodecName != "mov_text" {
		t.Errorf("output streams = %+v", res.Streams)
	}
	if res.Format.Tags.Value("title") != "Multi" {
		t.Errorf("metadata not carried: %v", res.Format.Tags)
	}
}
