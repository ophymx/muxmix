// Package tasks does the common media jobs that are surprisingly fiddly to
// get right with raw ffmpeg: thumbnails, trickplay sprite sheets with their
// WebVTT index, short previews as MP4, animated WebP or GIF, and waveform
// images.
//
//	err := tasks.Thumbnail(ctx, "movie.mkv", "poster.jpg", tasks.ThumbnailOptions{Width: 640})
//	tp, err := tasks.Trickplay(ctx, "movie.mkv", "out/trick", tasks.TrickplayOptions{})
//
// Every task probes the input first, so it knows the duration, the display
// size after rotation and which streams exist, and then runs one ffmpeg
// command built with the ffmpeg package.
package tasks

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
	"github.com/ophymx/muxmix/ffprobe"
)

// Tools holds the runner, prober and machine capabilities a task uses.
// The zero value uses the package defaults and software encoding.
type Tools struct {
	Runner ffmpeg.Runner
	Prober *ffprobe.Prober
	// System is what the ffmpeg binary can do on this machine, from
	// hwaccel.Detect or hwaccel.DetectCached. Tasks choose encoders from
	// its build capabilities, validate each command against them before
	// running, and resolve every job's hardware Policy against its probe
	// results. Nil means software encoding and no validation.
	System *hwaccel.System
	// RunOptions are passed to every ffmpeg run, for progress or logging.
	RunOptions []ffmpeg.RunOption
}

// Default is the Tools used by the package-level functions.
var Default = &Tools{}

func (t *Tools) runner() ffmpeg.Runner {
	if t != nil && t.Runner != nil {
		return t.Runner
	}
	return ffmpeg.DefaultRunner
}

func (t *Tools) prober() *ffprobe.Prober {
	if t != nil && t.Prober != nil {
		return t.Prober
	}
	return ffprobe.Default
}

// caps returns the build capabilities from System, or nil.
func (t *Tools) caps() *caps.Set {
	if t == nil || t.System == nil {
		return nil
	}
	return t.System.Caps
}

func (t *Tools) system() *hwaccel.System {
	if t == nil {
		return nil
	}
	return t.System
}

func capsOf(sys *hwaccel.System) *caps.Set {
	if sys == nil {
		return nil
	}
	return sys.Caps
}

// resolveHW applies a job's hardware policy to its video settings. An
// explicit Encoder is left alone. An explicit encode.Video.HW under a
// software policy counts as requiring that backend; without a System it
// is trusted as-is with the backend's default device. On a hardware
// selection the Encoder and HW are filled in and PixFmt is cleared, since
// the upload chain fixes the frame format.
func resolveHW(sys *hwaccel.System, policy hwaccel.Policy, v *encode.Video) (hwaccel.Selection, string, error) {
	if v.Encoder != "" {
		return hwaccel.Selection{}, "", nil
	}
	if policy.Mode == hwaccel.Software && v.HW != hwaccel.None {
		if sys == nil {
			enc, err := hwaccel.VideoEncoder(v.HW, string(v.Codec))
			if err != nil {
				return hwaccel.Selection{}, "", fmt.Errorf("tasks: %w", err)
			}
			sel := hwaccel.Selection{Kind: v.HW, Encoder: enc, Codec: string(v.Codec)}
			v.Encoder, v.PixFmt = enc, ""
			return sel, "", nil
		}
		policy = hwaccel.RequireHardware(v.HW)
	}
	sel, reason, err := policy.Resolve(sys, string(v.Codec))
	if err != nil {
		return sel, reason, fmt.Errorf("tasks: %w", err)
	}
	if sel.Hardware() {
		v.Encoder, v.HW, v.PixFmt = sel.Encoder, sel.Kind, ""
	}
	return sel, reason, nil
}

// Run executes a command through the Tools' runner with its RunOptions
// followed by opts, so a job can add its own progress callback. Plans
// returned by PlanTranscode and PlanPackage have a Run method that also
// supplies ffmpeg.TotalDuration.
func (t *Tools) Run(ctx context.Context, cmd *ffmpeg.Command, opts ...ffmpeg.RunOption) (*ffmpeg.Result, error) {
	var all []ffmpeg.RunOption
	if t != nil {
		all = append(all, t.RunOptions...)
	}
	all = append(all, opts...)
	return t.runner().Run(ctx, cmd, all...)
}

func (t *Tools) run(ctx context.Context, cmd *ffmpeg.Command, opts ...ffmpeg.RunOption) error {
	_, err := t.Run(ctx, cmd, opts...)
	return err
}

// ensureDir creates the directory an output file will be written to.
func ensureDir(output string) error {
	dir := filepath.Dir(output)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("tasks: create output directory: %w", err)
	}
	return nil
}

// Inspect probes input with the default Tools.
func Inspect(ctx context.Context, input string, inputOpts ...ffprobe.Option) (*Info, error) {
	return Default.Inspect(ctx, input, inputOpts...)
}

// Info is what the tasks learn about an input before working on it.
type Info struct {
	Probe    *ffprobe.Result
	Duration time.Duration
	// Width and Height are the display size: the coded size with any
	// rotation metadata applied, which is what ffmpeg produces since it
	// auto-rotates on decode.
	Width, Height int
	HasVideo      bool
	HasAudio      bool
}

// Inspect probes the input and derives the display geometry.
func (t *Tools) Inspect(ctx context.Context, input string, inputOpts ...ffprobe.Option) (*Info, error) {
	res, err := t.prober().Probe(ctx, input, inputOpts...)
	if err != nil {
		return nil, err
	}
	info := &Info{Probe: res, Duration: res.Duration(), HasAudio: res.HasAudio()}
	if v := res.VideoStream(); v != nil {
		info.HasVideo = true
		info.Width, info.Height = v.Resolution()
		if rot := v.Rotation(); rot == 90 || rot == -90 || rot == 270 || rot == -270 {
			info.Width, info.Height = info.Height, info.Width
		}
	}
	return info, nil
}

// fitSize returns the size that fits the source inside maxW x maxH keeping
// the aspect ratio, rounded to even numbers. Zero limits mean unconstrained;
// both zero returns the source size.
func fitSize(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return maxW, maxH
	}
	w, h := float64(srcW), float64(srcH)
	scale := 1.0
	if maxW > 0 && w > float64(maxW) {
		scale = float64(maxW) / w
	}
	if maxH > 0 && h*scale > float64(maxH) {
		scale = float64(maxH) / h
	}
	if maxW > 0 || maxH > 0 {
		w, h = w*scale, h*scale
	}
	return even(int(math.Round(w))), even(int(math.Round(h)))
}

func even(n int) int {
	if n < 2 {
		return 2
	}
	return n - n%2
}

// scaleFilter returns a scale filter fitting the frame inside maxW x maxH
// while keeping aspect ratio and even dimensions. With no limits it returns "".
func scaleFilter(maxW, maxH int) string {
	switch {
	case maxW > 0 && maxH > 0:
		return fmt.Sprintf("scale=w=%d:h=%d:force_original_aspect_ratio=decrease:force_divisible_by=2", maxW, maxH)
	case maxW > 0:
		return fmt.Sprintf("scale=w=%d:h=-2", maxW)
	case maxH > 0:
		return fmt.Sprintf("scale=w=-2:h=%d", maxH)
	}
	return ""
}

func joinFilters(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// clampTime keeps t within [0, max).
func clampTime(t, max time.Duration) time.Duration {
	if t < 0 {
		return 0
	}
	if max > 0 && t >= max {
		if max > 100*time.Millisecond {
			return max - 100*time.Millisecond
		}
		return 0
	}
	return t
}

func ext(path string) string { return strings.ToLower(filepath.Ext(path)) }
