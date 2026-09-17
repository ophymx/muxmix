package caps

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ophymx/muxmix/ffmpeg"
)

// Help is the parsed output of ffmpeg -h <kind>=<name>: what a component
// supports and the options it accepts.
type Help struct {
	Kind        string // "encoder", "decoder", "muxer", "demuxer", "filter" or "bsf"
	Name        string
	Description string

	// Codecs (encoders and decoders).
	Capabilities    []string // "dr1", "delay", "threads", ...
	Threading       string   // "frame and slice", "other", ...
	PixelFormats    []string
	SampleFormats   []string
	SampleRates     []int
	ChannelLayouts  []string
	FrameRates      []string
	HardwareDevices []string

	// Formats (muxers and demuxers).
	Extensions    []string
	MimeType      string
	DefaultCodecs map[MediaType]string

	// Bitstream filters.
	Codecs []string

	// Filters.
	Inputs         []Pad
	Outputs        []Pad
	DynamicInputs  bool
	DynamicOutputs bool

	// Options, grouped under each "<name> AVOptions:" heading.
	OptionGroups []OptionGroup
}

// Pad is one filter input or output.
type Pad struct {
	Name string
	Type MediaType
}

// OptionGroup is one AVOptions table.
type OptionGroup struct {
	Name    string
	Options []Option
}

// Option is one AVOption line.
type Option struct {
	Name string
	Type string // "int", "int64", "float", "double", "string", "boolean", "flags", "rational", "binary", "duration", "color", "image_size", "video_rate", "pix_fmt", "sample_fmt", "channel_layout", "dictionary"
	// Flags is the raw capability column, e.g. "E..V.......": E encoding,
	// D decoding, F filtering, V video, A audio, S subtitle, X export,
	// R readonly, B bsf, T runtime, P deprecated.
	Flags     string
	Help      string
	Default   string
	Min, Max  string
	Constants []Constant
}

// Constant is a named value an option accepts.
type Constant struct {
	Name  string
	Value string
	Help  string
}

// Encoding, Decoding, Filtering, VideoOption, AudioOption, Runtime and
// Deprecated read the option's flag column.
func (o Option) Encoding() bool    { return strings.ContainsRune(o.Flags, 'E') }
func (o Option) Decoding() bool    { return strings.ContainsRune(o.Flags, 'D') }
func (o Option) Filtering() bool   { return strings.ContainsRune(o.Flags, 'F') }
func (o Option) VideoOption() bool { return strings.ContainsRune(o.Flags, 'V') }
func (o Option) AudioOption() bool { return strings.ContainsRune(o.Flags, 'A') }
func (o Option) Runtime() bool     { return strings.ContainsRune(o.Flags, 'T') }
func (o Option) Deprecated() bool  { return strings.ContainsRune(o.Flags, 'P') }

// Constant returns the constant with the given name, or nil.
func (o *Option) Constant(name string) *Constant {
	for i := range o.Constants {
		if o.Constants[i].Name == name {
			return &o.Constants[i]
		}
	}
	return nil
}

// Options returns every option across all groups.
func (h *Help) Options() []Option {
	var out []Option
	for _, g := range h.OptionGroups {
		out = append(out, g.Options...)
	}
	return out
}

// Option returns the option with the given name, or nil.
func (h *Help) Option(name string) *Option {
	for gi := range h.OptionGroups {
		for oi := range h.OptionGroups[gi].Options {
			if h.OptionGroups[gi].Options[oi].Name == name {
				return &h.OptionGroups[gi].Options[oi]
			}
		}
	}
	return nil
}

// SupportsPixelFormat reports whether an encoder accepts the pixel format.
func (h *Help) SupportsPixelFormat(name string) bool { return contains(h.PixelFormats, name) }

// ─── querying ──────────────────────────────────────────────────────────────

// HelpFor runs ffmpeg -h kind=name. kind is one of encoder, decoder, muxer,
// demuxer, filter, bsf.
func HelpFor(ctx context.Context, runner ffmpeg.Runner, kind, name string) (*Help, error) {
	if runner == nil {
		runner = ffmpeg.DefaultRunner
	}
	res, err := runner.RunArgs(ctx, []string{"-hide_banner", "-h", kind + "=" + name})
	if err != nil {
		return nil, fmt.Errorf("caps: ffmpeg -h %s=%s: %w", kind, name, err)
	}
	h := ParseHelp(string(res.Stdout))
	if h.Name == "" {
		return nil, fmt.Errorf("caps: ffmpeg does not know %s %q", kind, name)
	}
	return h, nil
}

// EncoderHelp, DecoderHelp, MuxerHelp, DemuxerHelp, FilterHelp and
// BitstreamFilterHelp are HelpFor with the kind fixed.
func EncoderHelp(ctx context.Context, r ffmpeg.Runner, name string) (*Help, error) {
	return HelpFor(ctx, r, "encoder", name)
}

// DecoderHelp is HelpFor a decoder.
func DecoderHelp(ctx context.Context, r ffmpeg.Runner, name string) (*Help, error) {
	return HelpFor(ctx, r, "decoder", name)
}

// MuxerHelp is HelpFor a muxer.
func MuxerHelp(ctx context.Context, r ffmpeg.Runner, name string) (*Help, error) {
	return HelpFor(ctx, r, "muxer", name)
}

// DemuxerHelp is HelpFor a demuxer.
func DemuxerHelp(ctx context.Context, r ffmpeg.Runner, name string) (*Help, error) {
	return HelpFor(ctx, r, "demuxer", name)
}

// FilterHelp is HelpFor a filter.
func FilterHelp(ctx context.Context, r ffmpeg.Runner, name string) (*Help, error) {
	return HelpFor(ctx, r, "filter", name)
}

// BitstreamFilterHelp is HelpFor a bitstream filter.
func BitstreamFilterHelp(ctx context.Context, r ffmpeg.Runner, name string) (*Help, error) {
	return HelpFor(ctx, r, "bsf", name)
}

// ─── parsing ───────────────────────────────────────────────────────────────

var (
	helpHeader    = regexp.MustCompile(`^(Encoder|Decoder|Muxer|Demuxer|Filter|Bit stream filter) (\S+)(?: \[(.*)\])?:?\s*$`)
	optionHeader  = regexp.MustCompile(`^(.*) AVOptions:\s*$`)
	optionLine    = regexp.MustCompile(`^\s{1,3}-?(\S+)\s+<(\w+)>\s+([EDFVASXRBTP.]+)\s*(.*)$`)
	constantLine  = regexp.MustCompile(`^\s{4,}(\S+)\s+(\S+)\s+([EDFVASXRBTP.]+)\s*(.*)$`)
	rangeSuffix   = regexp.MustCompile(`\s*\(from (\S+) to (\S+)\)`)
	defaultSuffix = regexp.MustCompile(`\s*\(default (.*)\)\s*$`)
	padLine       = regexp.MustCompile(`^\s*#\d+: (\S+) \((\w+)\)\s*$`)
)

// ParseHelp parses ffmpeg -h <kind>=<name> output. An unknown component
// yields a Help with an empty Name.
func ParseHelp(text string) *Help {
	h := &Help{DefaultCodecs: map[MediaType]string{}}
	var group *OptionGroup
	var lastOption *Option
	padSection := ""
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if h.Name == "" {
			m := helpHeader.FindStringSubmatch(trimmed)
			if m == nil {
				continue
			}
			h.Kind = kindOf(m[1])
			h.Name = m[2]
			h.Description = m[3]
			continue
		}

		if m := optionHeader.FindStringSubmatch(trimmed); m != nil {
			h.OptionGroups = append(h.OptionGroups, OptionGroup{Name: m[1]})
			group = &h.OptionGroups[len(h.OptionGroups)-1]
			lastOption = nil
			padSection = ""
			continue
		}

		if group != nil {
			if m := optionLine.FindStringSubmatch(line); m != nil {
				opt := parseOption(m)
				group.Options = append(group.Options, opt)
				lastOption = &group.Options[len(group.Options)-1]
				continue
			}
			if m := constantLine.FindStringSubmatch(line); m != nil && lastOption != nil {
				lastOption.Constants = append(lastOption.Constants, Constant{Name: m[1], Value: m[2], Help: strings.TrimSpace(m[4])})
				continue
			}
			continue
		}

		// Header block: "Key: value" lines and filter pad sections.
		switch {
		case trimmed == "Inputs:":
			padSection = "in"
			continue
		case trimmed == "Outputs:":
			padSection = "out"
			continue
		case padSection != "":
			if m := padLine.FindStringSubmatch(line); m != nil {
				pad := Pad{Name: m[1], Type: MediaType(m[2])}
				if padSection == "in" {
					h.Inputs = append(h.Inputs, pad)
				} else {
					h.Outputs = append(h.Outputs, pad)
				}
				continue
			}
			if strings.HasPrefix(trimmed, "dynamic") || strings.HasPrefix(trimmed, "none") {
				if strings.HasPrefix(trimmed, "dynamic") {
					if padSection == "in" {
						h.DynamicInputs = true
					} else {
						h.DynamicOutputs = true
					}
				}
				continue
			}
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			// Filters print a bare description line right after the header.
			if h.Kind == "filter" && h.Description == "" && i > 0 {
				h.Description = trimmed
			}
			continue
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "."))
		switch key {
		case "General capabilities":
			h.Capabilities = strings.Fields(value)
		case "Threading capabilities":
			h.Threading = value
		case "Supported pixel formats":
			h.PixelFormats = strings.Fields(value)
		case "Supported sample formats":
			h.SampleFormats = strings.Fields(value)
		case "Supported sample rates":
			for _, s := range strings.Fields(value) {
				if n, err := strconv.Atoi(s); err == nil {
					h.SampleRates = append(h.SampleRates, n)
				}
			}
		case "Supported channel layouts":
			h.ChannelLayouts = strings.Fields(value)
		case "Supported framerates":
			h.FrameRates = strings.Fields(value)
		case "Supported hardware devices":
			h.HardwareDevices = strings.Fields(value)
		case "Common extensions":
			h.Extensions = strings.Split(value, ",")
		case "Mime type":
			h.MimeType = value
		case "Default video codec":
			h.DefaultCodecs[Video] = value
		case "Default audio codec":
			h.DefaultCodecs[Audio] = value
		case "Default subtitle codec":
			h.DefaultCodecs[Subtitle] = value
		case "Supported codecs":
			h.Codecs = strings.Fields(value)
		case "Description":
			h.Description = value
		}
	}
	if len(h.DefaultCodecs) == 0 {
		h.DefaultCodecs = nil
	}
	return h
}

func kindOf(header string) string {
	switch header {
	case "Bit stream filter":
		return "bsf"
	default:
		return strings.ToLower(header)
	}
}

func parseOption(m []string) Option {
	opt := Option{Name: m[1], Type: m[2], Flags: m[3]}
	help := strings.TrimSpace(m[4])
	if d := defaultSuffix.FindStringSubmatch(help); d != nil {
		opt.Default = strings.Trim(d[1], `"`)
		help = strings.TrimSpace(strings.TrimSuffix(help, d[0]))
	}
	if r := rangeSuffix.FindStringSubmatch(help); r != nil {
		opt.Min, opt.Max = r[1], r[2]
		help = strings.TrimSpace(strings.Replace(help, r[0], "", 1))
	}
	opt.Help = help
	return opt
}
