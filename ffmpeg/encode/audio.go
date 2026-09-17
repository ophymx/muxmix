package encode

import (
	"fmt"
	"math"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
)

// Audio is an encoder-neutral audio encoding request.
type Audio struct {
	Codec   Codec
	Encoder string // explicit encoder name; overrides Codec

	// Bitrate targets a bit rate ("160k"). When empty and VBR is set,
	// quality-based VBR is used where the encoder supports it.
	Bitrate string
	// VBR is a quality level from 1 (smallest) to 10 (best) for encoders
	// with quality modes: MP3, AAC, FDK-AAC and Vorbis. Zero leaves the
	// encoder default.
	VBR int

	Channels   int // -ac
	SampleRate int // -ar
	Extra      []ffmpeg.Opt
}

// ResolveEncoder returns the encoder these settings will use.
func (a Audio) ResolveEncoder(set *caps.Set) (string, error) {
	if a.Encoder != "" {
		if set != nil && a.Encoder != "copy" && !set.HasEncoder(a.Encoder) {
			return "", fmt.Errorf("encode: encoder %s is not in this ffmpeg build", a.Encoder)
		}
		return a.Encoder, nil
	}
	if a.Codec == "" {
		return "", fmt.Errorf("encode: Audio needs a Codec or Encoder")
	}
	return ChooseEncoder(set, a.Codec, hwaccel.None)
}

// Opts resolves the encoder and renders the output options, starting with
// -c:a.
func (a Audio) Opts(set *caps.Set) ([]ffmpeg.Opt, error) {
	enc, err := a.ResolveEncoder(set)
	if err != nil {
		return nil, err
	}
	return a.OptsFor(enc), nil
}

// MustOpts is Opts that panics on error.
func (a Audio) MustOpts(set *caps.Set) []ffmpeg.Opt {
	o, err := a.Opts(set)
	if err != nil {
		panic(err)
	}
	return o
}

// OptsFor renders the options for a specific encoder.
func (a Audio) OptsFor(encoder string) []ffmpeg.Opt {
	out := opts(ffmpeg.AudioCodec(encoder))
	if encoder == "copy" {
		return append(out, a.Extra...)
	}
	switch {
	case a.Bitrate != "":
		out = append(out, ffmpeg.BitRate("a", a.Bitrate))
	case a.VBR > 0:
		vbr := float64(clamp(a.VBR, 1, 10))
		switch encoder {
		case "libmp3lame":
			out = append(out, ffmpeg.QScale("a", math.Round(9-(vbr-1)*9/9)))
		case "aac":
			out = append(out, ffmpeg.QScale("a", math.Round((0.1+(vbr-1)*1.9/9)*100)/100))
		case "libfdk_aac":
			out = append(out, ffmpeg.Set("vbr", itoa(int(math.Round(1+(vbr-1)*4/9)))))
		case "libvorbis", "vorbis":
			out = append(out, ffmpeg.QScale("a", vbr))
		case "libopus":
			// Opus is always VBR; pick a bit rate from the level for stereo.
			out = append(out, ffmpeg.BitRate("a", fmt.Sprintf("%dk", 32+int(vbr-1)*16)))
		}
	}
	if a.Channels > 0 {
		out = append(out, ffmpeg.Channels(a.Channels))
	}
	if a.SampleRate > 0 {
		out = append(out, ffmpeg.SampleRate(a.SampleRate))
	}
	return append(out, a.Extra...)
}

func clamp(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
