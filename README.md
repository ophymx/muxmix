# muxmix

Composable FFmpeg pipelines in Go: structured commands, typed `ffprobe`
output, and the fiddly media jobs already solved.

muxmix drives the `ffmpeg` and `ffprobe` binaries rather than binding to
libav, so it builds anywhere Go does, with no cgo and **no dependencies**.

```sh
go get github.com/ophymx/muxmix
```

## Start at the top

Most jobs are a few lines in `tasks`, which probes the input, decides what
to do with every stream, and tells you what it decided:

```go
plan, err := tasks.Transcode(ctx, "in.mkv", "out.mp4", tasks.TranscodeOptions{
    Video: tasks.VideoRule{CopyCodecs: tasks.AllCodecs, MaxHeight: 1080},
    Audio: tasks.AudioRule{CopyCodecs: tasks.AllCodecs, Languages: []string{"eng"}},
})
fmt.Print(plan)
// 0 video h264: copy (codec accepted by container)
// 1 audio aac [eng]: copy (codec accepted by container)
// 2 audio opus [deu]: drop (language not selected)
// 3 subtitle subrip [eng]: encode → mov_text (converted for container)
// 4 attachment : drop (attachments are not carried)
```

It knows which codecs each container will accept for copying, converts text
subtitles to the container's format, drops bitmap ones it cannot hold, keeps
rotation metadata straight, and adds `+faststart` for MP4. `PlanTranscode`
returns the same plan and its `ffmpeg.Command` without running anything, so
you can inspect or adjust it first.

Thumbnails, trickplay sprite sheets with their WebVTT index, previews,
waveforms, and HLS, DASH or CMAF ladders are one call each.

## Drop down when you need to

Nothing is hidden. Build a command yourself, with progress and cancellation:

```go
in, err := ffprobe.Probe(ctx, "in.mkv")

cmd := ffmpeg.NewCommand().
    Input("in.mkv", ffmpeg.Seek(90*time.Second)).
    Output("out.mp4", ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(20), ffmpeg.AudioCodec("aac"))

res, err := ffmpeg.Run(ctx, cmd,
    ffmpeg.TotalDuration(in.Duration()),
    ffmpeg.OnProgress(func(p ffmpeg.Progress) {
        fmt.Printf("\r%3.0f%% %.1fx eta %s", p.Fraction*100, p.Speed, p.ETA.Round(time.Second))
    }))
```

Cancelling the context sends `SIGINT` first so ffmpeg finishes writing the
container trailer, and kills it only if it overstays the grace period.
Errors carry ffmpeg's own message, not just an exit code.

## Packages

| Package              | What it does                                                            |
|----------------------|-------------------------------------------------------------------------|
| `tasks`              | Transcode planning, HLS/DASH packaging, thumbnails, trickplay, previews  |
| `ffprobe`            | Typed, version-tolerant ffprobe results; streaming packet/frame reads    |
| `ffmpeg`             | Structured commands, live progress, graceful cancel, version parsing     |
| `ffmpeg/analyze`     | Typed results from analysis filters: loudness, silence, black, crop, scenes |
| `ffmpeg/caps`        | What an ffmpeg build supports: codecs, formats, filters, option tables   |
| `ffmpeg/encode`      | Encoder-neutral video, audio and image settings                          |
| `ffmpeg/filtergraph` | Build and validate filtergraph strings                                   |
| `ffmpeg/hwaccel`     | Detect built-in and actually usable hardware acceleration                |

Each has its own README and runnable examples.

## Version tolerance is the point

One set of `ffprobe` types decodes the output of every FFmpeg release from
4.4 to 8.1. They are generated from the union of each release's own
`doc/ffprobe.xsd` and verified against the JSON those releases actually
print, so a field only some versions emit is simply absent rather than a
parse error, and every scalar reports whether it was there:

```go
res, err := ffprobe.Probe(ctx, "movie.mkv")
v := res.VideoStream()
fmt.Println(v.CodecName, v.Width.Int(), v.Height.Int(), v.FrameRate().Float64(), res.Duration())
```

Hardware acceleration gets the same treatment from the other direction:
`ffmpeg -hwaccels` only says what was compiled in, and its encoder list is
no better -- a build carries `av1_vaapi` on a chip with no AV1 encoder. So
`hwaccel` initialises each backend for real, encodes one frame with every
encoder it offers, in 8-bit and 10-bit, and remembers what this machine
actually does. That is what makes a "prefer hardware" policy fall back
instead of failing mid-job, and what keeps a 10-bit master from being
flattened by the upload on hardware that could have kept it.

## Requirements

Go 1.24 or newer. An `ffmpeg` and `ffprobe` binary on `PATH` at runtime,
any release from 4.4 onward.

## Testing

Fixtures captured from every supported release are committed, so the
conformance tests need no ffmpeg installed:

```sh
go test ./...        # passes with no ffmpeg on PATH
```

Tests that exercise a live binary skip when it is absent. Tests that need
a GPU are off by default; on a machine with one, run them with:

```sh
MUXMIX_HWACCEL_TEST=1 go test ./ffmpeg/hwaccel ./tasks -run Hardware
```

CI covers Linux, macOS and Windows. `matrix/` holds Docker images for the distributions
carrying each release line; `matrix/test.sh` runs the whole suite inside
every one of them, so the runner, progress pipe, cancellation, analysis
filters and tasks are checked against each release before a release of our
own. See [matrix/README.md](matrix/README.md).

## Status

Pre-1.0 and the API is still moving; expect breaking changes between minor
versions until v1. The packages are in use and tested, but pin a version.

## License

MIT. See [LICENSE](LICENSE).
