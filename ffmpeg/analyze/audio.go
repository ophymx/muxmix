package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
)

// ─── loudnorm ──────────────────────────────────────────────────────────────

// LoudnormTargets are the EBU R128 targets for the loudnorm filter.
type LoudnormTargets struct {
	I   float64 // integrated loudness in LUFS, e.g. -16 (streaming) or -23 (broadcast)
	TP  float64 // true peak ceiling in dBTP, e.g. -1.5
	LRA float64 // loudness range in LU, e.g. 11
}

// DefaultLoudnormTargets is the common streaming target: -16 LUFS, -1.5 dBTP, 11 LU.
var DefaultLoudnormTargets = LoudnormTargets{I: -16, TP: -1.5, LRA: 11}

// LoudnormStats is what a loudnorm measurement pass reports.
type LoudnormStats struct {
	InputI, InputTP, InputLRA, InputThresh     float64
	OutputI, OutputTP, OutputLRA, OutputThresh float64
	NormalizationType                          string // "dynamic" or "linear"
	TargetOffset                               float64
}

// SecondPass returns the -af value for the normalising pass: loudnorm with
// the measured values, which lets it apply linear (transparent) gain.
func (s *LoudnormStats) SecondPass(t LoudnormTargets) string {
	return fmt.Sprintf("loudnorm=I=%s:TP=%s:LRA=%s:measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true:print_format=summary",
		formatFloat(t.I), formatFloat(t.TP), formatFloat(t.LRA),
		formatFloat(s.InputI), formatFloat(s.InputTP), formatFloat(s.InputLRA), formatFloat(s.InputThresh),
		formatFloat(s.TargetOffset))
}

// Loudnorm runs the loudnorm measurement pass (first of two).
func Loudnorm(ctx context.Context, r ffmpeg.Runner, input string, t LoudnormTargets, inputOpts ...ffmpeg.Opt) (*LoudnormStats, error) {
	filter := fmt.Sprintf("loudnorm=I=%s:TP=%s:LRA=%s:print_format=json", formatFloat(t.I), formatFloat(t.TP), formatFloat(t.LRA))
	log, err := run(ctx, r, input, false, filter, inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseLoudnorm(log)
}

// ParseLoudnorm extracts the JSON block loudnorm prints with print_format=json.
func ParseLoudnorm(log string) (*LoudnormStats, error) {
	start := strings.LastIndex(log, "{")
	end := strings.LastIndex(log, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("analyze: no loudnorm JSON block in log")
	}
	var raw map[string]string
	if err := json.Unmarshal([]byte(log[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("analyze: loudnorm JSON: %w", err)
	}
	return &LoudnormStats{
		InputI:            float(raw["input_i"]),
		InputTP:           float(raw["input_tp"]),
		InputLRA:          float(raw["input_lra"]),
		InputThresh:       float(raw["input_thresh"]),
		OutputI:           float(raw["output_i"]),
		OutputTP:          float(raw["output_tp"]),
		OutputLRA:         float(raw["output_lra"]),
		OutputThresh:      float(raw["output_thresh"]),
		NormalizationType: raw["normalization_type"],
		TargetOffset:      float(raw["target_offset"]),
	}, nil
}

// ─── ebur128 ───────────────────────────────────────────────────────────────

// Loudness is the ebur128 summary: EBU R128 loudness of the whole input.
type Loudness struct {
	Integrated float64 // LUFS
	Threshold  float64 // LUFS, gating threshold for Integrated
	Range      float64 // LU (LRA)
	RangeLow   float64 // LUFS
	RangeHigh  float64 // LUFS
	SamplePeak float64 // dBFS; NaN when not measured
	TruePeak   float64 // dBFS; NaN when not measured
}

// Ebur128 measures the input with the ebur128 filter, including sample and
// true peaks.
func Ebur128(ctx context.Context, r ffmpeg.Runner, input string, inputOpts ...ffmpeg.Opt) (*Loudness, error) {
	log, err := run(ctx, r, input, false, "ebur128=peak=true+sample", inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseEbur128(log)
}

// ParseEbur128 reads the "Summary:" block ebur128 prints at the end.
func ParseEbur128(log string) (*Loudness, error) {
	idx := strings.LastIndex(log, "Summary:")
	if idx < 0 {
		return nil, fmt.Errorf("analyze: no ebur128 summary in log")
	}
	l := &Loudness{SamplePeak: math.NaN(), TruePeak: math.NaN()}
	section := ""
	for _, ln := range lines(log[idx:]) {
		t := ln.text
		switch {
		case strings.HasSuffix(t, ":") && !strings.Contains(t, " LU"):
			section = strings.TrimSuffix(t, ":")
			continue
		}
		key, value, ok := strings.Cut(t, ":")
		if !ok {
			continue
		}
		v := float(strings.Fields(value)[0])
		switch key {
		case "I":
			l.Integrated = v
		case "Threshold":
			if section == "Integrated loudness" {
				l.Threshold = v
			}
		case "LRA":
			l.Range = v
		case "LRA low":
			l.RangeLow = v
		case "LRA high":
			l.RangeHigh = v
		case "Peak":
			if section == "Sample peak" {
				l.SamplePeak = v
			} else if section == "True peak" {
				l.TruePeak = v
			}
		}
	}
	return l, nil
}

// ─── volumedetect ──────────────────────────────────────────────────────────

// Volume is the volumedetect summary.
type Volume struct {
	Samples int64
	MeanDB  float64 // mean square power in dB
	MaxDB   float64 // peak in dB; 0 means the input touches full scale
	// Histogram counts samples at each dB below full scale: key 0 is the
	// number of samples at 0 dB, key 1 at -1 dB, and so on.
	Histogram map[int]int64
}

// Headroom returns how many dB the input can be raised before clipping.
func (v *Volume) Headroom() float64 { return -v.MaxDB }

// VolumeDetect runs the volumedetect filter.
func VolumeDetect(ctx context.Context, r ffmpeg.Runner, input string, inputOpts ...ffmpeg.Opt) (*Volume, error) {
	log, err := run(ctx, r, input, false, "volumedetect", inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseVolumeDetect(log)
}

// ParseVolumeDetect reads the volumedetect summary. ffmpeg may print a
// zero-sample summary for a filter instance replaced during graph
// reconfiguration; the instance with the most samples wins.
func ParseVolumeDetect(log string) (*Volume, error) {
	byInstance := map[string]*Volume{}
	var best *Volume
	for _, ln := range lines(log) {
		if !strings.Contains(ln.instance, "volumedetect") {
			continue
		}
		v := byInstance[ln.instance]
		if v == nil {
			v = &Volume{Histogram: map[int]int64{}}
			byInstance[ln.instance] = v
		}
		key, value, ok := strings.Cut(ln.text, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "dB"))
		switch {
		case key == "n_samples":
			v.Samples = integer(value)
		case key == "mean_volume":
			v.MeanDB = float(value)
		case key == "max_volume":
			v.MaxDB = float(value)
		case strings.HasPrefix(key, "histogram_") && strings.HasSuffix(key, "db"):
			db := int(integer(strings.TrimSuffix(strings.TrimPrefix(key, "histogram_"), "db")))
			v.Histogram[db] = integer(value)
		}
		if best == nil || v.Samples > best.Samples {
			best = v
		}
	}
	if best == nil {
		return nil, fmt.Errorf("analyze: no volumedetect output in log")
	}
	return best, nil
}

// ─── silencedetect ─────────────────────────────────────────────────────────

// SilenceOptions configures silence detection.
type SilenceOptions struct {
	NoiseDB     float64       // threshold below which audio counts as silence; default -60 dB
	MinDuration time.Duration // shortest silence to report; default 2s
}

// Silence runs silencedetect and returns the silent intervals.
func Silence(ctx context.Context, r ffmpeg.Runner, input string, o SilenceOptions, inputOpts ...ffmpeg.Opt) ([]Interval, error) {
	if o.NoiseDB == 0 {
		o.NoiseDB = -60
	}
	if o.MinDuration == 0 {
		o.MinDuration = 2 * time.Second
	}
	filter := fmt.Sprintf("silencedetect=n=%sdB:d=%s", formatFloat(o.NoiseDB), formatFloat(o.MinDuration.Seconds()))
	log, err := run(ctx, r, input, false, filter, inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseSilence(log), nil
}

// ParseSilence reads silence_start / silence_end lines.
func ParseSilence(log string) []Interval {
	return parseIntervals(log, "silence_start", "silence_end")
}

// parseIntervals pairs "<start>: t" and "<end>: t" lines, in either the
// "key: value" or "key:value" spelling, with or without a filter prefix.
func parseIntervals(log, startKey, endKey string) []Interval {
	var out []Interval
	open := -1
	for _, ln := range lines(log) {
		kv := keyValues(ln.text)
		if s, ok := kv[startKey]; ok {
			out = append(out, Interval{Start: seconds(s), Open: true})
			open = len(out) - 1
			// blackdetect prints start and end on one line.
			if e, ok := kv[endKey]; ok {
				out[open].End = seconds(e)
				out[open].Open = false
				open = -1
			}
			continue
		}
		if e, ok := kv[endKey]; ok && open >= 0 {
			out[open].End = seconds(e)
			out[open].Open = false
			open = -1
		}
	}
	return out
}

// ─── astats ────────────────────────────────────────────────────────────────

// AudioStats is the overall section of the astats filter.
type AudioStats struct {
	DCOffset     float64
	MinLevel     float64
	MaxLevel     float64
	PeakLevelDB  float64
	RMSLevelDB   float64
	RMSPeakDB    float64
	RMSTroughDB  float64
	CrestFactor  float64
	FlatFactor   float64
	PeakCount    int64
	NoiseFloorDB float64
	Entropy      float64
	BitDepth     string
	Samples      int64
	// Raw holds every "Key: value" line of the overall section.
	Raw map[string]string
}

// AStats runs astats and returns the overall statistics.
func AStats(ctx context.Context, r ffmpeg.Runner, input string, inputOpts ...ffmpeg.Opt) (*AudioStats, error) {
	log, err := run(ctx, r, input, false, "astats=measure_perchannel=none", inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseAStats(log)
}

// ParseAStats reads the "Overall" section of astats output.
func ParseAStats(log string) (*AudioStats, error) {
	idx := strings.LastIndex(log, "] Overall")
	if idx < 0 {
		return nil, fmt.Errorf("analyze: no astats overall section in log")
	}
	s := &AudioStats{Raw: map[string]string{}}
	for _, ln := range lines(log[idx:]) {
		if !strings.Contains(ln.instance, "astats") {
			continue
		}
		key, value, ok := strings.Cut(ln.text, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		s.Raw[key] = value
		switch key {
		case "DC offset":
			s.DCOffset = float(value)
		case "Min level":
			s.MinLevel = float(value)
		case "Max level":
			s.MaxLevel = float(value)
		case "Peak level dB":
			s.PeakLevelDB = float(value)
		case "RMS level dB":
			s.RMSLevelDB = float(value)
		case "RMS peak dB":
			s.RMSPeakDB = float(value)
		case "RMS trough dB":
			s.RMSTroughDB = float(value)
		case "Crest factor":
			s.CrestFactor = float(value)
		case "Flat factor":
			s.FlatFactor = float(value)
		case "Peak count":
			s.PeakCount = int64(float(value))
		case "Noise floor dB":
			s.NoiseFloorDB = float(value)
		case "Entropy":
			s.Entropy = float(value)
		case "Bit depth":
			s.BitDepth = value
		case "Number of samples":
			s.Samples = integer(value)
		}
	}
	return s, nil
}
