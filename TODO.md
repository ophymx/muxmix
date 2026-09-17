# TODO

## 1. ffprobe: helper layer over generated types (done)

Replaced by the lenient value types (`Int`, `Seconds`, `Rat`, `Bool`, `Tags`) and the
helpers in `ffprobe/helpers.go`. Remaining ffprobe follow-ups:

- Typed accessors for common side data (Display Matrix, Mastering display metadata,
  Content light level, Skip Samples) instead of reading `SideData.Extra` by key.
- A `Sections` query (`ffprobe -sections`) so callers can feature-detect a binary at runtime.

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
