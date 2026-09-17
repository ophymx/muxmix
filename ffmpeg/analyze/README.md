# analyze

Typed results from ffmpeg's analysis filters. Each function runs one
filter over the input into the null muxer and parses what the filter logged;
each has a `Parse` counterpart for logs obtained some other way. The
package-level functions use `ffmpeg.DefaultRunner`; an `Analyzer{Runner: r}`
has the same methods on a runner of your own.

```go
// Two-pass loudness normalisation
stats, err := analyze.Loudnorm(ctx, "in.wav", analyze.DefaultLoudnormTargets)
cmd := ffmpeg.NewCommand().Input("in.wav").
    Output("out.wav", ffmpeg.AudioFilter(stats.SecondPass(analyze.DefaultLoudnormTargets)))

// Where is the silence?
gaps, err := analyze.Silence(ctx, "talk.wav", analyze.SilenceOptions{NoiseDB: -40, MinDuration: time.Second})

// Auto-crop letterboxing
crop, err := analyze.CropDetect(ctx, "movie.mkv", analyze.CropOptions{})
cmd.Output("out.mkv", ffmpeg.VideoFilter(crop.Filter()))
```

| Function       | Filter          | Result                                                        |
|----------------|-----------------|---------------------------------------------------------------|
| `Loudnorm`     | `loudnorm`      | measured I/TP/LRA/threshold and a ready second-pass filter    |
| `Ebur128`      | `ebur128`       | integrated loudness, range, sample and true peak              |
| `VolumeDetect` | `volumedetect`  | mean and max dB, headroom, histogram                          |
| `Silence`      | `silencedetect` | silent intervals                                              |
| `AStats`       | `astats`        | peak, RMS, DC offset, noise floor and the rest                |
| `Black`        | `blackdetect`   | black intervals                                               |
| `BlackFrames`  | `blackframe`    | per-frame black percentage                                    |
| `Freeze`       | `freezedetect`  | frozen intervals                                              |
| `CropDetect`   | `cropdetect`    | the crop region and its filter string                         |
| `SceneChanges` | `select`+`metadata` | cut times with scores (works on every version)            |
| `IdetDetect`   | `idet`          | field order counts and a verdict                              |

Intervals that were still open when the input ended have `Open` set.
Values ffmpeg did not report are NaN for dB measurements and -1 otherwise.
When ffmpeg ran but the filter's output was not in the log the error wraps
`ErrNoResult`; an ffmpeg failure is an `*ffmpeg.Error` as usual.
Input options such as `ffmpeg.Seek`, `ffmpeg.Duration` or `ffmpeg.Lavfi`
are passed through, so any URL ffmpeg reads can be analysed:

```go
crop, err := analyze.CropDetect(ctx, "movie.mkv", analyze.CropOptions{},
    ffmpeg.Seek(10*time.Minute), ffmpeg.Duration(30*time.Second))
```

The parsers are tested against logs captured from every FFmpeg release
line in `matrix/` over deterministic synthetic media, so a change in a
filter's log format shows up as a test failure rather than a silent empty
result.
