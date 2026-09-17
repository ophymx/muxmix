package hwaccel

import (
	"strings"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

// Selection is a resolved way to encode one codec: which backend, on
// which device, with which encoder. The zero value, or any value whose
// Kind is None, means software; its Encoder is then empty and the caller
// picks one.
//
// A hardware Selection renders its own ffmpeg pieces: GlobalOpts
// initialises the device, Filter wraps the caller's filters in the upload
// chain, and Opts puts the filter and codec on an output stream.
type Selection struct {
	Kind    Kind   `json:"kind"`
	Device  string `json:"device,omitempty"`
	Encoder string `json:"encoder,omitempty"`
	Codec   string `json:"codec"`
}

// Hardware reports whether the selection uses a hardware backend.
func (s Selection) Hardware() bool { return s.Kind != None }

// String reads "h264_vaapi on /dev/dri/renderD128", "hevc_nvenc (cuda)"
// or "software".
func (s Selection) String() string {
	if !s.Hardware() {
		return "software"
	}
	if s.Device != "" {
		return s.Encoder + " on " + s.Device
	}
	return s.Encoder + " (" + string(s.Kind) + ")"
}

// GlobalOpts returns the global options that initialise the device, for
// Command.GlobalOptions. Software selections return a no-op.
func (s Selection) GlobalOpts() baseffmpeg.Opt {
	b, ok := Lookup(s.Kind)
	if !ok {
		return noOpt
	}
	args, err := b.DeviceArgs(s.Device)
	if err != nil || len(args) == 0 {
		return noOpt
	}
	return baseffmpeg.Raw(args...)
}

// Filter returns the filter chain that runs the given filters and then
// moves frames to the device: "scale=1280:-2,format=nv12,hwupload" for
// VAAPI. Empty filters are dropped. Software selections return the
// filters joined.
func (s Selection) Filter(filters ...string) string {
	b, ok := Lookup(s.Kind)
	if !ok {
		return joinFilters(filters)
	}
	chain, err := b.Filter(filters...)
	if err != nil {
		return joinFilters(filters)
	}
	return chain
}

// Opts returns the output options for one video stream: -filter:v with
// the upload chain when there are filters or the backend needs one, then
// -c:v. streamSpec is "v" or "v:0". Software selections return only the
// filters, with no codec.
func (s Selection) Opts(streamSpec string, filters ...string) []baseffmpeg.Opt {
	var out []baseffmpeg.Opt
	if chain := s.Filter(filters...); chain != "" {
		out = append(out, baseffmpeg.FilterString(streamSpec, chain))
	}
	if s.Hardware() {
		out = append(out, baseffmpeg.VideoCodec(s.Encoder))
	}
	return out
}

// Apply adds the device initialisation to cmd. It is safe to call for a
// software selection.
func (s Selection) Apply(cmd *baseffmpeg.Command) {
	if s.Hardware() {
		cmd.Global.Add(s.GlobalOpts())
	}
}

func noOpt(*baseffmpeg.Options) {}

// VideoEncoder maps a codec name ("h264") to the backend's encoder name
// ("h264_vaapi"). None returns the codec name unchanged, so callers can
// pass it to a software encoder table.
func VideoEncoder(kind Kind, codec string) (string, error) {
	if kind == None {
		return normalizeCodec(codec), nil
	}
	b, err := lookupOrErr(kind)
	if err != nil {
		return "", err
	}
	return b.VideoCodec(codec)
}

func joinFilters(extra []string, tail ...string) string {
	parts := make([]string, 0, len(extra)+len(tail))
	for _, f := range extra {
		if f = strings.TrimSpace(f); f != "" {
			parts = append(parts, f)
		}
	}
	return strings.Join(append(parts, tail...), ",")
}
