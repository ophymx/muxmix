// Package caps discovers what an ffmpeg build can do: its encoders,
// decoders, formats, filters, pixel and sample formats, hardware
// accelerators, bitstream filters and protocols, plus the option tables
// behind any of them.
//
//	set, err := caps.Detect(ctx, ffmpeg.DefaultRunner)
//	if !set.HasEncoder("libx264") { ... }
//	help, err := caps.EncoderHelp(ctx, ffmpeg.DefaultRunner, "libx264")
//	crf := help.Option("crf") // type float, range -1..FLT_MAX
//
// The parsers work on the text ffmpeg prints and are tested against the
// output of every release line from 4.4 onward.
package caps

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ophymx/muxmix/ffmpeg"
)

// MediaType is the stream type a codec handles.
type MediaType string

const (
	Video    MediaType = "video"
	Audio    MediaType = "audio"
	Subtitle MediaType = "subtitle"
	Data     MediaType = "data"
)

// Codec is one line of -encoders or -decoders.
type Codec struct {
	Name        string
	Description string
	// CodecID is the codec the implementation serves when it differs from
	// Name, e.g. "h264" for libx264; otherwise it equals Name.
	CodecID string
	Type    MediaType

	FrameThreading bool // .F....
	SliceThreading bool // ..S...
	Experimental   bool // ...X..
	DrawHorizBand  bool // ....B.
	DirectRender   bool // .....D
}

// Format is one line of -muxers, -demuxers or -formats.
type Format struct {
	Name        string
	Description string
	Demux       bool
	Mux         bool
	Device      bool
}

// Names returns the comma-separated aliases of the format name.
func (f Format) Names() []string { return strings.Split(f.Name, ",") }

// Filter is one line of -filters.
type Filter struct {
	Name        string
	Description string
	Inputs      string // "V", "AA", "N" (dynamic) or "|" (source)
	Outputs     string // likewise; "|" is a sink
	Timeline    bool   // T..
	Slice       bool   // .S.
	Command     bool   // ..C; ffmpeg 8.1 dropped this column, so it is always false there
}

// IsSource reports whether the filter has no inputs.
func (f Filter) IsSource() bool { return f.Inputs == "|" }

// IsSink reports whether the filter has no outputs.
func (f Filter) IsSink() bool { return f.Outputs == "|" }

// PixelFormat is one line of -pix_fmts.
type PixelFormat struct {
	Name         string
	Components   int
	BitsPerPixel int
	BitDepths    []int // per component; only printed by ffmpeg 7.0 and newer
	Input        bool  // I.... supported as conversion input
	Output       bool  // .O... supported as conversion output
	Hardware     bool  // ..H..
	Paletted     bool  // ...P.
	Bitstream    bool  // ....B
}

// SampleFormat is one line of -sample_fmts.
type SampleFormat struct {
	Name  string
	Depth int
}

// Planar reports whether the sample format stores channels separately.
func (s SampleFormat) Planar() bool { return strings.HasSuffix(s.Name, "p") }

// Protocols lists the URL schemes usable for input and output.
type Protocols struct {
	Input  []string
	Output []string
}

// Set is everything Detect discovers about one binary.
type Set struct {
	Version          *ffmpeg.VersionInfo
	Encoders         []Codec
	Decoders         []Codec
	Muxers           []Format
	Demuxers         []Format
	Filters          []Filter
	PixelFormats     []PixelFormat
	SampleFormats    []SampleFormat
	HWAccels         []string
	BitstreamFilters []string
	Protocols        Protocols
}

// Detect queries the binary behind runner for every listing.
func Detect(ctx context.Context, runner ffmpeg.Runner) (*Set, error) {
	if runner == nil {
		runner = ffmpeg.DefaultRunner
	}
	s := &Set{}
	var err error
	if s.Version, err = runner.Version(ctx); err != nil {
		return nil, err
	}
	list := func(flag string) (string, error) {
		res, err := runner.RunArgs(ctx, []string{"-hide_banner", flag})
		if err != nil {
			return "", fmt.Errorf("caps: ffmpeg %s: %w", flag, err)
		}
		return string(res.Stdout), nil
	}
	steps := []struct {
		flag string
		fn   func(string)
	}{
		{"-encoders", func(t string) { s.Encoders = ParseCodecs(t) }},
		{"-decoders", func(t string) { s.Decoders = ParseCodecs(t) }},
		{"-muxers", func(t string) { s.Muxers = ParseFormats(t) }},
		{"-demuxers", func(t string) { s.Demuxers = ParseFormats(t) }},
		{"-filters", func(t string) { s.Filters = ParseFilters(t) }},
		{"-pix_fmts", func(t string) { s.PixelFormats = ParsePixelFormats(t) }},
		{"-sample_fmts", func(t string) { s.SampleFormats = ParseSampleFormats(t) }},
		{"-hwaccels", func(t string) { s.HWAccels = ParseList(t) }},
		{"-bsfs", func(t string) { s.BitstreamFilters = ParseList(t) }},
		{"-protocols", func(t string) { s.Protocols = ParseProtocols(t) }},
	}
	for _, st := range steps {
		text, err := list(st.flag)
		if err != nil {
			return nil, err
		}
		st.fn(text)
	}
	return s, nil
}

// ─── lookups ───────────────────────────────────────────────────────────────

func findCodec(list []Codec, name string) *Codec {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func findFormat(list []Format, name string) *Format {
	for i := range list {
		for _, n := range list[i].Names() {
			if n == name {
				return &list[i]
			}
		}
	}
	return nil
}

// Encoder returns the encoder with the given name, or nil.
func (s *Set) Encoder(name string) *Codec { return findCodec(s.Encoders, name) }

// Decoder returns the decoder with the given name, or nil.
func (s *Set) Decoder(name string) *Codec { return findCodec(s.Decoders, name) }

// Muxer returns the muxer matching name or one of its aliases, or nil.
func (s *Set) Muxer(name string) *Format { return findFormat(s.Muxers, name) }

// Demuxer returns the demuxer matching name or one of its aliases, or nil.
func (s *Set) Demuxer(name string) *Format { return findFormat(s.Demuxers, name) }

// Filter returns the filter with the given name, or nil.
func (s *Set) Filter(name string) *Filter {
	for i := range s.Filters {
		if s.Filters[i].Name == name {
			return &s.Filters[i]
		}
	}
	return nil
}

// PixelFormat returns the pixel format with the given name, or nil.
func (s *Set) PixelFormat(name string) *PixelFormat {
	for i := range s.PixelFormats {
		if s.PixelFormats[i].Name == name {
			return &s.PixelFormats[i]
		}
	}
	return nil
}

// HasEncoder, HasDecoder, HasMuxer, HasDemuxer, HasFilter, HasHWAccel,
// HasBitstreamFilter and HasProtocol answer yes/no questions.
func (s *Set) HasEncoder(name string) bool { return s.Encoder(name) != nil }
func (s *Set) HasDecoder(name string) bool { return s.Decoder(name) != nil }
func (s *Set) HasMuxer(name string) bool   { return s.Muxer(name) != nil }
func (s *Set) HasDemuxer(name string) bool { return s.Demuxer(name) != nil }
func (s *Set) HasFilter(name string) bool  { return s.Filter(name) != nil }
func (s *Set) HasHWAccel(name string) bool { return contains(s.HWAccels, name) }
func (s *Set) HasBitstreamFilter(name string) bool {
	return contains(s.BitstreamFilters, name)
}
func (s *Set) HasProtocol(name string, output bool) bool {
	if output {
		return contains(s.Protocols.Output, name)
	}
	return contains(s.Protocols.Input, name)
}

// EncodersFor returns the encoders that produce the given codec, e.g.
// EncodersFor("h264") → libx264, h264_nvenc, h264_vaapi, ...
func (s *Set) EncodersFor(codecID string) []Codec {
	var out []Codec
	for _, c := range s.Encoders {
		if c.CodecID == codecID {
			out = append(out, c)
		}
	}
	return out
}

// DecodersFor returns the decoders for the given codec.
func (s *Set) DecodersFor(codecID string) []Codec {
	var out []Codec
	for _, c := range s.Decoders {
		if c.CodecID == codecID {
			out = append(out, c)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ─── parsers ───────────────────────────────────────────────────────────────

var (
	codecLine = regexp.MustCompile(`^ ([VAS])([F.])([S.])([X.])([B.])([D.]) (\S+)\s*(.*)$`)
	codecIDRe = regexp.MustCompile(`\(codec (\S+)\)\s*$`)
	// The device column of -formats appeared in 7.0; the command-support
	// column of -filters was dropped in 8.1. Both are optional here.
	formatLine = regexp.MustCompile(`^ ([D ])([E ])([d ])? (\S+)\s*(.*)$`)
	filterLine = regexp.MustCompile(`^ ([T.])([S.])([C.])? (\S+)\s+(\S+)->(\S+)\s*(.*)$`)
	pixFmtLine = regexp.MustCompile(`^([I.])([O.])([H.])([P.])([B.]) (\S+)\s+(\d+)\s+(\d+)(?:\s+(\S+))?\s*$`)
	sampleLine = regexp.MustCompile(`^(\S+)\s+(\d+)\s*$`)
)

// ParseCodecs parses -encoders or -decoders output.
func ParseCodecs(text string) []Codec {
	var out []Codec
	for _, line := range strings.Split(text, "\n") {
		m := codecLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		c := Codec{
			Name:           m[7],
			Description:    strings.TrimSpace(m[8]),
			FrameThreading: m[2] == "F",
			SliceThreading: m[3] == "S",
			Experimental:   m[4] == "X",
			DrawHorizBand:  m[5] == "B",
			DirectRender:   m[6] == "D",
		}
		switch m[1] {
		case "V":
			c.Type = Video
		case "A":
			c.Type = Audio
		case "S":
			c.Type = Subtitle
		}
		c.CodecID = c.Name
		if id := codecIDRe.FindStringSubmatch(c.Description); id != nil {
			c.CodecID = id[1]
			c.Description = strings.TrimSpace(strings.TrimSuffix(c.Description, id[0]))
		}
		out = append(out, c)
	}
	return out
}

// ParseFormats parses -formats, -muxers or -demuxers output.
func ParseFormats(text string) []Format {
	var out []Format
	for _, line := range strings.Split(text, "\n") {
		m := formatLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil || (m[1] == " " && m[2] == " ") {
			continue
		}
		out = append(out, Format{
			Name:        m[4],
			Description: strings.TrimSpace(m[5]),
			Demux:       m[1] == "D",
			Mux:         m[2] == "E",
			Device:      m[3] == "d",
		})
	}
	return out
}

// ParseFilters parses -filters output.
func ParseFilters(text string) []Filter {
	var out []Filter
	for _, line := range strings.Split(text, "\n") {
		m := filterLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		out = append(out, Filter{
			Name:        m[4],
			Inputs:      m[5],
			Outputs:     m[6],
			Description: strings.TrimSpace(m[7]),
			Timeline:    m[1] == "T",
			Slice:       m[2] == "S",
			Command:     m[3] == "C",
		})
	}
	return out
}

// ParsePixelFormats parses -pix_fmts output.
func ParsePixelFormats(text string) []PixelFormat {
	var out []PixelFormat
	for _, line := range strings.Split(text, "\n") {
		m := pixFmtLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		p := PixelFormat{
			Name:      m[6],
			Input:     m[1] == "I",
			Output:    m[2] == "O",
			Hardware:  m[3] == "H",
			Paletted:  m[4] == "P",
			Bitstream: m[5] == "B",
		}
		p.Components, _ = strconv.Atoi(m[7])
		p.BitsPerPixel, _ = strconv.Atoi(m[8])
		if m[9] != "" {
			for _, d := range strings.Split(m[9], "-") {
				n, err := strconv.Atoi(d)
				if err != nil {
					p.BitDepths = nil
					break
				}
				p.BitDepths = append(p.BitDepths, n)
			}
		}
		out = append(out, p)
	}
	return out
}

// ParseSampleFormats parses -sample_fmts output.
func ParseSampleFormats(text string) []SampleFormat {
	var out []SampleFormat
	for _, line := range strings.Split(text, "\n") {
		m := sampleLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil || m[1] == "name" {
			continue
		}
		depth, _ := strconv.Atoi(m[2])
		out = append(out, SampleFormat{Name: m[1], Depth: depth})
	}
	return out
}

// ParseList parses the one-name-per-line listings (-hwaccels, -bsfs),
// skipping the header line that ends in a colon.
func ParseList(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") {
			continue
		}
		out = append(out, line)
	}
	return out
}

// ParseProtocols parses -protocols output.
func ParseProtocols(text string) Protocols {
	var p Protocols
	var section *[]string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case line == "Input:":
			section = &p.Input
		case line == "Output:":
			section = &p.Output
		case strings.HasSuffix(line, ":"):
			section = nil
		case section != nil:
			*section = append(*section, line)
		}
	}
	return p
}
