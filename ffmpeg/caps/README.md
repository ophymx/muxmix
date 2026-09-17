# caps

What can this ffmpeg build do? `caps` turns ffmpeg's capability listings and
option tables into Go values.

```go
set, err := caps.Detect(ctx, nil) // nil = ffmpeg.DefaultRunner
if !set.HasEncoder("libx264") {
    return errors.New("this ffmpeg was built without libx264")
}
for _, e := range set.EncodersFor("hevc") {
    fmt.Println(e.Name) // libx265, hevc_nvenc, hevc_vaapi, ...
}

help, err := caps.EncoderHelp(ctx, nil, "libx264")
crf := help.Option("crf")         // Type "float", Min "-1", Max "FLT_MAX", Default "-1"
aq := help.Option("aq-mode")      // Constants: none=0, variance=1, autovariance=2, ...
help.SupportsPixelFormat("yuv420p10le")
```

`Set` holds encoders, decoders, muxers, demuxers, filters, pixel formats,
sample formats, hardware accelerators, bitstream filters and protocols,
plus the parsed `-version`. It is plain data with exported fields, so it can
be cached as JSON and reloaded without re-running ffmpeg.

`Help` is the parsed `-h <kind>=<name>` output for an encoder, decoder,
muxer, demuxer, filter or bitstream filter: supported pixel and sample
formats, hardware devices, container extensions and default codecs, filter
pads, and every AVOption with its type, flags, range, default and named
constants.

The parsers (`ParseCodecs`, `ParseFormats`, `ParseFilters`,
`ParsePixelFormats`, `ParseSampleFormats`, `ParseProtocols`, `ParseList`,
`ParseHelp`) are exported and tested against captures from every FFmpeg
release line in `matrix/`. Column changes between releases are handled:
the device column in `-formats` (7.0+), the command-support column dropped
from `-filters` (8.1), the bit-depth column in `-pix_fmts` (7.0+).

## Checking a command before running it

`Check` validates an `ffmpeg.Command` against a `Set` and reports every
problem at once, with what the build does have:

```go
if err := caps.Check(ctx, set, ffmpeg.DefaultRunner, cmd); err != nil {
    // ffmpeg command has 2 problems:
    //   output 0: -c:v h264: no such encoder (available: libx264, h264_nvenc, h264_vaapi)
    //   output 0: -crf -5: below minimum -1 for libx264
}
```

It checks encoders, decoders, muxers, demuxers, every filter named in
`-vf`, `-af` and `-filter_complex`, pixel formats (including what the
chosen encoder accepts), hardware device types, and encoder option values
against their AVOption type, range and named constants. Option tables are
fetched through the runner on demand; a `Checker` caches them, and
`AddHelp` preloads them for use without a runner.
