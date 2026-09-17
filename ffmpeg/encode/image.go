package encode

import (
	"math"
	"path/filepath"
	"strings"

	"github.com/ophymx/muxmix/ffmpeg"
)

// ImageFormat is a still or animated image encoding.
type ImageFormat string

const (
	JPEG         ImageFormat = "jpeg"
	PNGImage     ImageFormat = "png"
	WebPImage    ImageFormat = "webp"
	AnimatedWebP ImageFormat = "webp_anim"
	AnimatedGIF  ImageFormat = "gif"
)

// Image is an image encoding request with a JPEG-style quality: 1 to 100,
// higher is better. Zero leaves the encoder default.
type Image struct {
	Format  ImageFormat
	Quality int
	// Lossless applies to WebP.
	Lossless bool
	Extra    []ffmpeg.Opt
}

// ImageFormatFor guesses the format from a file name's extension.
func ImageFormatFor(path string) ImageFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return JPEG
	case ".png":
		return PNGImage
	case ".webp":
		return WebPImage
	case ".gif":
		return AnimatedGIF
	}
	return ""
}

// Opts renders the output options for the image encoder.
func (im Image) Opts() []ffmpeg.Opt {
	var out []ffmpeg.Opt
	switch im.Format {
	case JPEG:
		out = append(out, ffmpeg.VideoCodec("mjpeg"))
		if im.Quality > 0 {
			// mjpeg -q:v runs 2 (best) to 31 (worst).
			q := math.Round(2 + (100-float64(clamp(im.Quality, 1, 100)))*29/99)
			out = append(out, ffmpeg.QScale("v", q))
		}
		// Full-range JPEG the way browsers expect.
		out = append(out, ffmpeg.PixFmt("yuvj420p"))
	case PNGImage:
		out = append(out, ffmpeg.VideoCodec("png"))
	case WebPImage, AnimatedWebP:
		if im.Format == AnimatedWebP {
			out = append(out, ffmpeg.VideoCodec("libwebp_anim"))
		} else {
			out = append(out, ffmpeg.VideoCodec("libwebp"))
		}
		if im.Lossless {
			out = append(out, ffmpeg.Set("lossless", "1"))
		} else if im.Quality > 0 {
			out = append(out, ffmpeg.Set("quality", itoa(clamp(im.Quality, 1, 100))))
		}
	case AnimatedGIF:
		out = append(out, ffmpeg.VideoCodec("gif"))
	}
	return append(out, im.Extra...)
}
