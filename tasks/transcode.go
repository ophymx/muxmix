package tasks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/encode"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
	"github.com/ophymx/muxmix/ffprobe"
)

// Action is what a transcode does with a stream.
type Action string

const (
	Copy   Action = "copy"
	Encode Action = "encode"
	Drop   Action = "drop"
)

// VideoRule decides what happens to video streams.
type VideoRule struct {
	// Drop removes all video.
	Drop bool
	// Encode settings used when a stream is not copied. Codec defaults to
	// H.264 with quality 23.
	Encode encode.Video
	// CopyCodecs lists codec families that are copied rather than encoded
	// when the container accepts them. Nil means never copy; use
	// AllCodecs to copy whenever possible.
	CopyCodecs []encode.Codec
	// MaxWidth and MaxHeight scale down larger sources (aspect kept).
	// A stream that needs scaling is encoded even if it could be copied.
	MaxWidth, MaxHeight int
	// Filters are applied to every encoded video stream, before any
	// scaling, as one -filter:v chain ("crop=1920:800", "unsharp"). A
	// stream that could be copied is still copied; set CopyCodecs to nil
	// to force encoding.
	Filters []string
	// KeepAll keeps every video stream; the default keeps only the main one.
	KeepAll bool
	// KeepAttachedPic keeps cover art streams.
	KeepAttachedPic bool
}

// AudioRule decides what happens to audio streams.
type AudioRule struct {
	Drop bool
	// Encode settings used when a stream is not copied. Codec defaults to
	// AAC at 160k.
	Encode encode.Audio
	// CopyCodecs lists codec families copied when the container accepts them.
	CopyCodecs []encode.Codec
	// Languages keeps only streams whose language tag is listed (ISO 639
	// as in the file, e.g. "eng"); untagged streams match "und". Nil keeps all.
	Languages []string
	// Filters are applied to every encoded audio stream as one -filter:a
	// chain, e.g. a loudnorm second pass from analyze.
	Filters []string
	// MainOnly keeps only the main audio stream (default-flagged, else
	// first); the default keeps every audio stream.
	MainOnly bool
}

// SubtitleRule decides what happens to subtitle streams.
type SubtitleRule struct {
	Drop bool
	// Languages filters as for audio.
	Languages []string
	// Encoder names the subtitle encoder for text subtitles the container
	// cannot hold as-is; empty picks per container (mov_text for MP4,
	// webvtt for WebM, srt for Matroska). Bitmap subtitles are always
	// copied when the container allows and dropped otherwise.
	Encoder string
}

// AllCodecs is a CopyCodecs value meaning "copy whenever the container
// accepts the source codec".
var AllCodecs = []encode.Codec{"*"}

// TranscodeOptions describes a transcode declaratively.
type TranscodeOptions struct {
	Video     VideoRule
	Audio     AudioRule
	Subtitles SubtitleRule
	// HW says whether to encode video in hardware: the zero value is
	// software, hwaccel.PreferHardware falls back to software, and
	// hwaccel.RequireHardware fails without it. It is resolved against the
	// System on the Tools; the plan records the outcome per stream.
	HW hwaccel.Policy
	// DropMetadata and DropChapters stop the input's global metadata and
	// chapters from being carried over.
	DropMetadata bool
	DropChapters bool
	// NoFastStart leaves the MP4 index at the end of the file. By default
	// .mp4, .mov and .m4a outputs get +faststart so playback can begin
	// before the download finishes.
	NoFastStart bool
	// TwoPass encodes video in two passes for a more accurate bitrate
	// target (see ffmpeg.TwoPass). It applies only when Video.Encode has
	// a Bitrate; quality-targeted encodes ignore it.
	TwoPass bool
	// Extra output options appended verbatim. Build one with ffmpeg.Opts;
	// it is data so TranscodeOptions can be serialized.
	Extra ffmpeg.Options
	// Info is an already probed description of the input, from Inspect;
	// nil probes it. Reuse it when the same file gets several tasks.
	Info *Info
}

// PlannedStream is one input stream and what will happen to it.
type PlannedStream struct {
	Input    int    // input stream index
	Output   int    // output index within its type, -1 when dropped
	Type     string // video, audio, subtitle, data, attachment
	Codec    string // source codec name
	Language string
	Action   Action
	Encoder  string // encoder used when Action is Encode
	// HW is the hardware path when Action is Encode; its zero value is
	// software.
	HW     hwaccel.Selection
	Reason string // why this action was chosen
}

// TranscodePlan is the resolved transcode: the streams and the command.
type TranscodePlan struct {
	Input   string
	Output  string
	Streams []PlannedStream
	Command *ffmpeg.Command
	Info    *Info
	// TwoPass is set when Run will use ffmpeg.TwoPass.
	TwoPass bool
}

// Run executes the plan through t (nil for Default), adding
// ffmpeg.TotalDuration from the probe so progress callbacks get Fraction
// and ETA. opts are applied after the Tools' RunOptions.
func (p *TranscodePlan) Run(ctx context.Context, t *Tools, opts ...ffmpeg.RunOption) error {
	if t == nil {
		t = Default
	}
	var all []ffmpeg.RunOption
	if p.Info != nil && p.Info.Duration > 0 {
		all = append(all, ffmpeg.TotalDuration(p.Info.Duration))
	}
	all = append(all, opts...)
	if p.TwoPass {
		var d time.Duration
		if p.Info != nil {
			d = p.Info.Duration
		}
		_, err := ffmpeg.TwoPass(ctx, p.Command, ffmpeg.TwoPassOptions{
			Runner: t.runner(), Duration: d, RunOptions: append(append([]ffmpeg.RunOption(nil), t.RunOptions...), all...),
		})
		return err
	}
	return t.run(ctx, p.Command, all...)
}

// Kept returns the planned streams that make it to the output.
func (p *TranscodePlan) Kept() []PlannedStream {
	var out []PlannedStream
	for _, s := range p.Streams {
		if s.Action != Drop {
			out = append(out, s)
		}
	}
	return out
}

// String summarises the plan, one line per stream.
func (p *TranscodePlan) String() string {
	var sb strings.Builder
	for _, s := range p.Streams {
		fmt.Fprintf(&sb, "%d %s %s", s.Input, s.Type, s.Codec)
		if s.Language != "" {
			fmt.Fprintf(&sb, " [%s]", s.Language)
		}
		fmt.Fprintf(&sb, ": %s", s.Action)
		switch {
		case s.HW.Hardware():
			fmt.Fprintf(&sb, " → %s", s.HW)
		case s.Encoder != "":
			fmt.Fprintf(&sb, " → %s", s.Encoder)
		}
		if s.Reason != "" {
			fmt.Fprintf(&sb, " (%s)", s.Reason)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Transcode plans and runs a transcode.
func Transcode(ctx context.Context, input, output string, o TranscodeOptions) (*TranscodePlan, error) {
	return Default.Transcode(ctx, input, output, o)
}

// Transcode plans and runs a transcode.
func (t *Tools) Transcode(ctx context.Context, input, output string, o TranscodeOptions) (*TranscodePlan, error) {
	plan, err := t.PlanTranscode(ctx, input, output, o)
	if err != nil {
		return nil, err
	}
	if err := ensureDir(output); err != nil {
		return plan, err
	}
	if err := plan.Run(ctx, t); err != nil {
		return plan, err
	}
	return plan, nil
}

// PlanTranscode probes the input and builds the command with the default
// Tools, without running it.
func PlanTranscode(ctx context.Context, input, output string, o TranscodeOptions) (*TranscodePlan, error) {
	return Default.PlanTranscode(ctx, input, output, o)
}

// PlanTranscode probes the input and builds the command without running it.
func (t *Tools) PlanTranscode(ctx context.Context, input, output string, o TranscodeOptions) (*TranscodePlan, error) {
	info, err := t.info(ctx, input, o.Info)
	if err != nil {
		return nil, err
	}
	plan, err := BuildTranscodePlan(info, t.system(), input, output, o)
	if err != nil {
		return nil, err
	}
	if set := t.caps(); set != nil {
		if err := caps.Check(ctx, set, t.runner(), plan.Command); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// BuildTranscodePlan builds a plan from an already probed input. sys
// supplies the build capabilities and hardware probes; nil means software
// encoders chosen without checking the build.
func BuildTranscodePlan(info *Info, sys *hwaccel.System, input, output string, o TranscodeOptions) (*TranscodePlan, error) {
	container := containerFor(output)
	if container == nil {
		return nil, fmt.Errorf("tasks: unsupported output container %q", ext(output))
	}
	if info == nil || info.Probe == nil {
		return nil, fmt.Errorf("tasks: transcode planning needs Info.Probe (use Inspect or InfoFrom)")
	}
	plan := &TranscodePlan{Input: input, Output: output, Info: info}
	out := ffmpeg.NewOutput(output)
	plan.TwoPass = o.TwoPass && !o.Video.Drop && o.Video.Encode.Bitrate != ""

	// Video.
	var hw hwaccel.Selection
	if !o.Video.Drop {
		vrule := o.Video
		defaulted := vrule.Encode.Codec == "" && vrule.Encode.Encoder == ""
		if defaulted {
			vrule.Encode.Codec = encode.H264
			if vrule.Encode.Quality == 0 && vrule.Encode.Bitrate == "" {
				vrule.Encode.Quality = 23
			}
		}
		var hwReason string
		var err error
		if hw, hwReason, err = resolveHW(sys, o.HW, &vrule.Encode); err != nil {
			return nil, err
		}
		if defaulted && !hw.Hardware() && vrule.Encode.PixFmt == "" {
			vrule.Encode.PixFmt = "yuv420p"
		}
		idx := 0
		for _, s := range info.Probe.VideoStreams() {
			ps := PlannedStream{Input: s.Index.Int(), Output: -1, Type: "video", Codec: s.CodecName, Language: s.Language()}
			switch {
			case s.IsAttachedPic() && !vrule.KeepAttachedPic:
				ps.Action, ps.Reason = Drop, "cover art"
			case !vrule.KeepAll && idx > 0:
				ps.Action, ps.Reason = Drop, "not the main video stream"
			default:
				w, h := s.Resolution()
				if rot := s.Rotation(); rot == 90 || rot == 270 {
					w, h = h, w
				}
				needsScale := (vrule.MaxWidth > 0 && w > vrule.MaxWidth) || (vrule.MaxHeight > 0 && h > vrule.MaxHeight)
				if canCopy(s.CodecName, vrule.CopyCodecs, container.video) && !needsScale {
					ps.Action, ps.Reason = Copy, "codec accepted by container"
					out.Options.Add(ffmpeg.Map(fmt.Sprintf("0:%d", ps.Input)), ffmpeg.PerStream("v", idx, ffmpeg.Copy("v")))
				} else {
					enc, err := vrule.Encode.ResolveEncoder(capsOf(sys))
					if err != nil {
						return nil, err
					}
					// A 10-bit source keeps its depth when the device
					// proved it encodes p010; otherwise the upload
					// flattens it to 8-bit as before.
					sel := sys.PreserveDepth(hw, s.PixFmt)
					ps.Action, ps.Encoder, ps.HW = Encode, enc, sel
					ps.Reason = encodeReason(s.CodecName, container, needsScale)
					if hwReason != "" {
						ps.Reason += "; software: " + hwReason
					}
					opts := vrule.Encode.OptsFor(enc)
					filters := append([]string(nil), vrule.Filters...)
					if needsScale {
						filters = append(filters, scaleFilter(vrule.MaxWidth, vrule.MaxHeight))
					}
					if chain := sel.Filter(filters...); chain != "" {
						opts = append(opts, ffmpeg.FilterString("v", chain))
					}
					out.Options.Add(ffmpeg.Map(fmt.Sprintf("0:%d", ps.Input)), ffmpeg.PerStream("v", idx, opts...))
				}
				ps.Output = idx
				idx++
			}
			plan.Streams = append(plan.Streams, ps)
		}
	} else {
		for _, s := range info.Probe.VideoStreams() {
			plan.Streams = append(plan.Streams, PlannedStream{Input: s.Index.Int(), Output: -1, Type: "video", Codec: s.CodecName, Action: Drop, Reason: "video dropped"})
		}
	}

	// Audio.
	if !o.Audio.Drop {
		arule := o.Audio
		if arule.Encode.Codec == "" && arule.Encode.Encoder == "" {
			arule.Encode.Codec = encode.AAC
			if arule.Encode.Bitrate == "" && arule.Encode.VBR == 0 {
				arule.Encode.Bitrate = "160k"
			}
		}
		main := info.Probe.AudioStream()
		idx := 0
		for _, s := range info.Probe.AudioStreams() {
			ps := PlannedStream{Input: s.Index.Int(), Output: -1, Type: "audio", Codec: s.CodecName, Language: s.Language()}
			switch {
			case arule.MainOnly && s != main:
				ps.Action, ps.Reason = Drop, "not the main audio stream"
			case !languageMatches(s.Language(), arule.Languages):
				ps.Action, ps.Reason = Drop, "language not selected"
			case canCopy(s.CodecName, arule.CopyCodecs, container.audio):
				ps.Action, ps.Reason, ps.Output = Copy, "codec accepted by container", idx
				out.Options.Add(ffmpeg.Map(fmt.Sprintf("0:%d", ps.Input)), ffmpeg.PerStream("a", idx, ffmpeg.Copy("a")))
				idx++
			default:
				enc, err := arule.Encode.ResolveEncoder(capsOf(sys))
				if err != nil {
					return nil, err
				}
				ps.Action, ps.Encoder, ps.Output = Encode, enc, idx
				ps.Reason = encodeReason(s.CodecName, container, false)
				aopts := arule.Encode.OptsFor(enc)
				if len(arule.Filters) > 0 {
					aopts = append(aopts, ffmpeg.FilterString("a", strings.Join(arule.Filters, ",")))
				}
				out.Options.Add(ffmpeg.Map(fmt.Sprintf("0:%d", ps.Input)), ffmpeg.PerStream("a", idx, aopts...))
				idx++
			}
			plan.Streams = append(plan.Streams, ps)
		}
	} else {
		for _, s := range info.Probe.AudioStreams() {
			plan.Streams = append(plan.Streams, PlannedStream{Input: s.Index.Int(), Output: -1, Type: "audio", Codec: s.CodecName, Language: s.Language(), Action: Drop, Reason: "audio dropped"})
		}
	}

	// Subtitles.
	idx := 0
	for _, s := range info.Probe.SubtitleStreams() {
		ps := PlannedStream{Input: s.Index.Int(), Output: -1, Type: "subtitle", Codec: s.CodecName, Language: s.Language()}
		bitmap := isBitmapSubtitle(s.CodecName)
		switch {
		case o.Subtitles.Drop:
			ps.Action, ps.Reason = Drop, "subtitles dropped"
		case !languageMatches(s.Language(), o.Subtitles.Languages):
			ps.Action, ps.Reason = Drop, "language not selected"
		case contains(container.subtitles, s.CodecName):
			ps.Action, ps.Reason, ps.Output = Copy, "codec accepted by container", idx
			out.Options.Add(ffmpeg.Map(fmt.Sprintf("0:%d", ps.Input)), ffmpeg.PerStream("s", idx, ffmpeg.Copy("s")))
			idx++
		case bitmap || container.textSub == "":
			ps.Action, ps.Reason = Drop, "container cannot hold "+s.CodecName
		default:
			enc := o.Subtitles.Encoder
			if enc == "" {
				enc = container.textSub
			}
			ps.Action, ps.Encoder, ps.Output = Encode, enc, idx
			ps.Reason = "converted for container"
			out.Options.Add(ffmpeg.Map(fmt.Sprintf("0:%d", ps.Input)), ffmpeg.PerStream("s", idx, ffmpeg.SubtitleCodec(enc)))
			idx++
		}
		plan.Streams = append(plan.Streams, ps)
	}
	for _, s := range info.Probe.StreamsOfType(ffprobe.MediaTypeData) {
		plan.Streams = append(plan.Streams, PlannedStream{Input: s.Index.Int(), Output: -1, Type: "data", Codec: s.CodecName, Action: Drop, Reason: "data streams are not carried"})
	}
	for _, s := range info.Probe.StreamsOfType(ffprobe.MediaTypeAttachment) {
		plan.Streams = append(plan.Streams, PlannedStream{Input: s.Index.Int(), Output: -1, Type: "attachment", Codec: s.CodecName, Action: Drop, Reason: "attachments are not carried"})
	}

	if len(plan.Kept()) == 0 {
		return nil, fmt.Errorf("tasks: no streams selected for output")
	}

	if o.DropMetadata {
		out.Options.Add(ffmpeg.MapMetadata("", "-1"))
	}
	if o.DropChapters {
		out.Options.Add(ffmpeg.MapChapters(-1))
	}
	if container.faststart && !o.NoFastStart {
		out.Options.Add(ffmpeg.MovFlags("+faststart"))
	}
	out.Options = append(out.Options, o.Extra...)

	plan.Command = &ffmpeg.Command{
		Inputs:  []*ffmpeg.Input{ffmpeg.NewInput(input)},
		Outputs: []*ffmpeg.Output{out},
	}
	hw.Apply(plan.Command)
	return plan, nil
}

// ─── containers ────────────────────────────────────────────────────────────

type containerInfo struct {
	name      string
	video     []string // codec names accepted for copy
	audio     []string
	subtitles []string // subtitle codec names accepted for copy
	textSub   string   // encoder for text subtitles, "" if none
	faststart bool
}

var containers = map[string]*containerInfo{
	".mp4": {name: "mp4",
		video: []string{"h264", "hevc", "av1", "mpeg4", "vp9", "mjpeg"},
		// Opus and FLAC in MP4 are valid but poorly supported by players,
		// so they are re-encoded rather than copied.
		audio:     []string{"aac", "mp3", "ac3", "eac3", "alac", "mp2"},
		subtitles: []string{"mov_text"}, textSub: "mov_text", faststart: true},
	".mov": {name: "mov",
		video:     []string{"h264", "hevc", "prores", "mjpeg", "dnxhd", "mpeg4", "av1"},
		audio:     []string{"aac", "pcm_s16le", "pcm_s24le", "pcm_s16be", "pcm_s24be", "alac", "mp3", "ac3"},
		subtitles: []string{"mov_text"}, textSub: "mov_text", faststart: true},
	".mkv": {name: "matroska",
		video:     []string{"*"},
		audio:     []string{"*"},
		subtitles: []string{"subrip", "ass", "ssa", "webvtt", "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "text"},
		textSub:   "srt"},
	".webm": {name: "webm",
		video:     []string{"vp8", "vp9", "av1"},
		audio:     []string{"opus", "vorbis"},
		subtitles: []string{"webvtt"}, textSub: "webvtt"},
	".ts": {name: "mpegts",
		video:     []string{"h264", "hevc", "mpeg2video", "mpeg4"},
		audio:     []string{"aac", "mp3", "mp2", "ac3", "eac3"},
		subtitles: []string{"dvb_subtitle", "dvb_teletext"}},
	".m4a":  {name: "ipod", audio: []string{"aac", "alac", "mp3"}, faststart: true},
	".mp3":  {name: "mp3", audio: []string{"mp3"}},
	".flac": {name: "flac", audio: []string{"flac"}},
	".ogg":  {name: "ogg", audio: []string{"vorbis", "opus", "flac"}},
	".opus": {name: "opus", audio: []string{"opus"}},
	".wav":  {name: "wav", audio: []string{"pcm_s16le", "pcm_s24le", "pcm_s32le", "pcm_f32le", "pcm_u8"}},
}

// Audio-only containers drop video implicitly.
var audioOnly = map[string]bool{".m4a": true, ".mp3": true, ".flac": true, ".ogg": true, ".opus": true, ".wav": true}

func containerFor(output string) *containerInfo { return containers[ext(output)] }

// canCopy reports whether a stream with the given codec should be copied:
// the rule allows it and the container accepts it.
func canCopy(codec string, allowed []encode.Codec, accepted []string) bool {
	if len(allowed) == 0 {
		return false
	}
	ruleOK := false
	for _, c := range allowed {
		if c == "*" || string(c) == codec || (c == encode.PCM && strings.HasPrefix(codec, "pcm_")) {
			ruleOK = true
			break
		}
	}
	return ruleOK && contains(accepted, codec)
}

func contains(list []string, codec string) bool {
	for _, c := range list {
		if c == "*" || c == codec {
			return true
		}
	}
	return false
}

func languageMatches(lang string, wanted []string) bool {
	if len(wanted) == 0 {
		return true
	}
	if lang == "" {
		lang = "und"
	}
	for _, w := range wanted {
		if strings.EqualFold(w, lang) {
			return true
		}
	}
	return false
}

func isBitmapSubtitle(codec string) bool {
	switch codec {
	case "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "xsub":
		return true
	}
	return false
}

func encodeReason(codec string, c *containerInfo, scaled bool) string {
	switch {
	case scaled:
		return "scaled down"
	case !contains(c.video, codec) && !contains(c.audio, codec):
		return codec + " not accepted by " + c.name
	default:
		return "copy not enabled"
	}
}
