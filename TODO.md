# TODO

## 1. ffprobe: helper layer over generated types (done)

Replaced by the lenient value types (`Int`, `Seconds`, `Rat`, `Bool`, `Tags`), the helpers
in `ffprobe/helpers.go`, the typed side data in `ffprobe/sidedata.go`, and
`Prober.Sections` for runtime feature detection.

## 2. hwaccel: consolidated arg builder (done)

`hwaccel.Selection` renders the hardware pieces of a command: `Apply` adds the device
initialisation, `Filter` wraps filters in the upload chain, `Opts` puts filter and codec on
an output stream.

## 3. ffmpeg: higher-level codec builder (done)

`ffmpeg/encode` provides encoder-neutral `Video`, `Audio` and `Image` settings with one
quality scale and one speed scale mapped onto each encoder's own options, encoder selection
from `caps` and `hwaccel`, and `tasks` builds on it for thumbnails, trickplay, previews and
waveforms; `tasks.Transcode` does probe-driven stream mapping. `tasks.Package` writes HLS, DASH
or CMAF ladders; `ffmpeg.TwoPass` orchestrates two-pass encodes.

## 4. hwaccel: marshal/unmarshal SystemSupport for caching (done)

`hwaccel.System` bundles the build's `caps.Set` with the runtime probes; `DetectCached`
stores it as JSON and re-detects when the ffmpeg version, the file's age or the device
nodes change. `tasks.Tools.System` holds it for every job, and `hwaccel.Policy` says per
job whether to prefer or require hardware.

## 5. hwaccel: registerable Kind interface (done)

`hwaccel.Backend` plus `Register` and `RegisterAlias`; the built-in VAAPI, CUDA, QSV and
VideoToolbox backends are the reference implementations in `builtin.go`.

## 6. ffmpeg: Command.Check (done)

`caps.Check` validates encoders, decoders, muxers, demuxers, filters, pixel formats,
hardware devices and encoder option values against the installed build before running.

## 7. Release checklist

- Run `matrix/test.sh` (all eight images) and CI green.
- API review of exported identifiers, godoc `Example` functions, then tag v0.1.
