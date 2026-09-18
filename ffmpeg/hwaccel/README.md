# hwaccel

What hardware acceleration this machine can actually use, and how to ask
for it.

`ffmpeg -hwaccels` only says what the binary was compiled with. Whether a
backend works depends on the driver stack, device nodes, permissions and
runtime libraries on the host, so this package initialises each backend
for real by running a tiny pipeline, and records what happened.

The build's encoder list is no more honest. A distribution ffmpeg carries
`av1_vaapi` and `av1_nvenc` whatever silicon you have, so on a Tiger Lake
iGPU or an Ampere GPU -- neither of which encodes AV1 -- the encoder is
present, the device initialises, and the job dies partway through with
"No usable encoding profile found". So on every device that works, this
package also encodes one frame with each encoder the backend offers, and
records that too -- in 8-bit and in 10-bit, because a device that encodes
HEVC may still only encode it 8-bit, and the upload format is the only
thing standing between a 10-bit master and an 8-bit output.

## Detect once, share the result

```go
sys, fromCache, err := hwaccel.DetectCached(ctx, nil, "/var/cache/app/ffmpeg.json", 24*time.Hour, hwaccel.ProbeOptions{})
```

`System` holds the build's `caps.Set` plus a probe result per backend and
device, each carrying the encoders that ran on it. `Detect` runs
`caps.Detect` and then probes every registered backend; `DetectWithCaps`
probes for a `caps.Set` you already have. Detection takes a few seconds,
so `DetectCached` keeps it on disk and reuses it until the ffmpeg version
changes, the file is older than the given age, the device nodes a backend
would probe differ from the ones it probed last time, or the cache holds
no encoder verdicts for the codecs now asked about.

`ProbeOptions.Codecs` narrows the encoder probes to the codecs an
application actually encodes; `ProbeOptions.NoEncoderProbe` skips them,
leaving `Select` to trust the build's encoder list as before.

Ask the `System` what it found:

```go
sys.Available(hwaccel.VAAPI)          // initialised on at least one device
sys.Device(hwaccel.VAAPI)             // "/dev/dri/renderD128", true
sys.SupportsCodec(hwaccel.CUDA, "hevc")
sys.AvailableKinds()                  // []Kind{vaapi}
sys.Probes[hwaccel.QSV][0].Error      // why a backend was rejected
sys.Probes[hwaccel.VAAPI][0].Encoders // per-codec verdicts, their working
                                      // upload formats, and the driver's words
```

## Select a backend for a codec

```go
sel, err := sys.Select("h264", hwaccel.CUDA, hwaccel.VAAPI)
```

`Select` returns the first backend in preference order (registration order
when none is given) that initialised here, whose encoder for the codec is
in the build, and whose encoder probe -- when one ran -- encoded a frame.
The error is a `*SelectError` that explains every candidate: `vaapi:
av1_vaapi did not encode on /dev/dri/renderD128: No usable encoding
profile found.; cuda: build lacks av1_nvenc`.

A `Selection` carries the kind, device and encoder, and renders its own
ffmpeg pieces. The pipeline is software decode, upload, hardware encode,
which works for any input codec:

```go
cmd := ffmpeg.NewCommand().Input("in.mkv")
sel.Apply(cmd)                                  // -init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw
cmd.Output("out.mp4", sel.Opts("v:0", "scale=1280:-2")...)
// -filter:v:0 scale=1280:-2,format=nv12,hwupload -c:v h264_vaapi
```

The zero `Selection` means software: `Apply` adds nothing, `Filter` joins
the filters, and `Opts` omits the codec so the caller picks a software
encoder.

## Keep the source's bit depth

The upload is 8-bit unless asked otherwise, so a 10-bit source reaches the
encoder flattened. `PreserveDepth` sets the upload format the probes
proved this device encodes:

```go
sel = sys.PreserveDepth(sel, stream.PixFmt)  // "yuv420p10le"
sel.Filter("scale=1280:-2")                  // scale=1280:-2,format=p010,hwupload
```

On a device whose encoder has no 10-bit path it flattens to `nv12`
explicitly rather than leaving the backend's default, since CUDA's default
is to upload the source's own format -- which would hand 10-bit frames to
an 8-bit-only encoder and fail the job. An 8-bit source, a 4:2:2 or 4:4:4
layout, or a `System` with no probe for that encoder is left alone. The
`tasks` package calls it for every stream it encodes.

## Policy: what a job wants

Jobs should say how much they want hardware, not which backend:

```go
hwaccel.Policy{}                          // software (zero value)
hwaccel.PreferHardware()                  // any usable backend, else software
hwaccel.PreferHardware(hwaccel.CUDA)      // CUDA if usable, else software
hwaccel.RequireHardware(hwaccel.VAAPI)    // VAAPI or an error

sel, reason, err := policy.Resolve(sys, "h264")
```

`Resolve` returns the selection and, when it fell back to software under
`Prefer`, the reason (the `SelectError` detail) for the job's log or plan.
`Require` turns that reason into an error. A nil `System` counts as no
hardware detected. The `tasks` package takes a `Policy` on every job and
resolves it against the `System` on its `Tools`.

## Backends

Each backend is a `Backend` registered with `Register`; VAAPI, CUDA, QSV
and VideoToolbox are built in, and `RegisterAlias` maps other names
(`nvenc` resolves to `cuda`). A backend says how to probe itself, which
global options initialise its device, how the filter chain uploads to it,
and which encoder serves each codec. `VideoEncoder(kind, codec)` exposes
that last mapping on its own.

Device candidates: VAAPI and QSV probe every `/dev/dri/renderD*` on Linux;
CUDA and VideoToolbox probe once with no device. `ProbeOptions.Kinds`
limits which backends are probed and `ProbeOptions.Devices` overrides the
candidates per backend.

A backend may also implement two optional halves: `VideoCodecLister` so
`Detect` knows which codecs to probe on it (otherwise `CommonCodecs`), and
`EncoderProber` to build its own encoder probe command (otherwise one is
built from `DeviceArgs` and `Filter`).

## Testing against real hardware

The tests in `hardware_test.go` drive the machine's GPUs: they cross-check
every claim the `System` makes against an actual encode, in both
directions, and they are skipped unless asked for.

```sh
MUXMIX_HWACCEL_TEST=1 go test ./ffmpeg/hwaccel ./tasks -run Hardware -v
```

## Not yet

Hardware decoding. The current pipeline decodes in software and uploads,
which is safe for any input. A decode flag on `Policy` that switches to
`-hwaccel` with on-device scaling is the planned next step.
