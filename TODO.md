# TODO

## 1. ffprobe: helper layer over generated types (done)

Replaced by the lenient value types (`Int`, `Seconds`, `Rat`, `Bool`, `Tags`) and the
helpers in `ffprobe/helpers.go`. Remaining ffprobe follow-ups:

- Typed accessors for common side data (Display Matrix, Mastering display metadata,
  Content light level, Skip Samples) instead of reading `SideData.Extra` by key.
- A `Sections` query (`ffprobe -sections`) so callers can feature-detect a binary at runtime.

## 2. hwaccel: consolidated arg builder (done)

`hwaccel.InputOpt(kind, device)` and `hwaccel.EncodeOpt(kind, codec, filters...)` return
`ffmpeg.Opt` values that plug straight into `ffmpeg.Command`; `BuildEncodeArgs` remains for
raw argument lists.

## 3. ffmpeg: higher-level codec builder (in progress)

`ffmpeg.Command` now models inputs, outputs, maps and options, and the option constructors
cover the common encoder knobs; `ffmpeg/caps` reports what the installed build supports and
the option tables of each component. What is still missing is the codec-specific layer above
them: per-encoder presets and rate-control profiles, two-pass orchestration, and a
`Command.Check(set)` that validates encoders, formats and option values against `caps`.

Transcoder commands outside of this repository are currently being used to experiment with what
codec-specific builder APIs should look like before anything is added here. Once patterns
stabilize there, extract them into the `ffmpeg` package.

## 4. hwaccel: marshal/unmarshal SystemSupport for caching

`DetectSystem` probes hardware at runtime which takes noticeable time. `SystemSupport` should
be serializable so the result can be cached to disk (e.g. JSON in a state directory) and
reloaded on subsequent invocations without re-probing.

The cache should be invalidated on FFmpeg version change and should be easy to bust manually.
`Support` and `SystemSupport` need JSON tags and any unexported fields made serializable, or a
separate envelope type used for the on-disk representation.

## 5. hwaccel: registerable Kind interface

`Kind` is currently a closed string enum with probing logic and arg building implemented via
`switch` statements in `probe.go` and `build.go`. This prevents external packages from adding
new backends without modifying muxmix.

Replace the switch-based dispatch with a registry of backend descriptors that can be
registered at init time:

```go
type Backend interface {
    Kind() Kind
    DefaultDevices() []string
    ProbeArgs(device string) ([]string, error)
    BuildInputArgs(device string) ([]string, error)
    BuildFilter(extraFilters ...string) (string, error)
    BuildVideoCodec(codec string) (string, error)
}

func RegisterBackend(b Backend)
```

The built-in backends (`VAAPI`, `CUDA`, `QSV`, `VideoToolbox`) become the reference
implementations. An external package — e.g. a Topaz AI upscaler backend — could then register
its own `Kind`, probe command, and arg builder without any changes to muxmix itself.
