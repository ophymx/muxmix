package tasks

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/encode"
)

// ThumbnailOptions configures a single-frame thumbnail.
type ThumbnailOptions struct {
	// At is the position to grab. Zero means 10% into the file, which
	// skips leaders and fades on most content.
	At time.Duration
	// Smart lets ffmpeg's thumbnail filter pick the most representative
	// frame from the seconds after At instead of the exact frame.
	Smart bool
	// Width and Height bound the output; aspect ratio is kept. Zero means
	// the source size.
	Width, Height int
	// Image sets the format and quality; the format defaults from the
	// output extension and quality to the encoder default.
	Image encode.Image
}

// Thumbnail writes one frame of input to output (JPEG, PNG or WebP by
// extension). It returns the timestamp that was used.
func Thumbnail(ctx context.Context, input, output string, o ThumbnailOptions) (time.Duration, error) {
	return Default.Thumbnail(ctx, input, output, o)
}

// Thumbnail writes one frame of input to output.
func (t *Tools) Thumbnail(ctx context.Context, input, output string, o ThumbnailOptions) (time.Duration, error) {
	info, err := t.Inspect(ctx, input)
	if err != nil {
		return 0, err
	}
	if !info.HasVideo {
		return 0, fmt.Errorf("tasks: %s has no video stream", input)
	}
	at := o.At
	if at == 0 && info.Duration > 0 {
		at = info.Duration / 10
	}
	at = clampTime(at, info.Duration)

	img := o.Image
	if img.Format == "" {
		img.Format = encode.ImageFormatFor(output)
		if img.Format == "" {
			return 0, fmt.Errorf("tasks: cannot tell image format from %q", output)
		}
	}
	var filters []string
	if o.Smart {
		filters = append(filters, "thumbnail")
	}
	filters = append(filters, scaleFilter(o.Width, o.Height))

	cmd := ffmpeg.NewCommand().
		Input(input, ffmpeg.Seek(at)).
		Output(output, append(outputImageOpts(filters, 1), img.Opts()...)...)
	return at, t.run(ctx, cmd)
}

// outputImageOpts builds the output options shared by image tasks.
func outputImageOpts(filters []string, frames int) []ffmpeg.Opt {
	out := []ffmpeg.Opt{ffmpeg.Map("0:v:0"), ffmpeg.NoAudio(), ffmpeg.NoSubtitles(), ffmpeg.NoData()}
	if f := joinFilters(filters...); f != "" {
		out = append(out, ffmpeg.VideoFilter(f))
	}
	if frames > 0 {
		out = append(out, ffmpeg.Frames("v", frames))
	}
	return out
}

// ThumbnailsOptions configures a series of thumbnails.
type ThumbnailsOptions struct {
	// Count spreads this many thumbnails evenly over the duration, each
	// taken from the middle of its slot. Interval takes one every Interval
	// starting at Start. Set one or the other; Count wins.
	Count    int
	Interval time.Duration
	Start    time.Duration
	Width    int
	Height   int
	Image    encode.Image
}

// ThumbnailFile is one written thumbnail.
type ThumbnailFile struct {
	Path string
	Time time.Duration
}

// Thumbnails writes a series of frames using a printf-style pattern for the
// output, such as "thumbs/%03d.jpg". It returns the files in order.
func Thumbnails(ctx context.Context, input, pattern string, o ThumbnailsOptions) ([]ThumbnailFile, error) {
	return Default.Thumbnails(ctx, input, pattern, o)
}

// Thumbnails writes a series of frames.
func (t *Tools) Thumbnails(ctx context.Context, input, pattern string, o ThumbnailsOptions) ([]ThumbnailFile, error) {
	info, err := t.Inspect(ctx, input)
	if err != nil {
		return nil, err
	}
	if !info.HasVideo {
		return nil, fmt.Errorf("tasks: %s has no video stream", input)
	}
	if !strings.Contains(pattern, "%") {
		return nil, fmt.Errorf("tasks: output pattern %q needs a %%d sequence number", pattern)
	}
	img := o.Image
	if img.Format == "" {
		img.Format = encode.ImageFormatFor(pattern)
	}

	interval, start, count := o.Interval, o.Start, 0
	switch {
	case o.Count > 0:
		if info.Duration <= 0 {
			return nil, fmt.Errorf("tasks: %s has no known duration; use Interval", input)
		}
		count = o.Count
		interval = info.Duration / time.Duration(count)
		start = interval / 2
	case o.Interval > 0:
		if info.Duration > 0 {
			count = int((info.Duration-start)/interval) + 1
		}
	default:
		return nil, fmt.Errorf("tasks: ThumbnailsOptions needs Count or Interval")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("tasks: interval too small for the duration")
	}

	filters := []string{
		fmt.Sprintf("fps=%s", strconv.FormatFloat(1/interval.Seconds(), 'f', -1, 64)),
		scaleFilter(o.Width, o.Height),
	}
	cmd := ffmpeg.NewCommand().
		Input(input, ffmpeg.Seek(start)).
		Output(pattern, append(append(outputImageOpts(filters, count), ffmpeg.Set("start_number", "1")), img.Opts()...)...)
	if err := t.run(ctx, cmd); err != nil {
		return nil, err
	}
	files := make([]ThumbnailFile, 0, count)
	for i := 0; i < count; i++ {
		files = append(files, ThumbnailFile{Path: fmt.Sprintf(pattern, i+1), Time: start + time.Duration(i)*interval})
	}
	return files, nil
}
