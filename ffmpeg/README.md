# ffmpeg

Run ffmpeg from Go with a structured command model, live progress from
ffmpeg's own progress stream, graceful cancellation and errors that carry
ffmpeg's message.

```go
cmd := ffmpeg.NewCommand().
    Input("in.mkv", ffmpeg.Seek(90*time.Second)).
    Output("out.mp4",
        ffmpeg.Map("0:v:0"), ffmpeg.Map("0:a:m:language:eng"),
        ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(20), ffmpeg.Preset("slow"),
        ffmpeg.AudioCodec("aac"), ffmpeg.BitRate("a", "160k"),
        ffmpeg.MovFlags("+faststart"))

res, err := ffmpeg.Run(ctx, cmd, ffmpeg.OnProgress(func(p ffmpeg.Progress) {
    log.Printf("%v %.1fx", p.Time, p.Speed)
}))
```

## Command model

A `Command` is global options, inputs and outputs. Each input and output
carries its own ordered option list, rendered in the order ffmpeg needs:
global options, then per-input options before each `-i`, then per-output
options and `-map` entries before each output URL.

Options are functions (`Opt`) that append to an `Options` list. Constructors
exist for the options people reach for most, named after what they do rather
than the flag:

| Area        | Constructors                                                                                  |
|-------------|-----------------------------------------------------------------------------------------------|
| Input       | `Seek`, `SeekEOF`, `Duration`, `To`, `Format`, `Lavfi`, `ConcatDemuxer`, `StreamLoop`, `ReadRate`, `HWAccel`, `Probesize` |
| Mapping     | `Map`, `MapLabel`, `MapExclude`, `MapMetadata`, `MapChapters`                                 |
| Codecs      | `VideoCodec`, `AudioCodec`, `SubtitleCodec`, `Copy`, `CopyAll`, `NoVideo`, `NoAudio`, `NoSubtitles` |
| Rate control| `BitRate`, `MaxRate`, `BufSize`, `CRF`, `QScale`, `Pass`, `PassLogFile`                       |
| Encoder     | `Preset`, `Tune`, `Profile`, `Level`, `PixFmt`, `GOP`, `BFrames`, `X264Params`, `X265Params` |
| Video/audio | `FrameRate`, `Size`, `AspectRatio`, `Frames`, `SampleRate`, `Channels`, `ChannelLayout`, `SampleFmt` |
| Filters     | `Filter`, `VideoFilter`, `AudioFilter`, `FilterComplex` (global)                              |
| Container   | `Metadata`, `StreamMetadata`, `Disposition`, `MovFlags`, `Tag`, `Shortest`, `Timecode`, `Program`, `StreamGroup`, `StreamID` |
| Global      | `LogLevel`, `Overwrite`, `NoOverwrite`, `Threads`, `InitHWDevice`, `FilterHWDevice`           |

Anything else goes through `Set(name, value)`, `Flag(name)` or
`Raw(args...)`. `Command.String()` renders a shell-quoted line for logs.

`filtergraph` builds `-filter_complex` graphs; `hwaccel.InputOpt` and
`hwaccel.EncodeOpt` return the options for a hardware backend; `caps`
reports which encoders, formats and filters the binary has and what options
they take; `analyze` runs the analysis filters (loudnorm, silencedetect,
cropdetect, ...) and returns typed results.

## Running

`Run` applies sensible defaults unless `NoDefaultArgs` is given:
`-hide_banner -nostdin`, `-loglevel error` unless the command sets one, and
`-y` unless the command sets `-y` or `-n`. `RunArgs` runs a raw argument
list.

Run options:

- `OnProgress(fn)`: progress updates. ffmpeg writes its `-progress` stream
  to a dedicated pipe, so updates are structured and independent of the log
  level. `ProgressInterval` sets the reporting period. Where a pipe cannot
  be inherited (Windows) the stats line on stderr is parsed instead;
  `NoProgressPipe` forces that.
- `Stdin`, `Stdout`, `Stderr`, `Dir`, `Env`: process plumbing. Stdout is
  captured into the result unless redirected; use `Stdout` when an output
  is `pipe:1`.
- `Report(path)`: capture ffmpeg's full log via `FFREPORT`.

Cancelling the context sends SIGINT, which makes ffmpeg finish the file it
is writing, and kills it only after the grace period (`WithGrace`).

## Two-pass encoding

`TwoPass` runs a bitrate-targeted command twice: first to the null muxer
with `-pass 1` (audio and subtitles dropped), then as given with `-pass 2`,
sharing a statistics file that is cleaned up afterwards. libx265 gets its
`x265-params pass=N:stats=` form. One callback reports both passes with a
combined fraction when the input duration is known:

```go
res, err := ffmpeg.TwoPass(ctx, nil, cmd, ffmpeg.TwoPassOptions{
    Duration:   info.Duration(),
    OnProgress: func(p ffmpeg.TwoPassProgress) { bar.Set(p.Fraction) },
})
```

`PassCommand` derives either pass's command for inspection.

## Results and errors

`Result` carries the exit code, timings, captured stdout and stderr, the
last `Progress`, and the report if requested. `Result.LogLines` filters the
progress noise out of stderr.

Failures return `*Error`, whose message includes ffmpeg's own error line,
and which unwraps to the underlying `*exec.ExitError` or context error.
`ErrFFmpegNotFound` is returned when the binary is missing.

## Progress

`Progress` has frame count, fps, quantizer, bit rate, bytes written, output
time, dup/drop counts and speed. `Fields` holds the raw key=value pairs of a
`-progress` block, including per-stream quantizers. The parsers are tested
against the `-progress` and stats output recorded from every FFmpeg release
line in `matrix/`.

## Version

`Version` parses `ffmpeg -version`: release line, configure flags (so
`Enabled("libx264")` answers whether an encoder was built in), and library
versions.
