# muxmix

Go packages for driving `ffmpeg` and `ffprobe`.

| Package                | What it does                                                          |
|------------------------|-----------------------------------------------------------------------|
| `ffprobe`              | Typed, version-tolerant ffprobe results; streaming packet/frame reads |
| `ffmpeg`               | Structured commands, live progress, graceful cancel, version parsing  |
| `ffmpeg/filtergraph`   | Build and validate filtergraph strings                                |
| `ffmpeg/hwaccel`       | Detect built-in and actually usable hardware acceleration             |

Each package has its own README.

## Testing against FFmpeg versions

`matrix/` contains Dockerfiles for the distributions that ship each FFmpeg
release line (4.4 on Ubuntu 22.04 through 8.1 on Debian sid and Alpine edge).
`matrix/run.sh` builds them and captures ffprobe output for the synthetic
media in `ffprobe/testdata/media`; the captures are committed and drive the
ffprobe conformance tests, so `go test ./...` needs no ffmpeg install for
those. Tests that exercise a live binary skip when it is absent.

```sh
go generate ./ffprobe   # regenerate types from the XSDs and captures
go test ./...
```
