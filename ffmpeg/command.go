package ffmpeg

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Command is an ffmpeg invocation: global options, inputs each with their
// own options, and outputs each with their own options and stream maps.
// Args renders it in the order ffmpeg requires.
//
//	cmd := ffmpeg.NewCommand().
//		Input("in.mkv", ffmpeg.Seek(10*time.Second)).
//		Output("out.mp4",
//			ffmpeg.Map("0:v:0"), ffmpeg.Map("0:a:m:language:eng"),
//			ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(20), ffmpeg.Preset("slow"),
//			ffmpeg.AudioCodec("aac"), ffmpeg.BitRate("a", "160k"),
//			ffmpeg.MovFlags("+faststart"))
type Command struct {
	Global  Options
	Inputs  []*Input
	Outputs []*Output
}

// Input is one -i with the options that precede it.
type Input struct {
	URL     string
	Options Options
}

// Output is one output URL with the options and -map entries that precede it.
type Output struct {
	URL     string
	Options Options
}

// NewCommand returns an empty Command. Add inputs and outputs with Input and
// Output; global options with the option constructors passed to
// GlobalOptions.
func NewCommand() *Command { return &Command{} }

// Input appends an input.
func (c *Command) Input(url string, opts ...Opt) *Command {
	c.Inputs = append(c.Inputs, NewInput(url, opts...))
	return c
}

// Output appends an output.
func (c *Command) Output(url string, opts ...Opt) *Command {
	c.Outputs = append(c.Outputs, NewOutput(url, opts...))
	return c
}

// GlobalOptions appends global options (those that precede the first input,
// such as -loglevel, -filter_complex or -init_hw_device).
func (c *Command) GlobalOptions(opts ...Opt) *Command {
	c.Global.Add(opts...)
	return c
}

// NewInput builds an Input.
func NewInput(url string, opts ...Opt) *Input {
	in := &Input{URL: url}
	in.Options.Add(opts...)
	return in
}

// NewOutput builds an Output.
func NewOutput(url string, opts ...Opt) *Output {
	out := &Output{URL: url}
	out.Options.Add(opts...)
	return out
}

// Args renders the command line, without the ffmpeg binary itself.
func (c *Command) Args() []string {
	var args []string
	args = append(args, c.Global.Args()...)
	for _, in := range c.Inputs {
		args = append(args, in.Options.Args()...)
		args = append(args, "-i", in.URL)
	}
	for _, out := range c.Outputs {
		args = append(args, out.Options.Args()...)
		args = append(args, out.URL)
	}
	return args
}

// String renders the command line shell-quoted for logging.
func (c *Command) String() string {
	parts := []string{"ffmpeg"}
	for _, a := range c.Args() {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

// ErrInvalidCommand is wrapped by every error Validate returns.
var ErrInvalidCommand = errors.New("ffmpeg: invalid command")

// globalOnly lists options ffmpeg accepts only as globals; putting them on
// an input or output silently does the wrong thing or fails.
var globalOnly = map[string]bool{
	"filter_complex": true, "lavfi": true, "filter_complex_script": true, "filter_complex_threads": true,
	"init_hw_device": true, "filter_hw_device": true,
	"loglevel": true, "v": true, "report": true, "hide_banner": true, "nostdin": true,
	"y": true, "n": true, "stats": true, "nostats": true, "stats_period": true, "progress": true,
	"benchmark": true, "benchmark_all": true, "abort_on": true, "max_error_rate": true,
	"vsync": true, "xerror": true, "copy_unknown": true, "ignore_unknown": true,
}

// inputOnly lists options that only make sense before -i.
var inputOnly = map[string]bool{
	"re": true, "readrate": true, "stream_loop": true, "itsoffset": true, "itsscale": true,
	"hwaccel": true, "hwaccel_device": true, "hwaccel_output_format": true,
	"accurate_seek": true, "seek_timestamp": true, "sseof": true, "thread_queue_size": true,
	"find_stream_info": true, "dump_attachment": true, "discard": true,
}

// outputOnly lists options that only make sense on an output.
var outputOnly = map[string]bool{
	"map": true, "map_metadata": true, "map_chapters": true, "shortest": true,
	"pass": true, "passlogfile": true, "fps_mode": true, "disposition": true,
	"filter": true, "vf": true, "af": true, "filter_script": true,
	"frames": true, "vframes": true, "aframes": true, "dframes": true,
	"metadata": true, "timestamp": true, "target": true, "attach": true,
	"movflags": true, "output_ts_offset": true,
}

// Validate reports structural problems ffmpeg would reject: no inputs or
// outputs, missing URLs, and options in a place ffmpeg does not accept
// them (a global-only option such as -filter_complex on an output, an
// input-only one such as -re on an output, or -map on an input). Every
// error wraps ErrInvalidCommand.
func (c *Command) Validate() error {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidCommand, fmt.Sprintf(format, a...))
	}
	if len(c.Inputs) == 0 {
		return fail("no inputs")
	}
	if len(c.Outputs) == 0 {
		return fail("no outputs")
	}
	for i, in := range c.Inputs {
		if in.URL == "" {
			return fail("input %d has no URL", i)
		}
		for _, a := range in.Options {
			name := baseName(a.Name)
			if globalOnly[name] {
				return fail("input %d: -%s is a global option", i, a.Name)
			}
			if outputOnly[name] {
				return fail("input %d: -%s is an output option", i, a.Name)
			}
		}
	}
	for i, out := range c.Outputs {
		if out.URL == "" {
			return fail("output %d has no URL", i)
		}
		for _, a := range out.Options {
			name := baseName(a.Name)
			if globalOnly[name] {
				return fail("output %d: -%s is a global option", i, a.Name)
			}
			if inputOnly[name] {
				return fail("output %d: -%s is an input option", i, a.Name)
			}
		}
	}
	for _, a := range c.Global {
		name := baseName(a.Name)
		if inputOnly[name] || outputOnly[name] {
			return fail("global: -%s is a per-file option", a.Name)
		}
	}
	return nil
}

// baseName strips a stream specifier: "c:v:0" -> "c".
func baseName(name string) string {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[:i]
	}
	return name
}

// ─── options ───────────────────────────────────────────────────────────────

// Arg is one command-line option: "-name value", or "-name" alone when
// HasValue is false. Name includes any stream specifier, e.g. "c:v:0".
type Arg struct {
	Name     string
	Value    string
	HasValue bool
}

// Options is an ordered list of ffmpeg options. Order matters to ffmpeg in a
// few places (for example -map order defines output stream order), so it is
// preserved exactly.
type Options []Arg

// Opt appends one or more Args to an Options list.
type Opt func(*Options)

// Add applies opts in order.
func (o *Options) Add(opts ...Opt) *Options {
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// Args renders the options as command-line arguments.
func (o Options) Args() []string {
	args := make([]string, 0, len(o)*2)
	for _, a := range o {
		args = append(args, "-"+a.Name)
		if a.HasValue {
			args = append(args, a.Value)
		}
	}
	return args
}

// Get returns the value of the last option with the given name.
func (o Options) Get(name string) (string, bool) {
	for i := len(o) - 1; i >= 0; i-- {
		if o[i].Name == name {
			return o[i].Value, true
		}
	}
	return "", false
}

// Has reports whether an option with the given name is present.
func (o Options) Has(name string) bool {
	_, ok := o.Get(name)
	return ok
}

// Set appends "-name value".
func Set(name, value string) Opt {
	return func(o *Options) { *o = append(*o, Arg{Name: name, Value: value, HasValue: true}) }
}

// Flag appends "-name" with no value.
func Flag(name string) Opt {
	return func(o *Options) { *o = append(*o, Arg{Name: name}) }
}

// Raw appends arguments verbatim. Each argument starting with "-" begins an
// option; the following argument, if it does not start with "-", is its
// value. Use it for options that have no constructor here.
func Raw(args ...string) Opt {
	return func(o *Options) {
		for i := 0; i < len(args); i++ {
			a := args[i]
			if !strings.HasPrefix(a, "-") || a == "-" {
				*o = append(*o, Arg{Name: "", Value: a, HasValue: true})
				continue
			}
			name := strings.TrimPrefix(a, "-")
			if i+1 < len(args) && !looksLikeOption(args[i+1]) {
				*o = append(*o, Arg{Name: name, Value: args[i+1], HasValue: true})
				i++
			} else {
				*o = append(*o, Arg{Name: name})
			}
		}
	}
}

func looksLikeOption(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	// Negative numbers are values, not options.
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return false
	}
	return true
}

// spec joins an option name with a stream specifier ("c" + "v:0" → "c:v:0").
func spec(name, streamSpec string) string {
	if streamSpec == "" {
		return name
	}
	return name + ":" + streamSpec
}

// ─── global options ────────────────────────────────────────────────────────

// Overwrite adds -y. The runner adds it by default unless NoOverwrite is set.
func Overwrite() Opt { return Flag("y") }

// NoOverwrite adds -n: fail instead of overwriting an existing output.
func NoOverwrite() Opt { return Flag("n") }

// LogLevel sets -loglevel (e.g. "error", "warning", "info", "verbose").
func LogLevel(level string) Opt { return Set("loglevel", level) }

// Threads sets -threads for the following input or output, or globally.
func Threads(n int) Opt { return Set("threads", strconv.Itoa(n)) }

// FilterComplex sets -filter_complex from a filtergraph string or any value
// with a String method, such as *filtergraph.FilterGraph.
func FilterComplex(graph fmt.Stringer) Opt { return Set("filter_complex", graph.String()) }

// FilterComplexString sets -filter_complex from a raw string.
func FilterComplexString(graph string) Opt { return Set("filter_complex", graph) }

// InitHWDevice adds -init_hw_device, e.g. InitHWDevice("cuda=gpu:0").
func InitHWDevice(spec string) Opt { return Set("init_hw_device", spec) }

// FilterHWDevice adds -filter_hw_device naming a device from InitHWDevice.
func FilterHWDevice(name string) Opt { return Set("filter_hw_device", name) }

// ─── input options ─────────────────────────────────────────────────────────

// Seek adds -ss: start reading (or writing) at the given position. Before an
// input it seeks the demuxer; before an output it drops earlier frames.
func Seek(d time.Duration) Opt { return Set("ss", TimeSpec(d).String()) }

// SeekEOF adds -sseof: seek relative to the end of the input.
func SeekEOF(d time.Duration) Opt { return Set("sseof", TimeSpec(d).String()) }

// Duration adds -t: limit the input read or output written to d.
func Duration(d time.Duration) Opt { return Set("t", TimeSpec(d).String()) }

// To adds -to: stop at the given absolute position.
func To(d time.Duration) Opt { return Set("to", TimeSpec(d).String()) }

// Format forces the container format (-f) for an input or output.
func Format(name string) Opt { return Set("f", name) }

// StreamLoop adds -stream_loop: loop the input n times (-1 forever).
func StreamLoop(n int) Opt { return Set("stream_loop", strconv.Itoa(n)) }

// ReadRate adds -re: read the input at its native frame rate (streaming).
func ReadRate() Opt { return Flag("re") }

// InputFrameRate sets -framerate for image sequence or raw video inputs.
func InputFrameRate(fps string) Opt { return Set("framerate", fps) }

// HWAccel adds -hwaccel (and -hwaccel_device when device is not empty) to an
// input. Backends with different device options, such as VAAPI, are handled
// by the hwaccel package.
func HWAccel(kind, device string) Opt {
	return func(o *Options) {
		o.Add(Set("hwaccel", kind))
		if device != "" {
			o.Add(Set("hwaccel_device", device))
		}
	}
}

// HWAccelOutputFormat adds -hwaccel_output_format, e.g. "cuda" to keep
// decoded frames on the GPU.
func HWAccelOutputFormat(pixFmt string) Opt { return Set("hwaccel_output_format", pixFmt) }

// ConcatDemuxer selects the concat demuxer for a concat list file and allows
// unsafe (absolute or relative) paths within it.
func ConcatDemuxer() Opt {
	return func(o *Options) { o.Add(Format("concat"), Set("safe", "0")) }
}

// Lavfi selects the lavfi virtual input device, for sources such as
// "testsrc2=size=1280x720:rate=30" or "sine=frequency=440".
func Lavfi() Opt { return Format("lavfi") }

// Probesize and AnalyzeDuration tune stream detection on an input.
func Probesize(bytes int64) Opt { return Set("probesize", strconv.FormatInt(bytes, 10)) }

// AnalyzeDuration sets -analyzeduration on an input.
func AnalyzeDuration(d time.Duration) Opt {
	return Set("analyzeduration", strconv.FormatInt(d.Microseconds(), 10))
}

// ─── stream mapping ────────────────────────────────────────────────────────

// Map adds -map with a stream specifier such as "0", "0:v:0", "1:a",
// "0:m:language:eng" or "0:s?" (optional). Maps are emitted in the order
// added, which fixes the output stream order.
func Map(streamSpec string) Opt { return Set("map", streamSpec) }

// MapLabel maps a filtergraph output label: MapLabel("vout") → -map [vout].
func MapLabel(label string) Opt { return Set("map", "["+strings.Trim(label, "[]")+"]") }

// MapExclude removes streams matched by spec from the output: -map -0:s.
func MapExclude(streamSpec string) Opt { return Set("map", "-"+streamSpec) }

// MapMetadata copies metadata: MapMetadata("", "0") copies input 0's global
// metadata to the output; MapMetadata("s:a:0", "0:s:a:1") copies stream
// metadata. Use "-1" as the source to clear it.
func MapMetadata(outSpec, inSpec string) Opt { return Set(spec("map_metadata", outSpec), inSpec) }

// MapChapters copies chapters from the given input (-1 to drop them).
func MapChapters(input int) Opt { return Set("map_chapters", strconv.Itoa(input)) }

// ─── codec options ─────────────────────────────────────────────────────────

// Codec sets -c[:spec] for the given stream specifier ("v", "a:0", ...).
func Codec(streamSpec, codec string) Opt { return Set(spec("c", streamSpec), codec) }

// VideoCodec sets -c:v.
func VideoCodec(codec string) Opt { return Codec("v", codec) }

// AudioCodec sets -c:a.
func AudioCodec(codec string) Opt { return Codec("a", codec) }

// SubtitleCodec sets -c:s.
func SubtitleCodec(codec string) Opt { return Codec("s", codec) }

// Copy stream-copies the given streams ("" for all).
func Copy(streamSpec string) Opt { return Codec(streamSpec, "copy") }

// CopyAll stream-copies every mapped stream (-c copy).
func CopyAll() Opt { return Codec("", "copy") }

// NoVideo drops every video stream (-vn).
func NoVideo() Opt { return Flag("vn") }

// NoAudio drops every audio stream (-an).
func NoAudio() Opt { return Flag("an") }

// NoSubtitles drops every subtitle stream (-sn).
func NoSubtitles() Opt { return Flag("sn") }

// NoData drops every data stream (-dn).
func NoData() Opt { return Flag("dn") }

// BitRate sets -b[:spec], e.g. BitRate("v", "5M") or BitRate("a", "160k").
func BitRate(streamSpec, rate string) Opt { return Set(spec("b", streamSpec), rate) }

// MaxRate caps the bit rate for VBV-constrained encoding (-maxrate).
func MaxRate(rate string) Opt { return Set("maxrate", rate) }

// BufSize sets the VBV buffer size that MaxRate is enforced over
// (-bufsize); twice the max rate is a common choice.
func BufSize(size string) Opt { return Set("bufsize", size) }

// MinRate sets -minrate.
func MinRate(rate string) Opt { return Set("minrate", rate) }

// CRF sets the constant rate factor for encoders that support it.
func CRF(v float64) Opt { return Set("crf", strconv.FormatFloat(v, 'f', -1, 64)) }

// QScale sets -q[:spec] (fixed quality scale), e.g. QScale("a", 2) for
// libmp3lame VBR or QScale("v", 3) for mjpeg.
func QScale(streamSpec string, q float64) Opt {
	return Set(spec("q", streamSpec), strconv.FormatFloat(q, 'f', -1, 64))
}

// Preset selects the encoder's speed/quality preset (-preset), e.g.
// "veryfast" for x264 or "p4" for NVENC.
func Preset(name string) Opt { return Set("preset", name) }

// Tune selects the encoder's content tuning (-tune), e.g. "film",
// "animation" or "zerolatency" for x264.
func Tune(name string) Opt { return Set("tune", name) }

// Profile sets the codec profile (-profile[:spec]), e.g. Profile("v",
// "high") or Profile("a", "aac_low").
func Profile(streamSpec, name string) Opt { return Set(spec("profile", streamSpec), name) }

// Level sets the codec level (-level), e.g. "4.1".
func Level(level string) Opt { return Set("level", level) }

// PixFmt sets -pix_fmt for the output video.
func PixFmt(name string) Opt { return Set("pix_fmt", name) }

// FrameRate sets the output frame rate (-r), e.g. "30000/1001" or "25".
func FrameRate(fps string) Opt { return Set("r", fps) }

// Size sets the output video size (-s).
func Size(width, height int) Opt { return Set("s", fmt.Sprintf("%dx%d", width, height)) }

// AspectRatio sets -aspect, e.g. "16:9".
func AspectRatio(ratio string) Opt { return Set("aspect", ratio) }

// Frames limits the number of frames written (-frames:v, -frames:a ...).
func Frames(streamSpec string, n int) Opt { return Set(spec("frames", streamSpec), strconv.Itoa(n)) }

// GOP sets the keyframe interval (-g).
func GOP(frames int) Opt { return Set("g", strconv.Itoa(frames)) }

// BFrames sets -bf.
func BFrames(n int) Opt { return Set("bf", strconv.Itoa(n)) }

// SampleRate sets the audio sample rate (-ar).
func SampleRate(hz int) Opt { return Set("ar", strconv.Itoa(hz)) }

// Channels sets the audio channel count (-ac).
func Channels(n int) Opt { return Set("ac", strconv.Itoa(n)) }

// ChannelLayout sets -channel_layout (or -ch_layout on newer ffmpeg; both
// are accepted by 5.1 and later).
func ChannelLayout(layout string) Opt { return Set("channel_layout", layout) }

// SampleFmt sets -sample_fmt.
func SampleFmt(name string) Opt { return Set("sample_fmt", name) }

// Pass selects the pass of a two-pass encode (-pass 1 or 2); see TwoPass
// for the orchestration.
func Pass(n int) Opt { return Set("pass", strconv.Itoa(n)) }

// PassLogFile sets the prefix of the statistics file two-pass encoders
// share between passes (-passlogfile).
func PassLogFile(prefix string) Opt { return Set("passlogfile", prefix) }

// X264Params and X265Params pass encoder-private key=value strings.
func X264Params(params string) Opt { return Set("x264-params", params) }

// X265Params passes libx265's colon-separated private options (-x265-params).
func X265Params(params string) Opt { return Set("x265-params", params) }

// ─── filters ───────────────────────────────────────────────────────────────

// Filter sets -filter[:spec] for a simple per-stream filtergraph.
func Filter(streamSpec string, graph fmt.Stringer) Opt {
	return Set(spec("filter", streamSpec), graph.String())
}

// FilterString sets -filter[:spec] from a raw string.
func FilterString(streamSpec, graph string) Opt { return Set(spec("filter", streamSpec), graph) }

// VideoFilter sets -vf.
func VideoFilter(graph string) Opt { return Set("vf", graph) }

// AudioFilter sets -af.
func AudioFilter(graph string) Opt { return Set("af", graph) }

// ─── container and metadata ────────────────────────────────────────────────

// Metadata sets global output metadata: Metadata("title", "My Film").
func Metadata(key, value string) Opt { return Set("metadata", key+"="+value) }

// StreamMetadata sets metadata on the streams matched by spec:
// StreamMetadata("s:a:0", "language", "eng").
func StreamMetadata(streamSpec, key, value string) Opt {
	return Set(spec("metadata", streamSpec), key+"="+value)
}

// Disposition sets stream dispositions: Disposition("a:0", "default"),
// Disposition("s:0", "forced"), Disposition("a:1", "0") to clear,
// Disposition("v:1", "attached_pic").
func Disposition(streamSpec, flags string) Opt { return Set(spec("disposition", streamSpec), flags) }

// MovFlags sets -movflags, e.g. "+faststart" or "frag_keyframe+empty_moov".
func MovFlags(flags string) Opt { return Set("movflags", flags) }

// Tag sets the codec tag (fourcc) for the matched streams: Tag("v", "hvc1").
func Tag(streamSpec, tag string) Opt { return Set(spec("tag", streamSpec), tag) }

// Shortest adds -shortest: finish when the shortest stream ends.
func Shortest() Opt { return Flag("shortest") }

// Timecode sets the starting timecode (-timecode) for the output.
func Timecode(tc string) Opt { return Set("timecode", tc) }

// OutputTSOffset shifts every output timestamp by d (-output_ts_offset).
func OutputTSOffset(d time.Duration) Opt { return Set("output_ts_offset", TimeSpec(d).String()) }

// StreamID sets the container stream id for a mapped output stream
// (-streamid index:id), used by MPEG-TS and IAMF.
func StreamID(index, id int) Opt { return Set("streamid", fmt.Sprintf("%d:%d", index, id)) }

// Program declares an MPEG-TS program: Program("title=Main:st=0:st=1").
func Program(spec string) Opt { return Set("program", spec) }

// StreamGroup declares a stream group (IAMF, ffmpeg 7.0+).
func StreamGroup(spec string) Opt { return Set("stream_group", spec) }

// NullOutput renders to the null muxer, useful for analysis passes:
// Output(ffmpeg.NullTarget, ffmpeg.NullOutput()).
func NullOutput() Opt { return Format("null") }

// NullTarget is the URL to use with NullOutput.
const NullTarget = "-"

// ─── helpers ───────────────────────────────────────────────────────────────

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// PerStream rebinds options to one output stream, so the same encoder
// settings can be applied to different streams: PerStream("a", 1,
// AudioCodec("aac"), BitRate("a", "128k")) renders -c:a:1 aac -b:a:1 128k.
// Options that already carry a stream type ("c:a") get the index appended;
// bare codec options ("crf", "pix_fmt", "ac") get ":<type>:<index>".
func PerStream(streamType string, index int, opts ...Opt) Opt {
	return func(o *Options) {
		var tmp Options
		tmp.Add(opts...)
		suffix := fmt.Sprintf(":%d", index)
		for _, a := range tmp {
			switch {
			case a.Name == "":
				// positional value from Raw; leave it
			case strings.Contains(a.Name, ":"):
				a.Name += suffix
			default:
				a.Name += ":" + streamType + suffix
			}
			*o = append(*o, a)
		}
	}
}
