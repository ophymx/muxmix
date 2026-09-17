package caps_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
)

func captureSet(t *testing.T) (*caps.Set, *caps.Checker) {
	t.Helper()
	dir := filepath.Join("..", "testdata", "capture", "7.1.5")
	set := &caps.Set{
		Encoders:     caps.ParseCodecs(read(t, dir, "encoders.txt")),
		Decoders:     caps.ParseCodecs(read(t, dir, "decoders.txt")),
		Muxers:       caps.ParseFormats(read(t, dir, "muxers.txt")),
		Demuxers:     caps.ParseFormats(read(t, dir, "demuxers.txt")),
		Filters:      caps.ParseFilters(read(t, dir, "filters.txt")),
		PixelFormats: caps.ParsePixelFormats(read(t, dir, "pix_fmts.txt")),
		HWAccels:     caps.ParseList(read(t, dir, "hwaccels.txt")),
	}
	ch := caps.NewChecker(set, nil)
	ch.AddHelp(caps.ParseHelp(read(t, dir, "help-encoder-libx264.txt")))
	return set, ch
}

func TestCheckValid(t *testing.T) {
	_, ch := captureSet(t)
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.FilterComplexString("[0:v]scale=w=1280:h=-2,format=yuv420p[v];[0:a]volume=0.5[a]")).
		Input("in.mkv", ffmpeg.Format("matroska")).
		Output("out.mp4", ffmpeg.MapLabel("v"), ffmpeg.MapLabel("a"), ffmpeg.Format("mp4"),
			ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(23), ffmpeg.Preset("slow"), ffmpeg.PixFmt("yuv420p"),
			ffmpeg.Set("aq-mode", "variance"), ffmpeg.Set("b-pyramid", "normal"),
			ffmpeg.AudioCodec("aac"), ffmpeg.BitRate("a", "160k"))
	if err := ch.Check(context.Background(), cmd); err != nil {
		t.Fatalf("valid command rejected: %v", err)
	}
}

func TestCheckProblems(t *testing.T) {
	_, ch := captureSet(t)
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.InitHWDevice("nonsense=dev")).
		Input("in.mkv", ffmpeg.Format("nosuchdemuxer"), ffmpeg.HWAccel("magic", "")).
		Output("out.mp4",
			ffmpeg.VideoCodec("h264"), // a codec id, not an encoder name
			ffmpeg.Format("mp5"),
			ffmpeg.VideoFilter("scale=1280:-2,nonexistentfilter=1"),
			ffmpeg.PixFmt("yuv420z")).
		Output("out2.mp4",
			ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(-5), ffmpeg.PixFmt("rgb24"),
			ffmpeg.Set("aq-mode", "sometimes"), ffmpeg.Set("b-pyramid", "12"))
	err := ch.Check(context.Background(), cmd)
	var ce *caps.CheckError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CheckError, got %v", err)
	}
	got := map[string]caps.Problem{}
	for _, p := range ce.Problems {
		got[p.Where+"/"+p.Option+"/"+p.Value] = p
	}
	want := []string{
		"global/init_hw_device/nonsense=dev",
		"input 0/f/nosuchdemuxer",
		"input 0/hwaccel/magic",
		"output 0/c:v/h264",
		"output 0/f/mp5",
		"output 0/vf/nonexistentfilter",
		"output 0/pix_fmt/yuv420z",
		"output 1/crf/-5",
		"output 1/pix_fmt/rgb24",
		"output 1/aq-mode/sometimes",
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("missing problem %s in:\n%v", w, err)
		}
	}
	if len(ce.Problems) != len(want) {
		t.Errorf("got %d problems, want %d:\n%v", len(ce.Problems), len(want), err)
	}
	if p := got["output 0/c:v/h264"]; !contains(p.Suggestions, "libx264") {
		t.Errorf("h264 suggestions = %v", p.Suggestions)
	}
	if p := got["output 1/aq-mode/sometimes"]; !contains(p.Suggestions, "variance") {
		t.Errorf("aq-mode suggestions = %v", p.Suggestions)
	}
	if p := got["output 1/pix_fmt/rgb24"]; !strings.Contains(p.Message, "libx264") {
		t.Errorf("pix_fmt message = %q", p.Message)
	}
	if p := got["output 1/crf/-5"]; !strings.Contains(p.Message, "minimum") {
		t.Errorf("crf message = %q", p.Message)
	}
	if !strings.Contains(err.Error(), "10 problems") {
		t.Errorf("Error() = %s", err)
	}
}

func TestFilterNames(t *testing.T) {
	cases := map[string][]string{
		"scale=1280:-2,format=yuv420p": {"scale", "format"},
		"sws_flags=lanczos;[0:v]scale@hd=w=1280:h=720[v];[v][1:v]overlay=x=10:y=10[out]": {"scale", "overlay"},
		"drawtext=text='a,b;c':x=10,fps=30":                                              {"drawtext", "fps"},
		"[0:a][1:a]amix=inputs=2[a]":                                                     {"amix"},
		"":                                                                               nil,
	}
	for in, want := range cases {
		if got := caps.FilterNames(in); !reflect.DeepEqual(got, want) {
			t.Errorf("FilterNames(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCheckLive(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	set, err := caps.Detect(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !set.HasEncoder("libx264") {
		t.Skip("libx264 not in this build")
	}
	cmd := ffmpeg.NewCommand().Input("in.mp4").Output("out.mp4", ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(-5))
	// Without a runner the option table is unknown, so the value passes.
	if err := caps.Check(ctx, set, nil, cmd); err != nil {
		t.Errorf("check without runner = %v", err)
	}
	// With a runner the option table is fetched and the range enforced.
	err = caps.Check(ctx, set, ffmpeg.DefaultRunner, cmd)
	if err == nil || !strings.Contains(err.Error(), "crf") {
		t.Errorf("live check = %v", err)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
