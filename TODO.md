# TODO

## 1. ffprobe: helper layer over generated types

All fields on `StreamType`, `FormatType`, etc. are XSD-generated pointer types (`*string`,
`*int`, etc.). Every caller ends up writing the same nil-check boilerplate to extract a
duration, resolution, or frame rate safely.

Add a thin, non-generated helper layer on top of `ffprobe` — typed accessors that hide the
pointer indirection and supply sensible zero-value defaults:

```go
// e.g.
func VideoStream(info *ffprobe.FFprobeType) (*ffprobe.StreamType, error)
func (s *StreamType) HeightPx() int
func (s *StreamType) FrameRate() float64
func (s *StreamType) DurationSecs(fallback *FormatType) float64
```

Many ffprobe values are stored as rational strings (e.g. `r_frame_rate: "30000/1001"`).
The helpers should provide a `*big.Rat` accessor alongside the `float64` convenience form so
callers that need exact arithmetic — muxing, segment boundary calculations — can avoid
floating-point rounding:

```go
func (s *StreamType) FrameRateRat() *big.Rat  // returns nil if unparseable
func ParseRat(s string) (*big.Rat, bool)       // shared "num/den" parser
```

The generated types stay as-is; the helpers live alongside them in a separate file.

## 2. hwaccel: consolidated arg builder

Building a hardware-accelerated FFmpeg invocation currently requires three separate calls:

```go
inputArgs, _ := hwaccel.BuildInputArgs(kind, device)
vf, _        := hwaccel.BuildFilter(kind)
vc, _        := hwaccel.BuildVideoCodec(kind, codec)
```

Each returns independently, the caller must assemble them, and the conditional `-vf` insertion
is repetitive. `SystemSupport.Select` already exists for choosing a backend; there should be a
matching function that turns the selection into ready-to-use arg fragments:

```go
type HWArgs struct {
    Input  []string // pre-input device args
    Filter string   // -vf value, empty if none
    Codec  string   // encoder name
}

func BuildHWArgs(kind Kind, device, codec string, extraFilters ...string) (HWArgs, error)
```

## 3. ffmpeg: higher-level codec builder (in progress)

The `ffmpeg` package has `RunWithRawArgs`, which is flexible but pushes full arg-list
construction into callers. Every transcoding command re-encodes the same knowledge about
bitrate flags, pass-log conventions, and pixel formats.

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
