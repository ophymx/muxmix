# TODO

## 1. ffprobe: helper layer over generated types (done)

Replaced by the lenient value types (`Int`, `Seconds`, `Rat`, `Bool`, `Tags`), the helpers
in `ffprobe/helpers.go`, the typed side data in `ffprobe/sidedata.go`, and
`Prober.Sections` for runtime feature detection.

## 2. hwaccel: consolidated arg builder (done)

`hwaccel.InputOpt(kind, device)` and `hwaccel.EncodeOpt(kind, codec, filters...)` return
`ffmpeg.Opt` values that plug straight into `ffmpeg.Command`; `BuildEncodeArgs` remains for
raw argument lists.

## 3. ffmpeg: higher-level codec builder (in progress)

`ffmpeg/analyze` covers the analysis side (loudnorm two-pass, silence/black/freeze/scene
detection, cropdetect, idet, ebur128, volumedetect, astats).

`ffmpeg.Command` now models inputs, outputs, maps and options, and the option constructors
cover the common encoder knobs; `ffmpeg/caps` reports what the installed build supports and
the option tables of each component. What is still missing is the codec-specific layer above
them: per-encoder presets and rate-control profiles, two-pass orchestration, and a
`Command.Check(set)` that validates encoders, formats and option values against `caps`.

Transcoder commands outside of this repository are currently being used to experiment with what
codec-specific builder APIs should look like before anything is added here. Once patterns
stabilize there, extract them into the `ffmpeg` package.

## 4. hwaccel: marshal/unmarshal SystemSupport for caching (done)

`hwaccel.SaveCache`, `LoadCache` and `DetectSystemCached` store the detection keyed by the
ffmpeg version string; a binary upgrade invalidates it.

## 5. hwaccel: registerable Kind interface (done)

`hwaccel.Backend` plus `Register` and `RegisterAlias`; the built-in VAAPI, CUDA, QSV and
VideoToolbox backends are the reference implementations in `builtin.go`.

## 6. ffmpeg: Command.Check (done)

`caps.Check` validates encoders, decoders, muxers, demuxers, filters, pixel formats,
hardware devices and encoder option values against the installed build before running.
