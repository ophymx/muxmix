package ffmpeg_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/filtergraph"
)

func TestCommandArgs(t *testing.T) {
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.LogLevel("warning")).
		Input("in.mkv", ffmpeg.Seek(90*time.Second), ffmpeg.Duration(5*time.Second+500*time.Millisecond)).
		Input("logo.png", ffmpeg.StreamLoop(-1)).
		Output("out.mp4",
			ffmpeg.Map("0:v:0"), ffmpeg.Map("0:a:m:language:eng"), ffmpeg.MapExclude("0:s"),
			ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(20), ffmpeg.Preset("slow"), ffmpeg.PixFmt("yuv420p"),
			ffmpeg.AudioCodec("aac"), ffmpeg.BitRate("a", "160k"),
			ffmpeg.StreamMetadata("s:a:0", "language", "eng"), ffmpeg.Disposition("a:0", "default"),
			ffmpeg.MovFlags("+faststart"), ffmpeg.Shortest()).
		Output(ffmpeg.NullTarget, ffmpeg.NullOutput(), ffmpeg.Map("0:v"), ffmpeg.Frames("v", 10))

	got := strings.Join(cmd.Args(), " ")
	want := "-loglevel warning " +
		"-ss 00:01:30.00 -t 00:00:05.50 -i in.mkv -stream_loop -1 -i logo.png " +
		"-map 0:v:0 -map 0:a:m:language:eng -map -0:s -c:v libx264 -crf 20 -preset slow -pix_fmt yuv420p " +
		"-c:a aac -b:a 160k -metadata:s:a:0 language=eng -disposition:a:0 default -movflags +faststart -shortest out.mp4 " +
		"-f null -map 0:v -frames:v 10 -"
	if got != want {
		t.Errorf("Args()\n got %s\nwant %s", got, want)
	}
	if err := cmd.Validate(); err != nil {
		t.Error(err)
	}
	if err := ffmpeg.NewCommand().Input("x").Validate(); err == nil {
		t.Error("expected validation error without outputs")
	}
}

func TestCommandFilters(t *testing.T) {
	g := filtergraph.NewFilterGraph()
	g.NewChain().Input("0:v").Scale(1280, 720).Output("v")
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.FilterComplex(g)).
		Input("in.mp4").
		Output("out.mp4", ffmpeg.MapLabel("v"), ffmpeg.MapLabel("[0:a]"), ffmpeg.AudioFilter("volume=0.5"))
	got := strings.Join(cmd.Args(), " ")
	want := "-filter_complex [0:v]scale=w=1280:h=720[v] -i in.mp4 -map [v] -map [0:a] -af volume=0.5 out.mp4"
	if got != want {
		t.Errorf("Args()\n got %s\nwant %s", got, want)
	}
}

func TestRawOptions(t *testing.T) {
	var o ffmpeg.Options
	o.Add(ffmpeg.Raw("-hwaccel", "cuda", "-hwaccel_output_format", "cuda", "-an", "-ss", "-5", "-y"))
	got := strings.Join(o.Args(), " ")
	if got != "-hwaccel cuda -hwaccel_output_format cuda -an -ss -5 -y" {
		t.Errorf("Raw = %s", got)
	}
	if v, ok := o.Get("hwaccel"); !ok || v != "cuda" {
		t.Errorf("Get hwaccel = %q %v", v, ok)
	}
	if !o.Has("an") || o.Has("vn") {
		t.Error("Has")
	}
}

func TestCommandString(t *testing.T) {
	cmd := ffmpeg.NewCommand().Input("my movie.mkv").Output("out.mp4", ffmpeg.Metadata("title", "It's here"))
	got := cmd.String()
	want := `ffmpeg -i 'my movie.mkv' -metadata 'title=It'\''s here' out.mp4`
	if got != want {
		t.Errorf("String()\n got %s\nwant %s", got, want)
	}
}

func TestCommandJSONRoundTrip(t *testing.T) {
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.LogLevel("warning"), ffmpeg.FilterComplexString("[0:v]split=2[a][b]")).
		Input("in.mkv", ffmpeg.Seek(90*time.Second)).
		Input("logo.png").
		Output("out.mp4",
			ffmpeg.MapLabel("a"), ffmpeg.Map("0:a:0"), ffmpeg.MapLabel("b"),
			ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(20),
			ffmpeg.Set("empty_value", ""), ffmpeg.Flag("shortest"),
			ffmpeg.MovFlags("+faststart"))

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back ffmpeg.Command
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(back.Args(), cmd.Args()) {
		t.Errorf("Args after round trip\n got %q\nwant %q", back.Args(), cmd.Args())
	}
	if err := back.Validate(); err != nil {
		t.Errorf("Validate after round trip: %v", err)
	}
}

// Option order is significant to ffmpeg -- -map order defines the output
// stream order -- so the encoding must be a list, not a map.
func TestCommandJSONKeepsOptionOrder(t *testing.T) {
	cmd := ffmpeg.NewCommand().Input("in.mkv").
		Output("out.mp4", ffmpeg.Map("0:a:1"), ffmpeg.Map("0:v:0"), ffmpeg.Map("0:a:0"))

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var back ffmpeg.Command
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	got := strings.Join(back.Outputs[0].Options.Args(), " ")
	if want := "-map 0:a:1 -map 0:v:0 -map 0:a:0"; got != want {
		t.Errorf("maps after round trip\n got %s\nwant %s", got, want)
	}
}

func TestOptsBuildsOptions(t *testing.T) {
	extra := ffmpeg.Opts(ffmpeg.X264Params("aq-mode=3"), ffmpeg.Flag("shortest"))
	if got, want := strings.Join(extra.Args(), " "), "-x264-params aq-mode=3 -shortest"; got != want {
		t.Errorf("Opts = %s, want %s", got, want)
	}
	var into ffmpeg.Options
	into.Add(ffmpeg.CRF(20), extra.Opt())
	if got, want := strings.Join(into.Args(), " "), "-crf 20 -x264-params aq-mode=3 -shortest"; got != want {
		t.Errorf("Options.Opt = %s, want %s", got, want)
	}
	var empty ffmpeg.Options
	into = nil
	into.Add(empty.Opt())
	if len(into) != 0 {
		t.Errorf("empty Options.Opt appended %d args", len(into))
	}
}
