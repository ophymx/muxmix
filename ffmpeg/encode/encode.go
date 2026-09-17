// Package encode turns encoder-neutral settings into the options each
// ffmpeg encoder actually takes.
//
//	video := encode.Video{Codec: encode.H264, Quality: 22, Speed: encode.Fast}
//	audio := encode.Audio{Codec: encode.AAC, Bitrate: "160k"}
//	cmd.Output("out.mp4", video.MustOpts(set)..., audio.MustOpts(set)...)
//
// Quality is one scale for every video encoder: the x264 CRF scale, where 0
// is lossless, 23 is the default and 51 is worst. Encoders with a different
// range (SVT-AV1, VP9 and AOM at 0-63, NVENC's cq, VAAPI's qp, QSV's ICQ,
// VideoToolbox's 1-100) get an approximate mapping. Speed is a five-step
// scale mapped onto each encoder's preset or cpu-used option.
//
// Encoders are chosen from what the build has (caps.Set) and, when a
// hardware backend is requested, from hwaccel's tables. Video.HW names a
// backend outright; to pick one from what the machine can actually run,
// resolve a hwaccel.Policy against a detected hwaccel.System first and set
// Encoder from the Selection.
package encode

import (
	"fmt"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
	"github.com/ophymx/muxmix/ffmpeg/hwaccel"
)

// Codec is a codec family, independent of which encoder implements it.
type Codec string

// Video codecs.
const (
	H264   Codec = "h264"
	HEVC   Codec = "hevc"
	AV1    Codec = "av1"
	VP9    Codec = "vp9"
	VP8    Codec = "vp8"
	ProRes Codec = "prores"
	MJPEG  Codec = "mjpeg"
	PNG    Codec = "png"
	WebP   Codec = "webp"
	GIF    Codec = "gif"
)

// Audio codecs.
const (
	AAC    Codec = "aac"
	Opus   Codec = "opus"
	MP3    Codec = "mp3"
	FLAC   Codec = "flac"
	AC3    Codec = "ac3"
	EAC3   Codec = "eac3"
	Vorbis Codec = "vorbis"
	PCM    Codec = "pcm_s16le"
)

// Copy is the pseudo-codec for stream copy.
const Copy Codec = "copy"

// Speed trades encoding time for compression. Zero leaves the encoder's
// default preset.
type Speed int

const (
	DefaultSpeed Speed = iota
	Slowest            // best compression; veryslow, cpu-used 0, p7
	Slow
	Medium
	Fast
	Fastest // realtime-ish; ultrafast, cpu-used 8, p1
)

// softwareEncoders lists software encoders per codec in preference order.
// The first entry is also the choice when no caps.Set is given, so it is
// the one every common build has (the native aac encoder rather than
// libfdk_aac, for example).
var softwareEncoders = map[Codec][]string{
	H264:   {"libx264", "libopenh264"},
	HEVC:   {"libx265", "libkvazaar"},
	AV1:    {"libsvtav1", "libaom-av1", "librav1e"},
	VP9:    {"libvpx-vp9"},
	VP8:    {"libvpx"},
	ProRes: {"prores_ks", "prores"},
	MJPEG:  {"mjpeg"},
	PNG:    {"png"},
	WebP:   {"libwebp"},
	GIF:    {"gif"},
	AAC:    {"aac", "libfdk_aac"},
	Opus:   {"libopus", "opus"},
	MP3:    {"libmp3lame", "shine"},
	FLAC:   {"flac"},
	AC3:    {"ac3"},
	EAC3:   {"eac3"},
	Vorbis: {"libvorbis", "vorbis"},
	PCM:    {"pcm_s16le"},
}

// ChooseEncoder picks an encoder for codec. With a hardware kind it uses
// hwaccel's mapping and requires the build to have that encoder; otherwise
// it returns the first preferred software encoder the build has. A nil set
// skips the availability check.
func ChooseEncoder(set *caps.Set, codec Codec, hw hwaccel.Kind) (string, error) {
	if codec == Copy {
		return "copy", nil
	}
	if hw != hwaccel.None {
		name, err := hwaccel.VideoEncoder(hw, string(codec))
		if err != nil {
			return "", err
		}
		if set != nil && !set.HasEncoder(name) {
			return "", fmt.Errorf("encode: %s encoder %s is not in this ffmpeg build", hw, name)
		}
		return name, nil
	}
	candidates, ok := softwareEncoders[codec]
	if !ok {
		return "", fmt.Errorf("encode: unknown codec %q", codec)
	}
	if set == nil {
		return candidates[0], nil
	}
	for _, c := range candidates {
		if set.HasEncoder(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("encode: no %s encoder in this ffmpeg build (looked for %v)", codec, candidates)
}

// CodecOf reports the codec family an encoder name belongs to, or "".
func CodecOf(encoder string) Codec {
	for codec, names := range softwareEncoders {
		for _, n := range names {
			if n == encoder {
				return codec
			}
		}
	}
	for _, codec := range []Codec{H264, HEVC, AV1, VP9, VP8, MJPEG, ProRes} {
		for _, suffix := range []string{"_nvenc", "_vaapi", "_qsv", "_videotoolbox", "_amf", "_v4l2m2m", "_mf"} {
			if encoder == string(codec)+suffix {
				return codec
			}
		}
	}
	return ""
}

func opts(list ...ffmpeg.Opt) []ffmpeg.Opt { return list }

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func ftoa(f float64) string { return fmt.Sprintf("%g", f) }
