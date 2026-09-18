# ffprobe

Typed access to `ffprobe` output for Go, generated from FFmpeg's own schema
and verified against the JSON every supported FFmpeg release actually prints.

```go
res, err := ffprobe.Probe(ctx, "movie.mkv")
if err != nil {
    return err
}
v := res.VideoStream()
fmt.Printf("%s %dx%d @ %.3f fps, %s, rotated %d\n",
    v.CodecName, v.Width.Int(), v.Height.Int(), v.FrameRate().Float64(), res.Duration(), v.Rotation())
for _, a := range res.AudioStreams() {
    fmt.Println(a.Language(), a.CodecName, a.Channels.Int(), a.SampleRate.Int(), a.Bitrate())
}
```

`example_test.go` has runnable examples for probing, streaming frames,
decoding side data, colour detection and version gating.

## Versions

One set of types decodes the output of FFmpeg 4.4 through 8.1. The types are
the union of every release's `doc/ffprobe.xsd`, so a field that only some
releases print is simply absent from older or newer output. Every scalar
field reports whether it was present:

| Type      | ffprobe spelling                        | Accessors                                   |
|-----------|-----------------------------------------|---------------------------------------------|
| `Int`     | `42` or `"42"`                          | `Int()`, `Int64()`, `Valid()`, `Or(def)`, `OrInt(def)` |
| `Seconds` | `"0.040000"`                            | `Duration()`, `Float64()`, `Valid()`, `Or(def)` |
| `Rat`     | `"30000/1001"`, `"16:9"`, `"0/0"`       | `Num()`, `Den()`, `Float64()`, `Rat()`, `Duration(ticks)` |
| `Bool`    | `0` / `1`                               | `Bool()`, `Valid()`                         |
| `Tags`    | `{"language": "eng"}`                   | `Get(key)` (case-insensitive), `Value(key)` |

A field that the running ffprobe does not print, because the release is
too old to know it, too new to still print it, or because the value could
not be determined (`"N/A"`), decodes to the type's zero value: `Valid()` is
false, and the numeric accessors return 0, `Bool()` false, and `Rat`
`0/0`. Nothing panics and nothing is required, so `-show_entries` output
and partial sections decode too. The generated doc comment on each field
says which release added or dropped it. `String()` on each type prints
the value the way ffprobe spells it, or `N/A`.

The same rule applies to whole sections. Every array section is a slice
of pointers (`[]*Stream`, `[]*Frame`, `[]*Chapter`, ...) so a range loop
can call the helpers directly, and every nested object is a value:
`res.Format.BitRate` on a streams-only probe is simply not `Valid()`, and
`stream.Disposition.Default` on a stream without a disposition is false.
The only pointer is `Result.Error`, whose presence is the signal.

### Seconds fields and their helpers

ffprobe prints most timestamps twice: as an integer count of ticks
(`duration_ts`, `start_pts`, `pts`, `duration`) and as decimal seconds
(`duration`, `start_time`, `pts_time`, `duration_time`). The generated
types name every seconds field with a `Secs` suffix after the ffprobe key
minus its `_time` (`DurationSecs`, `StartSecs`, `PTSSecs`), and the tick
count keeps ffprobe's name (`DurationTS`, `StartPTS`, `PTS`; the integer
`duration` of frames and packets is `DurationTS` too). That leaves the
natural names for `time.Duration` helpers with fallbacks:

| Method                                   | Reads, in order                                            |
|------------------------------------------|------------------------------------------------------------|
| `Stream.Duration()`                      | `duration`, `duration_ts` × `time_base`, Matroska `DURATION` tag |
| `Stream.StartTime()`                     | `start_time`, `start_pts` × `time_base`                    |
| `Stream.Bitrate()`                       | `bit_rate`, Matroska `BPS` / `BPS-eng` tag                 |
| `Stream.FrameCount()`                    | `nb_frames`, `nb_read_frames` (`CountFrames()`), Matroska `NUMBER_OF_FRAMES` tag |
| `Format.Duration()`, `Format.StartTime()`| the format fields                                          |
| `Chapter.StartTime()`, `EndTime()`, `Duration()` | `start_time`/`end_time`, else `start`/`end` × `time_base` |
| `Frame.Time()`                           | `pts_time`, `pkt_pts_time` (4.4), `best_effort_timestamp_time` |
| `Frame.Duration()`                       | `duration_time` (6.0+), `pkt_duration_time`                |
| `Packet.Time()`, `Packet.Duration()`     | `pts_time` else `dts_time`; `duration_time`                |
| `Result.Duration()`                      | the format duration, else the longest stream               |

Every helper returns 0 when nothing is known, as for a raw elementary
stream.

### Side data and rotation

Sections whose keys vary with content (side data, IAMF stream group
components) keep their named fields and collect everything else in an
`Extra map[string]any`. The common side data types have typed accessors:
`stream.DisplayMatrix()`, `stream.MasteringDisplay()`,
`stream.ContentLightLevel()`, `stream.DolbyVision()`, `stream.Stereo3D()`,
`stream.Spherical()`, `packet.SkipSamples()`, and `stream.IsHDR()`.
Anything else decodes with `SideDataAs[T]` or `DecodeSideData`.
The enumerated side-data values are typed with constants in ffprobe's own
spelling, so `s.Stereo3D().Type == ffprobe.StereoSideBySide` and
`s.Spherical().Projection == ffprobe.ProjectionEquirectangular` need no
transcription from the ffmpeg source; `Stereo3DType.Packed` says whether
both eyes share the frame.

`Stream.Rotation()` is the one rotation to use: the clockwise degrees a
player applies before display, normalised to 0, 90, 180 or 270, read from
the Display Matrix side data first and then the legacy `rotate` tag that
ffprobe 4.4 and old MP4 files carry. Streams rotated 90 or 270 display
with width and height swapped. `DisplayMatrix.Rotation` keeps ffprobe's
raw value (counter-clockwise, so a phone video reports -90) and
`Degrees()` normalises it the same way.

HDR mastering and light level metadata usually reach ffprobe only as
per-frame SEI, so the stream section alone says nothing. `ffprobe.Color`
reads the stream header and, when it has no HDR side data, decodes the first
frame:

```go
c, err := ffprobe.Color(ctx, "movie.mkv")
if err == nil && c.IsHDR() {
    fmt.Println(c.Transfer, c.Mastering.MaxNits(), c.LightLevel.MaxContent.Int())
}
```

### Asking the binary

`Version` runs `ffprobe -show_program_version -show_library_versions` once
per `Prober` and caches the answer; `VersionInfo.AtLeast(major, minor)`
understands release and distribution spellings (`7.1.5`, `n8.0`,
`6.1.1-3ubuntu5`) and treats git snapshots (`N-118000-g...`) as newest.
`Prober.Sections` returns the section tree from `ffprobe -sections` for
finer feature detection, for example `root.Has("stream_groups")`.

```go
if v, err := ffprobe.Version(ctx); err == nil && v.AtLeast(7, 0) {
    opts = append(opts, ffprobe.ShowStreamGroups())
}
```

## Sections

With no options, `Probe` shows format and streams. Add sections with
options:

```go
res, err := ffprobe.Probe(ctx, path,
    ffprobe.ShowChapters(),
    ffprobe.ShowPrograms(),
    ffprobe.CountFrames(),
    ffprobe.SelectStreams("v:0"),
)
```

| Option                       | ffprobe flag                    |
|------------------------------|---------------------------------|
| `ShowFormat()`               | `-show_format`                  |
| `ShowStreams()`              | `-show_streams`                 |
| `ShowChapters()`             | `-show_chapters`                |
| `ShowPrograms()`             | `-show_programs`                |
| `ShowStreamGroups()`         | `-show_stream_groups` (7.0+)    |
| `ShowPackets()`              | `-show_packets`                 |
| `ShowFrames()`               | `-show_frames`                  |
| `ShowData()`, `ShowDataHash` | `-show_data`, `-show_data_hash` |
| `CountFrames()`, `CountPackets()` | `-count_frames`, `-count_packets` |
| `SelectStreams(spec)`        | `-select_streams`               |
| `ShowEntries(spec)`          | `-show_entries`                 |
| `ReadIntervals(spec)`        | `-read_intervals`               |
| `InputFormat(name)`          | `-f`                            |
| `InputOption(k, v)`          | any `-k v` before the input     |
| `Probesize`, `AnalyzeDuration` | `-probesize`, `-analyzeduration` |
| `Timeout(d)`                 | kills ffprobe after `d` (this call only) |
| `Args(...)`                  | verbatim extra arguments        |

`ProbeReader` probes an `io.Reader` through stdin. `Version`,
`PixelFormats` and `Sections` query the binary itself.

A `Prober` is built with `New(ffprobe.WithBinary(path),
ffprobe.WithEnv(...), ffprobe.WithStderr(w), ffprobe.WithTimeout(d))`;
`WithTimeout` is the default for every call and `Timeout(d)` overrides it
per call (`Timeout(0)` disables it). When either expires ffprobe is
killed and the error wraps `context.DeadlineExceeded` while still naming
ffprobe and the input: `ffprobe: movie.mkv: timed out after 5s: context
deadline exceeded`.
Build one `Prober` per binary and keep it for the life of the process:
it caches the version and section tree, so a fresh `Prober` per call
throws that away and re-runs ffprobe for it.

## Streaming packets and frames

`-show_frames` on a long file produces more JSON than you want in memory.
`Frames`, `Packets` and `PacketsAndFrames` (on a `Prober` or at package
level) decode one element at a time and stop ffprobe when you leave the
loop:

```go
for f, err := range ffprobe.Frames(ctx, path, ffprobe.SelectStreams("v:0")) {
    if err != nil {
        return err
    }
    if f.KeyFrame.Bool() {
        keyframes = append(keyframes, f.Time())
    }
}
```

## Errors

`Probe` always passes `-show_error`, so failures come back as a `*ProbeError`
carrying ffprobe's AVERROR code and message. `ProbeError.Code` is an
`AVError`: a negated POSIX errno for system errors, or one of the named
FFmpeg constants (`AVErrorInvalidData`, `AVErrorDemuxerNotFound`,
`AVErrorProtocolNotFound`, `AVErrorEOF`, `AVErrorHTTPNotFound`, ...).
`errors.Is` matches the constants themselves, the sentinels
`ErrInvalidData`, `ErrDemuxerNotFound`, `ErrProtocolNotFound` and
`ErrEOF`, and for errno codes whatever a `syscall.Errno` matches:

```go
_, err := ffprobe.Probe(ctx, path)
switch {
case errors.Is(err, fs.ErrNotExist):          // ENOENT
case errors.Is(err, fs.ErrPermission):        // EACCES
case errors.Is(err, syscall.ETIMEDOUT):       // a network input that timed out
case errors.Is(err, ffprobe.ErrInvalidData):  // not a media file
case errors.Is(err, ffprobe.AVErrorHTTPNotFound):
case errors.Is(err, ffprobe.ErrFFProbeNotFound):
case errors.Is(err, context.DeadlineExceeded): // Timeout or WithTimeout
}
```

An `*ExitError` is returned when ffprobe fails without a structured error,
for example on a bad option; it carries the exit code, stderr and the
arguments, and unwraps to the `os/exec` error.

## How the types are generated

`go generate ./ffprobe` runs `internal/gen`. The generator is a module of
its own, so its schema library is not a dependency of muxmix, which has
none; the directive enters that module and points it back here with
`-dir`. It reads three inputs:

- `xsd/ffprobe-<tag>.xsd` for every tag in `ffprobe.versions`: the field
  vocabulary and which release introduced or removed each field.
- `testdata/probe/<version>/sections.txt`, the output of `ffprobe -sections`:
  which sections are arrays and which carry variable keys.
- `testdata/probe/<version>/*.json`: real output, used to confirm fields the
  XSD dropped but the JSON writer kept, to date fields the XSD never listed,
  and to warn about any key the generated types would not recognise.

`ffprobe.schema.json` is written from the same model as a JSON Schema
(draft-07) for use outside Go.

## Conformance matrix

`matrix/` holds Dockerfiles for the distributions that ship each FFmpeg
release line. `matrix/run.sh` builds them and runs `matrix/capture.sh`
inside each, probing the synthetic media under `testdata/media` (produced by
`matrix/gen-media.sh`) in every output mode. The captures are committed;
`go test` decodes each of them leniently and strictly (no unknown keys) and
checks the same facts about the same media across every version.

To add a release line: append its tag to `ffprobe.versions`, add a
Dockerfile under `matrix/`, run `matrix/run.sh`, then `go generate` and
`go test`.
