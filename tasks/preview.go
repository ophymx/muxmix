package tasks

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/encode"
)

// PreviewOptions configures a short low-resolution preview clip. The output
// format follows the extension: .mp4/.webm/.mkv for video, .webp for
// animated WebP, .gif for GIF.
type PreviewOptions struct {
	// Start of the excerpt; zero means 30% into the file.
	Start time.Duration
	// Duration of the excerpt (default 3s).
	Duration time.Duration
	// Width bounds the output (default 320); height follows.
	Width int
	// FPS for animated images (default 10) and video (default source).
	FPS float64
	// Audio keeps the audio track in video previews (off by default).
	Audio bool
	// Video settings for video outputs; zero means H.264, quality 28,
	// fastest preset, yuv420p.
	Video encode.Video
	// Image settings for WebP output (quality) and GIF.
	Image encode.Image
	// Loop count for animated images: 0 loops forever.
	Loop int
}

// Preview writes a short excerpt of input to output.
func Preview(ctx context.Context, input, output string, o PreviewOptions) error {
	return Default.Preview(ctx, input, output, o)
}

// Preview writes a short excerpt of input to output.
func (t *Tools) Preview(ctx context.Context, input, output string, o PreviewOptions) error {
	info, err := t.Inspect(ctx, input)
	if err != nil {
		return err
	}
	if !info.HasVideo {
		return fmt.Errorf("tasks: %s has no video stream", input)
	}
	if o.Duration <= 0 {
		o.Duration = 3 * time.Second
	}
	start := o.Start
	if start == 0 && info.Duration > 0 {
		start = info.Duration * 3 / 10
	}
	start = clampTime(start, info.Duration)
	if info.Duration > 0 && start+o.Duration > info.Duration {
		o.Duration = info.Duration - start
	}
	if o.Width <= 0 {
		o.Width = 320
	}
	scale := scaleFilter(o.Width, 0)

	in := ffmpeg.NewInput(input, ffmpeg.Seek(start), ffmpeg.Duration(o.Duration))
	var out *ffmpeg.Output
	switch ext(output) {
	case ".gif":
		fps := o.FPS
		if fps <= 0 {
			fps = 10
		}
		// Two-pass palette in one graph: split, build a palette from the
		// whole excerpt, then dither with it.
		graph := fmt.Sprintf("[0:v]fps=%s,%s,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle",
			fpsStr(fps), scale)
		out = ffmpeg.NewOutput(output, ffmpeg.NoAudio(), ffmpeg.Set("loop", strconv.Itoa(o.Loop)))
		cmd := &ffmpeg.Command{Inputs: []*ffmpeg.Input{in}, Outputs: []*ffmpeg.Output{out}}
		cmd.GlobalOptions(ffmpeg.FilterComplexString(graph))
		return t.run(ctx, cmd)
	case ".webp":
		fps := o.FPS
		if fps <= 0 {
			fps = 10
		}
		img := o.Image
		img.Format = encode.AnimatedWebP
		if img.Quality == 0 {
			img.Quality = 75
		}
		out = ffmpeg.NewOutput(output, append(
			outputImageOpts([]string{"fps=" + fpsStr(fps), scale}, 0),
			append(img.Opts(), ffmpeg.Set("loop", strconv.Itoa(o.Loop)))...)...)
	default:
		v := o.Video
		if v.Codec == "" && v.Encoder == "" {
			v.Codec = encode.H264
			if v.Quality == 0 {
				v.Quality = 28
			}
			if v.Speed == encode.DefaultSpeed {
				v.Speed = encode.Fastest
			}
			if v.PixFmt == "" {
				v.PixFmt = "yuv420p"
			}
		}
		vopts, err := v.Opts(nil)
		if err != nil {
			return err
		}
		filters := []string{scale}
		if o.FPS > 0 {
			filters = append([]string{"fps=" + fpsStr(o.FPS)}, filters...)
		}
		opts := []ffmpeg.Opt{ffmpeg.Map("0:v:0"), ffmpeg.NoSubtitles(), ffmpeg.NoData()}
		if o.Audio && info.HasAudio {
			opts = append(opts, ffmpeg.Map("0:a:0"), ffmpeg.AudioCodec("aac"), ffmpeg.BitRate("a", "96k"))
		} else {
			opts = append(opts, ffmpeg.NoAudio())
		}
		if f := joinFilters(filters...); f != "" {
			opts = append(opts, ffmpeg.VideoFilter(f))
		}
		opts = append(opts, vopts...)
		if ext(output) == ".mp4" {
			opts = append(opts, ffmpeg.MovFlags("+faststart"))
		}
		out = ffmpeg.NewOutput(output, opts...)
	}
	cmd := &ffmpeg.Command{Inputs: []*ffmpeg.Input{in}, Outputs: []*ffmpeg.Output{out}}
	return t.run(ctx, cmd)
}

func fpsStr(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
