package ffmpeg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TwoPassOptions configures TwoPass.
type TwoPassOptions struct {
	// LogPrefix is the -passlogfile prefix; empty uses a temporary
	// directory that is removed afterwards.
	LogPrefix string
	// KeepLogs leaves the first-pass statistics on disk.
	KeepLogs bool
	// Duration of the input, used to compute TwoPassProgress.Fraction.
	Duration time.Duration
	// OnProgress receives updates from both passes.
	OnProgress func(TwoPassProgress)
	// RunOptions are applied to both runs.
	RunOptions []RunOption
}

// TwoPassProgress is a progress update tagged with the pass it came from.
type TwoPassProgress struct {
	Pass int // 1 or 2
	Progress
	// Fraction of the whole job done, 0-1, when Duration was given.
	Fraction float64
}

// TwoPassResult holds the results of both passes.
type TwoPassResult struct {
	First  *Result
	Second *Result
}

// TwoPass runs cmd as a two-pass encode: first to the null muxer with
// -pass 1, then as given with -pass 2, sharing the statistics file. Every
// output that encodes video with an encoder supporting two-pass rate
// control (libx264, libx265, libvpx, libaom, mpeg4 and the other native
// encoders) gets the pass options; other outputs are left as they are.
//
// Two-pass is worth it for bitrate-targeted encodes (a fixed file size or
// a broadcast bit rate); constant-quality encodes gain nothing from it.
func TwoPass(ctx context.Context, r Runner, cmd *Command, o TwoPassOptions) (*TwoPassResult, error) {
	if r == nil {
		r = DefaultRunner
	}
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	prefix := o.LogPrefix
	if prefix == "" {
		dir, err := os.MkdirTemp("", "ffmpeg-2pass-")
		if err != nil {
			return nil, err
		}
		prefix = filepath.Join(dir, "stats")
		if !o.KeepLogs {
			defer os.RemoveAll(dir)
		}
	} else if !o.KeepLogs {
		defer removeGlob(prefix + "*")
	}

	first := PassCommand(cmd, 1, prefix)
	second := PassCommand(cmd, 2, prefix)
	if len(first.Outputs) == 0 {
		return nil, fmt.Errorf("ffmpeg: two-pass: no output encodes video")
	}

	res := &TwoPassResult{}
	run := func(pass int, c *Command) (*Result, error) {
		opts := append([]RunOption(nil), o.RunOptions...)
		if o.OnProgress != nil {
			opts = append(opts, OnProgress(func(p Progress) {
				tp := TwoPassProgress{Pass: pass, Progress: p}
				if o.Duration > 0 {
					f := float64(p.Time) / float64(o.Duration)
					if f > 1 {
						f = 1
					}
					tp.Fraction = (float64(pass-1) + f) / 2
				}
				o.OnProgress(tp)
			}))
		}
		return r.Run(ctx, c, opts...)
	}
	var err error
	if res.First, err = run(1, first); err != nil {
		return res, fmt.Errorf("first pass: %w", err)
	}
	if res.Second, err = run(2, second); err != nil {
		return res, fmt.Errorf("second pass: %w", err)
	}
	return res, nil
}

// PassCommand derives the command for one pass. Pass 1 keeps only the
// outputs that encode video, sends them to the null muxer and drops audio,
// subtitles and data; pass 2 is the original command. Both add the pass
// options to every video encoder that supports them.
func PassCommand(cmd *Command, pass int, logPrefix string) *Command {
	out := &Command{Global: append(Options(nil), cmd.Global...)}
	for _, in := range cmd.Inputs {
		out.Inputs = append(out.Inputs, &Input{URL: in.URL, Options: append(Options(nil), in.Options...)})
	}
	for i, o := range cmd.Outputs {
		enc := videoEncoder(o.Options)
		if !supportsTwoPass(enc) {
			if pass == 2 {
				out.Outputs = append(out.Outputs, &Output{URL: o.URL, Options: append(Options(nil), o.Options...)})
			}
			continue
		}
		prefix := logPrefix
		if len(cmd.Outputs) > 1 {
			prefix = fmt.Sprintf("%s-out%d", logPrefix, i)
		}
		var opts Options
		for _, a := range o.Options {
			if pass == 1 && (a.Name == "f" || a.Name == "movflags") {
				continue // null muxer instead
			}
			if a.Name == "pass" || a.Name == "passlogfile" {
				continue
			}
			opts = append(opts, a)
		}
		switch {
		case strings.HasPrefix(enc, "libx265"):
			opts = mergeParams(opts, "x265-params", fmt.Sprintf("pass=%d:stats=%s.x265.log", pass, prefix))
		default:
			opts.Add(Pass(pass), PassLogFile(prefix))
		}
		url := o.URL
		if pass == 1 {
			opts.Add(NoAudio(), NoSubtitles(), NoData(), Format("null"))
			url = NullTarget
		}
		out.Outputs = append(out.Outputs, &Output{URL: url, Options: opts})
	}
	return out
}

// videoEncoder returns the encoder named by the first -c:v style option.
func videoEncoder(opts Options) string {
	for _, a := range opts {
		switch {
		case a.Name == "c:v" || strings.HasPrefix(a.Name, "c:v:") || a.Name == "vcodec" || a.Name == "codec:v":
			return a.Value
		}
	}
	return ""
}

// supportsTwoPass reports whether an encoder honours -pass.
func supportsTwoPass(enc string) bool {
	switch {
	case enc == "" || enc == "copy":
		return false
	case strings.HasPrefix(enc, "libx264"), enc == "libx265", enc == "libvpx", enc == "libvpx-vp9", enc == "libaom-av1",
		enc == "mpeg4", enc == "mpeg2video", enc == "mpeg1video", enc == "libxvid", enc == "msmpeg4", enc == "h263", enc == "wmv2":
		return true
	}
	return false
}

// mergeParams appends key=value pairs to an existing dictionary option
// (x265-params style) or adds it.
func mergeParams(opts Options, name, params string) Options {
	for i := range opts {
		if opts[i].Name == name {
			opts[i].Value = strings.TrimSuffix(opts[i].Value, ":") + ":" + params
			return opts
		}
	}
	return append(opts, Arg{Name: name, Value: params, HasValue: true})
}

func removeGlob(pattern string) {
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		_ = os.Remove(m)
	}
}
