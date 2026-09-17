# encode

Encoder-neutral settings that render to the options each ffmpeg encoder
actually takes.

```go
video := encode.Video{Codec: encode.H264, Quality: 22, Speed: encode.Fast, PixFmt: "yuv420p"}
audio := encode.Audio{Codec: encode.AAC, Bitrate: "160k"}

cmd := ffmpeg.NewCommand().Input("in.mkv").
    Output("out.mp4", append(video.MustOpts(set), audio.MustOpts(set)...)...)
```

`set` is a `caps.Set` from `caps.Detect`; it decides which encoder
implements the codec (libx264 for H.264, libsvtav1 then libaom for AV1, and
so on) and reports an error when the build has none. Pass nil to skip the
check and take the first preference. `HW: hwaccel.CUDA` picks the hardware
encoder instead; `Encoder: "libx265"` names one outright.

## One quality scale, one speed scale

`Video.Quality` is the x264 CRF scale: lower is better, 18 is visually near-lossless,
23 is x264's default, 51 is worst. Zero means unset (each encoder's own default), so
true lossless needs `ffmpeg.CRF(0)` in `Extra`.
Each encoder gets its own knob and range:

| Encoder            | Quality maps to                  | Speed maps to                    |
|--------------------|----------------------------------|----------------------------------|
| libx264, libx265   | `-crf q`                         | `-preset veryslow … veryfast`    |
| libsvtav1          | `-crf q×63/51`                   | `-preset 3 … 12`                 |
| libaom-av1         | `-crf q×63/51`                   | `-cpu-used 1 … 8`, row-mt        |
| libvpx-vp9         | `-crf q×63/51 -b:v 0`            | `-deadline good -cpu-used 0 … 8` |
| *_nvenc            | `-rc vbr -cq q -b:v 0`           | `-preset p7 … p1`                |
| *_vaapi            | `-rc_mode CQP -qp q`             |                                  |
| *_qsv              | `-global_quality q`              | `-preset veryslow … veryfast`    |
| *_videotoolbox     | `-q:v 100-2q`                    |                                  |

Setting `Bitrate` switches to bit-rate targeting (`-b:v`, with `-maxrate`
and `-bufsize` when `MaxRate` is given), including the `-rc vbr` /
`-rc_mode VBR` that NVENC and VAAPI need. `Extra` options come last and
override anything derived.

`Audio` takes a `Bitrate` or a `VBR` level from 1 to 10 (higher is better),
mapped onto `-q:a` for MP3 and AAC, `-vbr` for FDK-AAC and a bit rate for
Opus. `Image` covers stills and animations (JPEG, PNG, WebP, animated WebP,
GIF) with a JPEG-style quality from 1 to 100.

The mappings between scales are approximate by nature; they are meant to
make "quality 22, fast" mean the same intent everywhere, not the same bits.
