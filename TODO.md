# TODO

## 1. ffprobe: helper layer over generated types (done)

Replaced by the lenient value types (`Int`, `Seconds`, `Rat`, `Bool`, `Tags`), the helpers
in `ffprobe/helpers.go`, the typed side data in `ffprobe/sidedata.go`, and
`Prober.Sections` for runtime feature detection.

## 2. hwaccel: consolidated arg builder (done)

`hwaccel.InputOpt(kind, device)` and `hwaccel.EncodeOpt(kind, codec, filters...)` return
`ffmpeg.Opt` values that plug straight into `ffmpeg.Command`; `BuildEncodeArgs` remains for
raw argument lists.

## 3. ffmpeg: higher-level codec builder (done)

`ffmpeg/encode` provides encoder-neutral `Video`, `Audio` and `Image` settings with one
quality scale and one speed scale mapped onto each encoder's own options, encoder selection
from `caps` and `hwaccel`, and `tasks` builds on it for thumbnails, trickplay, previews and
waveforms; `tasks.Transcode` does probe-driven stream mapping. `tasks.Package` writes HLS, DASH
or CMAF ladders; `ffmpeg.TwoPass` orchestrates two-pass encodes.

## 4. hwaccel: marshal/unmarshal SystemSupport for caching (done)

`hwaccel.SaveCache`, `LoadCache` and `DetectSystemCached` store the detection keyed by the
ffmpeg version string; a binary upgrade invalidates it.

## 5. hwaccel: registerable Kind interface (done)

`hwaccel.Backend` plus `Register` and `RegisterAlias`; the built-in VAAPI, CUDA, QSV and
VideoToolbox backends are the reference implementations in `builtin.go`.

## 6. ffmpeg: Command.Check (done)

`caps.Check` validates encoders, decoders, muxers, demuxers, filters, pixel formats,
hardware devices and encoder option values against the installed build before running.
