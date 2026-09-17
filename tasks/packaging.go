package tasks

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
)

// PackageFormat selects the streaming format.
type PackageFormat string

const (
	// HLS writes a master playlist, one media playlist per rendition and
	// an audio group, using ffmpeg's hls muxer.
	HLS PackageFormat = "hls"
	// DASH writes an MPD with segment templates using the dash muxer.
	DASH PackageFormat = "dash"
	// CMAF writes DASH and HLS playlists over one set of fMP4 segments.
	CMAF PackageFormat = "cmaf"
)

// SegmentType selects the HLS container.
type SegmentType string

const (
	FMP4   SegmentType = "fmp4"   // fragmented MP4 (default; required for HEVC and CMAF)
	MPEGTS SegmentType = "mpegts" // transport stream, the most compatible for H.264 HLS
)

// Rendition is one rung of the bitrate ladder.
type Rendition struct {
	Name    string // playlist directory and variant name, e.g. "720p"
	Height  int    // output height; width follows the aspect ratio
	Bitrate string // target video bit rate, e.g. "2800k"
	MaxRate string // VBV cap (default: Bitrate × 1.07)
	BufSize string // VBV buffer (default: Bitrate × 1.5)
	// Video overrides the shared encode settings for this rendition.
	// Fields left zero inherit from PackageOptions.Video; naming a Codec
	// or Encoder opts the rung out of the shared encoder and hardware
	// selection.
	Video *encode.Video
}

// DefaultLadder returns the standard rungs at or below the source height:
// 1080p, 720p, 480p and 360p with Apple-style bit rates. A source shorter
// than 360 lines gets a single rung at its own height.
func DefaultLadder(sourceHeight int) []Rendition {
	all := []Rendition{
		{Name: "1080p", Height: 1080, Bitrate: "5000k", MaxRate: "5350k", BufSize: "7500k"},
		{Name: "720p", Height: 720, Bitrate: "2800k", MaxRate: "2996k", BufSize: "4200k"},
		{Name: "480p", Height: 480, Bitrate: "1400k", MaxRate: "1498k", BufSize: "2100k"},
		{Name: "360p", Height: 360, Bitrate: "800k", MaxRate: "856k", BufSize: "1200k"},
	}
	var out []Rendition
	for _, r := range all {
		if sourceHeight <= 0 || r.Height <= sourceHeight {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		h := even(sourceHeight)
		out = []Rendition{{Name: fmt.Sprintf("%dp", h), Height: h, Bitrate: "400k", MaxRate: "428k", BufSize: "600k"}}
	}
	return out
}

// PackageOptions configures HLS/DASH packaging.
type PackageOptions struct {
	Format   PackageFormat // default HLS
	Segments SegmentType   // HLS only; default FMP4
	// Renditions is the ladder; nil uses DefaultLadder for the source.
	Renditions []Rendition
	// Video settings shared by all renditions (codec, speed, profile);
	// bit rates come from each Rendition. Default H.264, yuv420p.
	Video encode.Video
	// Audio settings for the audio rendition(s); default AAC 128k.
	Audio encode.Audio
	// AudioLanguages selects which audio streams become alternate audio
	// renditions, by language tag. Nil packages the main audio stream only.
	AudioLanguages []string
	// SegmentDuration (default 6s). Keyframes are forced on every boundary.
	SegmentDuration time.Duration
	// MasterName names the HLS master playlist (default "master.m3u8");
	// ManifestName the DASH MPD (default "manifest.mpd").
	MasterName   string
	ManifestName string
	// HW is the hardware encoding policy, as for TranscodeOptions. One
	// backend serves every rung.
	HW    hwaccel.Policy
	Extra []ffmpeg.Opt
	// Info is an already probed description of the input, from Inspect;
	// nil probes it. Reuse it when the same file gets several tasks.
	Info *Info
}

// PackagedRendition is one video variant in the output.
type PackagedRendition struct {
	Name          string
	Width, Height int
	Bitrate       string
	Playlist      string // media playlist path (HLS); "" for DASH
}

// PackagedAudio is one audio rendition in the output.
type PackagedAudio struct {
	Language string
	Playlist string
}

// PackageResult describes the generated package.
type PackageResult struct {
	Dir        string
	Master     string // HLS master playlist path, when HLS or CMAF
	Manifest   string // DASH MPD path, when DASH or CMAF
	Renditions []PackagedRendition
	Audio      []PackagedAudio
	Command    *ffmpeg.Command
	// Duration of the source, for progress.
	Duration time.Duration
	// HW is the hardware path the video rungs use; zero means software.
	// HWReason explains a software outcome under a Prefer policy.
	HW       hwaccel.Selection
	HWReason string
	// Skipped names the renditions taller than the source, which are not
	// produced rather than upscaled.
	Skipped []string
}

// Run executes the package command through t (nil for Default), adding
// ffmpeg.TotalDuration from the probe so progress callbacks get Fraction
// and ETA. opts are applied after the Tools' RunOptions.
func (r *PackageResult) Run(ctx context.Context, t *Tools, opts ...ffmpeg.RunOption) error {
	if t == nil {
		t = Default
	}
	var all []ffmpeg.RunOption
	if r.Duration > 0 {
		all = append(all, ffmpeg.TotalDuration(r.Duration))
	}
	return t.run(ctx, r.Command, append(all, opts...)...)
}

// Package encodes a bitrate ladder and writes HLS and/or DASH into outDir.
func Package(ctx context.Context, input, outDir string, o PackageOptions) (*PackageResult, error) {
	return Default.Package(ctx, input, outDir, o)
}

// Package encodes a bitrate ladder and writes HLS and/or DASH into outDir.
func (t *Tools) Package(ctx context.Context, input, outDir string, o PackageOptions) (*PackageResult, error) {
	res, err := t.PlanPackage(ctx, input, outDir, o)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	if err := res.Run(ctx, t); err != nil {
		return res, err
	}
	return res, nil
}

// PlanPackage builds the package with the default Tools without running it.
func PlanPackage(ctx context.Context, input, outDir string, o PackageOptions) (*PackageResult, error) {
	return Default.PlanPackage(ctx, input, outDir, o)
}

// PlanPackage probes the input and builds the command without running it.
func (t *Tools) PlanPackage(ctx context.Context, input, outDir string, o PackageOptions) (*PackageResult, error) {
	info, err := t.info(ctx, input, o.Info)
	if err != nil {
		return nil, err
	}
	res, err := BuildPackagePlan(info, t.system(), input, outDir, o)
	if err != nil {
		return nil, err
	}
	if set := t.caps(); set != nil {
		if err := caps.Check(ctx, set, t.runner(), res.Command); err != nil {
			return res, err
		}
	}
	return res, nil
}

var safeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// BuildPackagePlan builds the packaging command from an already probed input.
// sys supplies the build capabilities and hardware probes as for
// BuildTranscodePlan.
func BuildPackagePlan(info *Info, sys *hwaccel.System, input, outDir string, o PackageOptions) (*PackageResult, error) {
	if !info.HasVideo {
		return nil, fmt.Errorf("tasks: %s has no video stream", input)
	}
	if o.Format == "" {
		o.Format = HLS
	}
	if o.Segments == "" {
		o.Segments = FMP4
	}
	if o.Format != HLS && o.Segments != FMP4 {
		return nil, fmt.Errorf("tasks: %s requires fMP4 segments", o.Format)
	}
	if o.SegmentDuration <= 0 {
		o.SegmentDuration = 6 * time.Second
	}
	if o.MasterName == "" {
		o.MasterName = "master.m3u8"
	}
	if o.ManifestName == "" {
		o.ManifestName = "manifest.mpd"
	}
	var renditions, skipped []Rendition
	for _, r := range o.Renditions {
		if info.Height > 0 && r.Height > info.Height {
			skipped = append(skipped, r)
			continue
		}
		renditions = append(renditions, r)
	}
	if len(o.Renditions) == 0 {
		renditions = DefaultLadder(info.Height)
	} else if len(renditions) == 0 {
		return nil, fmt.Errorf("tasks: every rendition is taller than the %dp source", info.Height)
	}
	base := o.Video
	if base.Codec == "" && base.Encoder == "" {
		base.Codec = encode.H264
	}
	hw, hwReason, err := resolveHW(sys, o.HW, &base)
	if err != nil {
		return nil, err
	}
	if base.PixFmt == "" && !hw.Hardware() {
		base.PixFmt = "yuv420p"
	}
	audio := o.Audio
	if audio.Codec == "" && audio.Encoder == "" {
		audio.Codec = encode.AAC
		if audio.Bitrate == "" && audio.VBR == 0 {
			audio.Bitrate = "128k"
		}
	}
	segSecs := o.SegmentDuration.Seconds()

	res := &PackageResult{Dir: outDir, HW: hw, HWReason: hwReason, Duration: info.Duration}
	for _, r := range skipped {
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("%dp", r.Height)
		}
		res.Skipped = append(res.Skipped, name)
	}
	out := ffmpeg.NewOutput("")
	var graph []string
	var varMap []string

	// Video renditions: split the decoded stream once, scale per rung.
	labels := make([]string, len(renditions))
	for i := range renditions {
		labels[i] = fmt.Sprintf("[v%d]", i)
	}
	graph = append(graph, fmt.Sprintf("[0:v:0]split=%d%s", len(renditions), strings.Join(labels, "")))
	fps := info.Probe.VideoStream().FrameRate().Float64()
	for i, r := range renditions {
		if r.Height <= 0 {
			return nil, fmt.Errorf("tasks: rendition %d needs a Height", i)
		}
		if r.Bitrate == "" {
			return nil, fmt.Errorf("tasks: rendition %s needs a Bitrate", r.Name)
		}
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("%dp", r.Height)
		}
		name = safeName.ReplaceAllString(name, "_")
		h := even(r.Height)
		graph = append(graph, fmt.Sprintf("%s%s[v%dout]", labels[i], hw.Filter(fmt.Sprintf("scale=w=-2:h=%d", h)), i))

		v := base
		if r.Video != nil {
			v = mergeVideo(base, *r.Video)
		}
		v.Bitrate, v.MaxRate, v.BufSize = r.Bitrate, r.MaxRate, r.BufSize
		if v.MaxRate == "" {
			v.MaxRate = scaleRate(r.Bitrate, 1.07)
		}
		if v.BufSize == "" {
			v.BufSize = scaleRate(r.Bitrate, 1.5)
		}
		enc, err := v.ResolveEncoder(capsOf(sys))
		if err != nil {
			return nil, err
		}
		opts := v.OptsFor(enc)
		if fps > 0 {
			g := int(math.Round(fps * segSecs))
			opts = append(opts, ffmpeg.GOP(g), ffmpeg.Set("keyint_min", fmt.Sprint(g)))
		}
		opts = append(opts, ffmpeg.Set("sc_threshold", "0"))
		out.Options.Add(ffmpeg.MapLabel(fmt.Sprintf("v%dout", i)), ffmpeg.PerStream("v", i, opts...))

		w, _ := fitSize(info.Width, info.Height, 0, h)
		res.Renditions = append(res.Renditions, PackagedRendition{Name: name, Width: w, Height: h, Bitrate: r.Bitrate})
		varMap = append(varMap, fmt.Sprintf("v:%d,agroup:audio,name:%s", i, name))
	}

	// Audio renditions.
	var audioStreams []*audioPick
	if info.HasAudio {
		if len(o.AudioLanguages) == 0 {
			if a := info.Probe.AudioStream(); a != nil {
				audioStreams = append(audioStreams, &audioPick{index: a.Index.Int(), lang: a.Language(), def: true})
			}
		} else {
			for _, lang := range o.AudioLanguages {
				for _, a := range info.Probe.AudioStreams() {
					if languageMatches(a.Language(), []string{lang}) {
						audioStreams = append(audioStreams, &audioPick{index: a.Index.Int(), lang: a.Language(), def: len(audioStreams) == 0})
						break
					}
				}
			}
		}
	}
	aenc, err := audio.ResolveEncoder(capsOf(sys))
	if err != nil && len(audioStreams) > 0 {
		return nil, err
	}
	for j, a := range audioStreams {
		out.Options.Add(ffmpeg.Map(fmt.Sprintf("0:%d", a.index)), ffmpeg.PerStream("a", j, audio.OptsFor(aenc)...))
		name := "audio"
		if a.lang != "" {
			name += "-" + a.lang
		}
		entry := fmt.Sprintf("a:%d,agroup:audio,name:%s", j, name)
		if a.lang != "" {
			entry += ",language:" + a.lang
		}
		if a.def {
			entry += ",default:yes"
		}
		varMap = append([]string{entry}, varMap...)
		res.Audio = append(res.Audio, PackagedAudio{Language: a.lang})
		a.name = name
	}
	if len(audioStreams) == 0 {
		for i := range varMap {
			varMap[i] = strings.Replace(varMap[i], ",agroup:audio", "", 1)
		}
	}

	out.Options.Add(ffmpeg.Set("force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%s)", ftoa(segSecs))))

	switch o.Format {
	case HLS:
		segExt := "m4s"
		if o.Segments == MPEGTS {
			segExt = "ts"
		}
		out.Options.Add(
			ffmpeg.Format("hls"),
			ffmpeg.Set("hls_time", ftoa(segSecs)),
			ffmpeg.Set("hls_playlist_type", "vod"),
			ffmpeg.Set("hls_list_size", "0"),
			ffmpeg.Set("hls_flags", "independent_segments"),
			ffmpeg.Set("hls_segment_type", string(o.Segments)),
			ffmpeg.Set("master_pl_name", o.MasterName),
			ffmpeg.Set("var_stream_map", strings.Join(varMap, " ")),
			ffmpeg.Set("hls_segment_filename", filepath.Join(outDir, "%v", "seg_%05d."+segExt)),
		)
		out.URL = filepath.Join(outDir, "%v", "index.m3u8")
		res.Master = filepath.Join(outDir, o.MasterName)
		for i := range res.Renditions {
			res.Renditions[i].Playlist = filepath.Join(outDir, res.Renditions[i].Name, "index.m3u8")
		}
		for j, a := range audioStreams {
			res.Audio[j].Playlist = filepath.Join(outDir, a.name, "index.m3u8")
		}
	case DASH, CMAF:
		sets := "id=0,streams=v"
		if len(audioStreams) > 0 {
			sets += " id=1,streams=a"
		}
		out.Options.Add(
			ffmpeg.Format("dash"),
			ffmpeg.Set("seg_duration", ftoa(segSecs)),
			ffmpeg.Set("use_template", "1"),
			ffmpeg.Set("use_timeline", "1"),
			ffmpeg.Set("init_seg_name", "init-$RepresentationID$.m4s"),
			ffmpeg.Set("media_seg_name", "chunk-$RepresentationID$-$Number%05d$.m4s"),
			ffmpeg.Set("adaptation_sets", sets),
		)
		if o.Format == CMAF {
			out.Options.Add(ffmpeg.Set("hls_playlist", "1"), ffmpeg.Set("hls_master_name", o.MasterName))
			res.Master = filepath.Join(outDir, o.MasterName)
			for i := range res.Renditions {
				res.Renditions[i].Playlist = filepath.Join(outDir, fmt.Sprintf("media_%d.m3u8", i))
			}
			for j := range audioStreams {
				res.Audio[j].Playlist = filepath.Join(outDir, fmt.Sprintf("media_%d.m3u8", len(renditions)+j))
			}
		}
		out.URL = filepath.Join(outDir, o.ManifestName)
		res.Manifest = out.URL
	default:
		return nil, fmt.Errorf("tasks: unknown package format %q", o.Format)
	}
	out.Options.Add(o.Extra...)

	res.Command = &ffmpeg.Command{
		Inputs:  []*ffmpeg.Input{ffmpeg.NewInput(input)},
		Outputs: []*ffmpeg.Output{out},
	}
	hw.Apply(res.Command)
	res.Command.GlobalOptions(ffmpeg.FilterComplexString(strings.Join(graph, ";")))
	return res, nil
}

type audioPick struct {
	index int
	lang  string
	def   bool
	name  string
}

// scaleRate multiplies a rate such as "2800k" by f, keeping the suffix.
func scaleRate(rate string, f float64) string {
	num := strings.TrimRight(rate, "kKmMgG")
	suffix := rate[len(num):]
	var v float64
	if _, err := fmt.Sscanf(num, "%g", &v); err != nil {
		return rate
	}
	return fmt.Sprintf("%d%s", int(math.Round(v*f)), suffix)
}

func ftoa(f float64) string { return fmt.Sprintf("%g", f) }

// mergeVideo fills the zero fields of an override from the shared
// settings. An override that names its own Codec or Encoder does not
// inherit the shared encoder or hardware backend.
func mergeVideo(base, over encode.Video) encode.Video {
	v := over
	ownEncoder := over.Codec != "" || over.Encoder != ""
	if !ownEncoder {
		v.Codec, v.Encoder, v.HW = base.Codec, base.Encoder, base.HW
	}
	if v.Quality == 0 {
		v.Quality = base.Quality
	}
	if v.Speed == encode.DefaultSpeed {
		v.Speed = base.Speed
	}
	if v.Profile == "" {
		v.Profile = base.Profile
	}
	if v.Level == "" {
		v.Level = base.Level
	}
	if v.Tune == "" {
		v.Tune = base.Tune
	}
	if v.PixFmt == "" && (base.HW == hwaccel.None || ownEncoder) {
		v.PixFmt = base.PixFmt
	}
	if v.KeyframeInterval == 0 {
		v.KeyframeInterval = base.KeyframeInterval
	}
	if v.Extra == nil {
		v.Extra = base.Extra
	}
	return v
}
