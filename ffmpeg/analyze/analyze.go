// Package analyze runs ffmpeg's analysis filters and returns their results
// as Go values instead of log lines.
//
//	silences, err := analyze.Silence(ctx, "talk.wav", analyze.SilenceOptions{})
//	stats, err := analyze.Loudnorm(ctx, "talk.wav", analyze.LoudnormTargets{I: -16, TP: -1.5, LRA: 11})
//	second := stats.SecondPass(targets) // -af value for the normalising pass
//
// The package-level functions use ffmpeg.DefaultRunner; an Analyzer binds
// them to a Runner of your own. Every function has a matching Parse
// function that works on the raw ffmpeg log, so results can also be
// extracted from a run made some other way. The parsers are tested against
// logs captured from every FFmpeg release line.
//
// A result ffmpeg did not report is NaN for values that can be negative
// (loudness and levels in dB) and -1 otherwise. When the filter logged
// nothing usable the error wraps ErrNoResult, which distinguishes a
// changed log format from ffmpeg failing (an *ffmpeg.Error).
package analyze

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
)

// ErrNoResult is wrapped when ffmpeg ran but the filter's output was not
// found in the log.
var ErrNoResult = errors.New("analyze: no result in ffmpeg log")

// Analyzer runs the analysis filters through one Runner. The zero value
// uses ffmpeg.DefaultRunner.
type Analyzer struct {
	Runner ffmpeg.Runner
}

// Default is the Analyzer behind the package-level functions.
var Default = &Analyzer{}

func (a *Analyzer) runner() ffmpeg.Runner {
	if a != nil && a.Runner != nil {
		return a.Runner
	}
	return ffmpeg.DefaultRunner
}

// Interval is a detected span of the input. End is zero and Open is true
// when the condition still held at the end of the input.
type Interval struct {
	Start time.Duration
	End   time.Duration
	Open  bool
}

// Duration returns End - Start, or 0 for an open interval.
func (i Interval) Duration() time.Duration {
	if i.Open {
		return 0
	}
	return i.End - i.Start
}

// run executes an analysis pass: the input filtered through one filter
// into the null muxer at info log level, returning the log.
func run(ctx context.Context, r ffmpeg.Runner, input string, video bool, filter string, inputOpts []ffmpeg.Opt) (string, error) {
	out := []ffmpeg.Opt{ffmpeg.NullOutput()}
	if video {
		out = append(out, ffmpeg.NoAudio(), ffmpeg.NoSubtitles(), ffmpeg.NoData(), ffmpeg.VideoFilter(filter))
	} else {
		out = append(out, ffmpeg.NoVideo(), ffmpeg.NoSubtitles(), ffmpeg.NoData(), ffmpeg.AudioFilter(filter))
	}
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.LogLevel("info")).
		Input(input, inputOpts...).
		Output(ffmpeg.NullTarget, out...)
	res, err := r.Run(ctx, cmd)
	if err != nil {
		return "", err
	}
	return string(res.Stderr), nil
}

// ─── log helpers ───────────────────────────────────────────────────────────

var logPrefix = regexp.MustCompile(`^\[[^\]]*\]\s*`)

// lines splits a log and strips the "[filter @ 0x...] " prefix from each
// line, returning the prefix separately so callers can group by instance.
func lines(log string) []logLine {
	var out []logLine
	for _, raw := range strings.Split(log, "\n") {
		raw = strings.TrimRight(raw, "\r")
		prefix := logPrefix.FindString(raw)
		out = append(out, logLine{instance: strings.TrimSpace(prefix), text: strings.TrimSpace(raw[len(prefix):])})
	}
	return out
}

type logLine struct {
	instance string // e.g. "[Parsed_idet_0 @ 0x55c2b5f15b00]"
	text     string
}

func seconds(s string) time.Duration {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}

func float(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func integer(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// keyValues parses "k:v k2:v2" or "k: v | k2: v2" fragments into a map.
func keyValues(text string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == '|' || r == ',' }) {
		for _, tok := range strings.Fields(part) {
			if k, v, ok := strings.Cut(tok, ":"); ok && v != "" {
				out[k] = v
			} else if ok {
				// "key: value" with a space after the colon
				out[k] = ""
			}
		}
	}
	// Second pass for "key: value" spellings.
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == '|' || r == ',' }) {
		fields := strings.Fields(part)
		for i := 0; i+1 < len(fields); i++ {
			if strings.HasSuffix(fields[i], ":") {
				out[strings.TrimSuffix(fields[i], ":")] = fields[i+1]
			}
		}
	}
	return out
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
