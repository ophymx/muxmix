package tasks

import (
	"context"
	"fmt"

	"github.com/ophymx/muxmix/ffmpeg"
)

// WaveformOptions configures a waveform image.
type WaveformOptions struct {
	Width, Height int    // default 1000 x 200
	Color         string // ffmpeg colour, e.g. "white", "#3b82f6" (default "white")
	// Scale is the amplitude scale: "lin" (default), "log", "sqrt" or "cbrt".
	Scale string
	// SplitChannels draws each channel in its own lane instead of mixing
	// down to one.
	SplitChannels bool
	// Stream selects the audio stream (default 0).
	Stream int
}

// Waveform renders the audio of input to a PNG (or any image format ffmpeg
// can write, by extension).
func Waveform(ctx context.Context, input, output string, o WaveformOptions) error {
	return Default.Waveform(ctx, input, output, o)
}

// Waveform renders the audio of input to an image.
func (t *Tools) Waveform(ctx context.Context, input, output string, o WaveformOptions) error {
	info, err := t.Inspect(ctx, input)
	if err != nil {
		return err
	}
	if !info.HasAudio {
		return fmt.Errorf("tasks: %s has no audio stream", input)
	}
	if o.Width <= 0 {
		o.Width = 1000
	}
	if o.Height <= 0 {
		o.Height = 200
	}
	if o.Color == "" {
		o.Color = "white"
	}
	if o.Scale == "" {
		o.Scale = "lin"
	}
	split := 0
	pre := "aformat=channel_layouts=mono,"
	if o.SplitChannels {
		split = 1
		pre = ""
	}
	graph := fmt.Sprintf("[0:a:%d]%sshowwavespic=s=%dx%d:colors=%s:draw=full:scale=%s:split_channels=%d",
		o.Stream, pre, o.Width, o.Height, o.Color, o.Scale, split)
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.FilterComplexString(graph)).
		Input(input).
		Output(output, ffmpeg.Frames("v", 1))
	if err := ensureDir(output); err != nil {
		return err
	}
	return t.run(ctx, cmd)
}
