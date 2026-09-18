# matrix

Docker images for the distributions that ship each FFmpeg release line,
and the scripts that capture fixtures from them.

| Image           | Base                | FFmpeg |
|-----------------|---------------------|--------|
| `ubuntu-22.04`  | ubuntu:22.04        | 4.4    |
| `debian-12`     | debian:bookworm     | 5.1    |
| `ubuntu-24.04`  | ubuntu:24.04        | 6.1    |
| `alpine-3.20`   | alpine:3.20         | 6.1    |
| `debian-13`     | debian:trixie       | 7.1    |
| `alpine-3.22`   | alpine:3.22         | 7.1    |
| `debian-sid`    | debian:sid          | 8.1    |
| `alpine-edge`   | alpine:edge         | 8.1    |

## Why

The packages accept the output of every FFmpeg release from 4.4 onward.
That claim is only worth something if it is tested, so the fixtures under
`ffprobe/testdata/probe/<version>/` and `ffmpeg/testdata/capture/<version>/`
are captured from real binaries and committed. The conformance tests read
them, which is why `go test ./...` passes with no ffmpeg installed at all.

## Scripts

- `run.sh [image ...]` builds the images and runs `capture.sh` inside each,
  writing ffprobe fixtures to `ffprobe/testdata/probe/<version>/`.
- `capture-ffmpeg.sh` records each build's version banner, capability
  listings and progress output under `ffmpeg/testdata/capture/<version>/`.
- `test.sh [image ...]` runs the whole Go suite, live tests included,
  inside every container using the host toolchain, so the runner, progress
  pipe, cancellation, analysis filters and tasks are exercised against each
  release line. `GOTESTFLAGS="-run Live -v" matrix/test.sh debian-13`
  narrows it.
- `gen-media.sh` regenerates the synthetic media the fixtures describe.
  Run it once with a full-featured ffmpeg; the outputs are committed under
  `ffprobe/testdata/media`.

Build logs and the shared module cache land in `matrix/*.log` and
`matrix/.cache/`, both ignored.
