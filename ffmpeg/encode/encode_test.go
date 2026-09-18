package encode_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
	"github.com/ophymx/muxmix/ffprobe"
)

func render(o []ffmpeg.Opt) string {
	var opts ffmpeg.Options
	opts.Add(o...)
	return strings.Join(opts.Args(), " ")
}

func TestVideoOptsFor(t *testing.T) {
	v := encode.Video{Quality: 22, Speed: encode.Fast, Profile: "high", PixFmt: "yuv420p", KeyframeInterval: 48}
	cases := map[string]string{
		"libx264":           "-c:v libx264 -crf 22 -preset fast -profile:v high -pix_fmt yuv420p -g 48",
		"libx265":           "-c:v libx265 -crf 22 -preset fast -profile:v high -pix_fmt yuv420p -g 48",
		"libsvtav1":         "-c:v libsvtav1 -crf 27 -preset 9 -profile:v high -pix_fmt yuv420p -g 48",
		"libvpx-vp9":        "-c:v libvpx-vp9 -crf 27 -b:v 0 -deadline good -cpu-used 4 -row-mt 1 -profile:v high -pix_fmt yuv420p -g 48",
		"libaom-av1":        "-c:v libaom-av1 -crf 27 -cpu-used 6 -row-mt 1 -profile:v high -pix_fmt yuv420p -g 48",
		"h264_nvenc":        "-c:v h264_nvenc -rc vbr -cq 22 -b:v 0 -preset p2 -profile:v high -pix_fmt yuv420p -g 48",
		"hevc_vaapi":        "-c:v hevc_vaapi -rc_mode CQP -qp 22 -profile:v high -pix_fmt yuv420p -g 48",
		"h264_qsv":          "-c:v h264_qsv -global_quality 22 -preset fast -profile:v high -pix_fmt yuv420p -g 48",
		"h264_videotoolbox": "-c:v h264_videotoolbox -q:v 56 -profile:v high -pix_fmt yuv420p -g 48",
	}
	for enc, want := range cases {
		if got := render(v.OptsFor(enc)); got != want {
			t.Errorf("%s:\n got %s\nwant %s", enc, got, want)
		}
	}

	b := encode.Video{Bitrate: "5M", MaxRate: "6M", Speed: encode.Slowest, Tune: "film"}
	if got := render(b.OptsFor("libx264")); got != "-c:v libx264 -b:v 5M -maxrate 6M -bufsize 12M -preset veryslow -tune film" {
		t.Errorf("bitrate x264 = %s", got)
	}
	if got := render(b.OptsFor("h264_nvenc")); got != "-c:v h264_nvenc -b:v 5M -rc vbr -maxrate 6M -bufsize 12M -preset p7 -tune film" {
		t.Errorf("bitrate nvenc = %s", got)
	}
	if got := render(encode.Video{Codec: encode.Copy}.OptsFor("copy")); got != "-c:v copy" {
		t.Errorf("copy = %s", got)
	}
	extra := encode.Video{Quality: 20, Extra: ffmpeg.Opts(ffmpeg.X264Params("aq-mode=3"))}
	if got := render(extra.OptsFor("libx264")); !strings.HasSuffix(got, "-x264-params aq-mode=3") {
		t.Errorf("extra = %s", got)
	}
}

func TestAudioOptsFor(t *testing.T) {
	cases := []struct {
		a    encode.Audio
		enc  string
		want string
	}{
		{encode.Audio{Bitrate: "160k", Channels: 2, SampleRate: 48000}, "aac", "-c:a aac -b:a 160k -ac 2 -ar 48000"},
		{encode.Audio{VBR: 10}, "libmp3lame", "-c:a libmp3lame -q:a 0"},
		{encode.Audio{VBR: 1}, "libmp3lame", "-c:a libmp3lame -q:a 9"},
		{encode.Audio{VBR: 10}, "aac", "-c:a aac -q:a 2"},
		{encode.Audio{VBR: 5}, "libfdk_aac", "-c:a libfdk_aac -vbr 3"},
		{encode.Audio{VBR: 6}, "libvorbis", "-c:a libvorbis -q:a 6"},
		{encode.Audio{VBR: 5}, "libopus", "-c:a libopus -b:a 96k"},
		{encode.Audio{}, "copy", "-c:a copy"},
	}
	for _, c := range cases {
		if got := render(c.a.OptsFor(c.enc)); got != c.want {
			t.Errorf("%s %+v:\n got %s\nwant %s", c.enc, c.a, got, c.want)
		}
	}
}

func TestImageOpts(t *testing.T) {
	if got := render(encode.Image{Format: encode.JPEG, Quality: 90}.Opts()); got != "-c:v mjpeg -q:v 5 -pix_fmt yuvj420p" {
		t.Errorf("jpeg = %s", got)
	}
	if got := render(encode.Image{Format: encode.WebPImage, Quality: 80}.Opts()); got != "-c:v libwebp -quality 80" {
		t.Errorf("webp = %s", got)
	}
	if got := render(encode.Image{Format: encode.AnimatedWebP, Lossless: true}.Opts()); got != "-c:v libwebp_anim -lossless 1" {
		t.Errorf("webp anim = %s", got)
	}
	if encode.ImageFormatFor("x/y.JPG") != encode.JPEG || encode.ImageFormatFor("a.gif") != encode.AnimatedGIF || encode.ImageFormatFor("a.bmp") != "" {
		t.Error("ImageFormatFor")
	}
}

func TestChooseEncoder(t *testing.T) {
	dir := filepath.Join("..", "testdata", "capture", "7.1.5")
	b, err := os.ReadFile(filepath.Join(dir, "encoders.txt"))
	if err != nil {
		t.Skip("no capture")
	}
	set := &caps.Set{Encoders: caps.ParseCodecs(string(b))}

	if enc, err := encode.ChooseEncoder(set, encode.H264, hwaccel.None); err != nil || enc != "libx264" {
		t.Errorf("h264 = %s %v", enc, err)
	}
	if enc, err := encode.ChooseEncoder(set, encode.AAC, hwaccel.None); err != nil || enc != "aac" {
		t.Errorf("aac = %s %v", enc, err)
	}
	if enc, err := encode.ChooseEncoder(set, encode.H264, hwaccel.CUDA); err != nil || enc != "h264_nvenc" {
		t.Errorf("nvenc = %s %v", enc, err)
	}
	if _, err := encode.ChooseEncoder(set, encode.ProRes, hwaccel.CUDA); err == nil {
		t.Error("prores on cuda should fail")
	}
	if _, err := encode.ChooseEncoder(set, "nope", hwaccel.None); err == nil {
		t.Error("unknown codec should fail")
	}
	if enc, err := encode.ChooseEncoder(nil, encode.AV1, hwaccel.None); err != nil || enc != "libsvtav1" {
		t.Errorf("nil set = %s %v", enc, err)
	}
	v := encode.Video{Encoder: "libnope"}
	if _, err := v.Opts(set); err == nil {
		t.Error("missing explicit encoder should fail")
	}
	if encode.CodecOf("hevc_nvenc") != encode.HEVC || encode.CodecOf("libx264") != encode.H264 || encode.CodecOf("weird") != "" {
		t.Error("CodecOf")
	}
}

func TestLiveEncode(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	set, err := caps.Detect(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	video := encode.Video{Codec: encode.H264, Quality: 30, Speed: encode.Fastest, PixFmt: "yuv420p"}
	audio := encode.Audio{Codec: encode.AAC, Bitrate: "64k"}
	vo, err := video.Opts(set)
	if err != nil {
		t.Skip(err)
	}
	ao, err := audio.Opts(set)
	if err != nil {
		t.Skip(err)
	}
	out := filepath.Join(t.TempDir(), "out.mp4")
	cmd := ffmpeg.NewCommand().
		Input("testsrc2=size=64x64:rate=25:duration=1", ffmpeg.Lavfi()).
		Input("sine=duration=1", ffmpeg.Lavfi()).
		Output(out, append(append([]ffmpeg.Opt{ffmpeg.Map("0:v"), ffmpeg.Map("1:a")}, vo...), ao...)...)
	if err := caps.Check(ctx, set, ffmpeg.DefaultRunner, cmd); err != nil {
		t.Fatal(err)
	}
	if _, err := ffmpeg.Run(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	info, err := ffprobe.Probe(ctx, out)
	if err != nil || info.VideoStream().CodecName != "h264" || info.AudioStream().CodecName != "aac" {
		t.Errorf("probe = %+v %v", info, err)
	}
}
