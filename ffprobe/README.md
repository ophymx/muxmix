# ffprobe

Typed access to `ffprobe` output for Go, generated from FFmpeg's own schema
and verified against the JSON every supported FFmpeg release actually prints.

```go
res, err := ffprobe.Probe(ctx, "movie.mkv")
if err != nil {
    return err
}
v := res.VideoStream()
fmt.Printf("%s %dx%d @ %.3f fps, %s\n",
    v.CodecName, v.Width.Int(), v.Height.Int(), v.FrameRate().Float64(), res.Duration())
for _, a := range res.AudioStreams() {
    fmt.Println(a.Language(), a.CodecName, a.Channels.Int(), a.SampleRate.Int())
}
```

## Versions

One set of types decodes the output of FFmpeg 4.4 through 8.1. The types are
the union of every release's `doc/ffprobe.xsd`, so a field that only some
releases print is simply absent from older or newer output. Every scalar
field reports whether it was present:

| Type      | ffprobe spelling                        | Accessors                                   |
|-----------|-----------------------------------------|---------------------------------------------|
| `Int`     | `42` or `"42"`                          | `Int()`, `Int64()`, `Valid()`, `Or(def)`    |
| `Seconds` | `"0.040000"`                            | `Duration()`, `Float64()`, `Valid()`        |
| `Rat`     | `"30000/1001"`, `"16:9"`, `"0/0"`       | `Num()`, `Den()`, `Float64()`, `Rat()`, `Duration(ticks)` |
| `Bool`    | `0` / `1`                               | `Bool()`, `Valid()`                         |
| `Tags`    | `{"language": "eng"}`                   | `Get(key)` (case-insensitive), `Value(key)` |

Anything ffprobe cannot determine is absent (or `"N/A"`), and both decode to
a value whose `Valid()` is false. There are no required fields, so
`-show_entries` output and partial sections decode too.

Sections whose keys vary with content (side data, IAMF stream group
components) keep their named fields and collect everything else in an
`Extra map[string]any`. The common side data types have typed accessors:
`stream.DisplayMatrix()`, `stream.MasteringDisplay()`,
`stream.ContentLightLevel()`, `stream.DolbyVision()`, `stream.Stereo3D()`,
`stream.Spherical()`, `packet.SkipSamples()`, and `stream.IsHDR()`.
Anything else decodes with `SideDataAs[T]` or `DecodeSideData`.

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

`Prober.Sections` returns the section tree from `ffprobe -sections` for
runtime feature detection, for example `root.Has("stream_groups")`.

Fields that were renamed between releases keep both names; helpers such as
`Frame.Time()` and `Frame.DurationOf()` read whichever one is present.

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
| `Timeout(d)`                 | kills ffprobe after `d`         |
| `Args(...)`                  | verbatim extra arguments        |

`ProbeReader` probes an `io.Reader` through stdin. `Version` and
`PixelFormats` query the binary itself.

## Streaming packets and frames

`-show_frames` on a long file produces more JSON than you want in memory.
`Frames`, `Packets` and `PacketsAndFrames` decode one element at a time and
stop ffprobe when you leave the loop:

```go
for f, err := range ffprobe.Default.Frames(ctx, path, ffprobe.SelectStreams("v:0")) {
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
carrying ffprobe's AVERROR code and message. The code maps onto sentinels:

```go
_, err := ffprobe.Probe(ctx, path)
switch {
case errors.Is(err, fs.ErrNotExist):
case errors.Is(err, ffprobe.ErrInvalidData): // not a media file
case errors.Is(err, ffprobe.ErrFFProbeNotFound):
}
```

An `*ExitError` is returned when ffprobe fails without a structured error,
for example on a bad option; it carries the exit code and stderr.

## How the types are generated

`go generate ./ffprobe` runs `internal/gen`, which reads three inputs:

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
