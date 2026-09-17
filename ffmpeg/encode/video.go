package encode

import (
	"fmt"
	"math"
	"strings"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
)

// Video is an encoder-neutral video encoding request.
type Video struct {
	Codec   Codec
	Encoder string       // explicit encoder name; overrides Codec and HW
	HW      hwaccel.Kind // hardware backend to encode with, or None

	// Quality on the x264 CRF scale: lower is better, 18 is visually
	// near-lossless, 23 is x264's default, 51 is worst. Zero means unset and
	// leaves each encoder's own default. True lossless (crf 0) is not
	// reachable through Quality; put ffmpeg.CRF(0) in Extra for that.
	// Fractional values are rounded to one decimal. Ignored when Bitrate is
	// set.
	Quality float64
	// Bitrate switches to bitrate-targeted rate control ("5M", "2500k").
	Bitrate string
	// MaxRate and BufSize constrain peaks (VBV); MaxRate alone implies a
	// buffer of twice the max rate.
	MaxRate string
	BufSize string

	Speed   Speed
	Profile string // "high", "main10", ...
	Level   string // "4.1"
	Tune    string // passed through when the encoder has -tune
	PixFmt  string // "yuv420p"
	// KeyframeInterval sets -g in frames; 0 leaves the default.
	KeyframeInterval int
	// Extra options are appended last and override anything above.
	Extra []ffmpeg.Opt
}

// ResolveEncoder returns the encoder these settings will use.
func (v Video) ResolveEncoder(set *caps.Set) (string, error) {
	if v.Encoder != "" {
		if set != nil && v.Encoder != "copy" && !set.HasEncoder(v.Encoder) {
			return "", fmt.Errorf("encode: encoder %s is not in this ffmpeg build", v.Encoder)
		}
		return v.Encoder, nil
	}
	if v.Codec == "" {
		return "", fmt.Errorf("encode: Video needs a Codec or Encoder")
	}
	return ChooseEncoder(set, v.Codec, v.HW)
}

// Opts resolves the encoder and renders the output options, starting with
// -c:v. set may be nil to skip availability checks.
func (v Video) Opts(set *caps.Set) ([]ffmpeg.Opt, error) {
	enc, err := v.ResolveEncoder(set)
	if err != nil {
		return nil, err
	}
	return v.OptsFor(enc), nil
}

// MustOpts is Opts that panics on error, for literal command construction.
func (v Video) MustOpts(set *caps.Set) []ffmpeg.Opt {
	o, err := v.Opts(set)
	if err != nil {
		panic(err)
	}
	return o
}

// OptsFor renders the options for a specific encoder.
func (v Video) OptsFor(encoder string) []ffmpeg.Opt {
	out := opts(ffmpeg.VideoCodec(encoder))
	if encoder == "copy" {
		return append(out, v.Extra...)
	}
	fam := family(encoder)

	// Rate control.
	switch {
	case v.Bitrate != "":
		out = append(out, ffmpeg.BitRate("v", v.Bitrate))
		switch fam {
		case famNVENC:
			out = append(out, ffmpeg.Set("rc", "vbr"))
		case famVAAPI:
			out = append(out, ffmpeg.Set("rc_mode", "VBR"))
		}
		if v.MaxRate != "" {
			out = append(out, ffmpeg.MaxRate(v.MaxRate))
			buf := v.BufSize
			if buf == "" {
				buf = doubleRate(v.MaxRate)
			}
			out = append(out, ffmpeg.BufSize(buf))
		} else if v.BufSize != "" {
			out = append(out, ffmpeg.BufSize(v.BufSize))
		}
	case v.Quality > 0:
		q := math.Round(v.Quality*10) / 10
		switch fam {
		case famX264, famX265:
			out = append(out, ffmpeg.CRF(q))
		case famSVTAV1, famAOM:
			out = append(out, ffmpeg.CRF(scale(q, 51, 63)))
		case famVPX:
			out = append(out, ffmpeg.CRF(scale(q, 51, 63)), ffmpeg.BitRate("v", "0"))
		case famNVENC:
			out = append(out, ffmpeg.Set("rc", "vbr"), ffmpeg.Set("cq", ftoa(q)), ffmpeg.BitRate("v", "0"))
		case famVAAPI:
			out = append(out, ffmpeg.Set("rc_mode", "CQP"), ffmpeg.Set("qp", itoa(int(math.Round(q)))))
		case famQSV:
			out = append(out, ffmpeg.Set("global_quality", itoa(int(math.Round(q)))))
		case famVideoToolbox:
			out = append(out, ffmpeg.QScale("v", math.Round(100-q*2)))
		case famMJPEG:
			out = append(out, ffmpeg.QScale("v", math.Round(scale(q, 51, 29)+2)))
		case famWebP:
			out = append(out, ffmpeg.Set("quality", ftoa(math.Round(100-q*2))))
		case famRav1e:
			out = append(out, ffmpeg.Set("qp", itoa(int(math.Round(scale(q, 51, 255))))))
		}
	}

	// Speed.
	if v.Speed != DefaultSpeed {
		switch fam {
		case famX264, famX265:
			out = append(out, ffmpeg.Preset(pick(v.Speed, "veryslow", "slow", "medium", "fast", "veryfast")))
		case famSVTAV1:
			out = append(out, ffmpeg.Preset(pick(v.Speed, "3", "5", "7", "9", "12")))
		case famAOM:
			out = append(out, ffmpeg.Set("cpu-used", pick(v.Speed, "1", "2", "4", "6", "8")), ffmpeg.Set("row-mt", "1"))
		case famVPX:
			out = append(out, ffmpeg.Set("deadline", "good"), ffmpeg.Set("cpu-used", pick(v.Speed, "0", "1", "2", "4", "8")), ffmpeg.Set("row-mt", "1"))
		case famNVENC:
			out = append(out, ffmpeg.Preset(pick(v.Speed, "p7", "p6", "p4", "p2", "p1")))
		case famQSV:
			out = append(out, ffmpeg.Preset(pick(v.Speed, "veryslow", "slow", "medium", "fast", "veryfast")))
		case famRav1e:
			out = append(out, ffmpeg.Set("speed", pick(v.Speed, "2", "4", "6", "8", "10")))
		}
	}

	if v.Profile != "" {
		out = append(out, ffmpeg.Profile("v", v.Profile))
	}
	if v.Level != "" {
		out = append(out, ffmpeg.Level(v.Level))
	}
	if v.Tune != "" {
		out = append(out, ffmpeg.Tune(v.Tune))
	}
	if v.PixFmt != "" {
		out = append(out, ffmpeg.PixFmt(v.PixFmt))
	}
	if v.KeyframeInterval > 0 {
		out = append(out, ffmpeg.GOP(v.KeyframeInterval))
	}
	return append(out, v.Extra...)
}

// ─── encoder families ──────────────────────────────────────────────────────

type encoderFamily int

const (
	famUnknown encoderFamily = iota
	famX264
	famX265
	famSVTAV1
	famAOM
	famRav1e
	famVPX
	famNVENC
	famVAAPI
	famQSV
	famVideoToolbox
	famMJPEG
	famWebP
)

func family(encoder string) encoderFamily {
	switch {
	case encoder == "libx264" || encoder == "libx264rgb":
		return famX264
	case encoder == "libx265":
		return famX265
	case encoder == "libsvtav1":
		return famSVTAV1
	case encoder == "libaom-av1":
		return famAOM
	case encoder == "librav1e":
		return famRav1e
	case encoder == "libvpx-vp9" || encoder == "libvpx":
		return famVPX
	case strings.HasSuffix(encoder, "_nvenc"):
		return famNVENC
	case strings.HasSuffix(encoder, "_vaapi"):
		return famVAAPI
	case strings.HasSuffix(encoder, "_qsv"):
		return famQSV
	case strings.HasSuffix(encoder, "_videotoolbox"):
		return famVideoToolbox
	case encoder == "mjpeg":
		return famMJPEG
	case encoder == "libwebp" || encoder == "libwebp_anim":
		return famWebP
	}
	return famUnknown
}

// pick maps a Speed onto five encoder-specific values, slowest first.
func pick(s Speed, slowest, slow, medium, fast, fastest string) string {
	switch s {
	case Slowest:
		return slowest
	case Slow:
		return slow
	case Fast:
		return fast
	case Fastest:
		return fastest
	default:
		return medium
	}
}

// scale maps q on a 0..from scale onto 0..to.
func scale(q, from, to float64) float64 {
	return math.Round(q * to / from)
}

// doubleRate doubles a rate string such as "5M" or "2500k".
func doubleRate(rate string) string {
	num := strings.TrimRight(rate, "kKmMgG")
	suffix := rate[len(num):]
	var f float64
	if _, err := fmt.Sscanf(num, "%g", &f); err != nil {
		return rate
	}
	return fmt.Sprintf("%g%s", f*2, suffix)
}
