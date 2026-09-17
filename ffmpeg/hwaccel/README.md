# hwaccel

What hardware acceleration this machine can actually use, and how to ask
for it.

`ffmpeg -hwaccels` only says what the binary was compiled with. Whether a
backend works depends on the driver stack, device nodes, permissions and
runtime libraries on the host, so this package initialises each backend
for real by running a tiny pipeline, and records what happened.

## Detect once, share the result

```go
sys, fromCache, err := hwaccel.DetectCached(ctx, nil, "/var/cache/app/ffmpeg.json", 24*time.Hour, hwaccel.ProbeOptions{})
```

`System` holds the build's `caps.Set` plus a probe result per backend and
device. `Detect` runs `caps.Detect` and then probes every registered
backend; `DetectWithCaps` probes for a `caps.Set` you already have.
Detection takes a second or so, so `DetectCached` keeps it on disk and
reuses it until the ffmpeg version changes, the file is older than the
given age, or the device nodes a backend would probe differ from the ones
it probed last time.

Ask the `System` what it found:

```go
sys.Available(hwaccel.VAAPI)          // initialised on at least one device
sys.Device(hwaccel.VAAPI)             // "/dev/dri/renderD128", true
sys.SupportsCodec(hwaccel.CUDA, "hevc")
sys.AvailableKinds()                  // []Kind{vaapi}
sys.Probes[hwaccel.QSV][0].Error      // why a backend was rejected
```

## Select a backend for a codec

```go
sel, err := sys.Select("h264", hwaccel.CUDA, hwaccel.VAAPI)
```

`Select` returns the first backend in preference order (registration order
when none is given) that initialised here and whose encoder for the codec
is in the build. The error is a `*SelectError` that explains every
candidate: `vaapi: probe failed: no render node; cuda: build lacks
h264_nvenc`.

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

## Not yet

Hardware decoding. The current pipeline decodes in software and uploads,
which is safe for any input. A decode flag on `Policy` that switches to
`-hwaccel` with on-device scaling is the planned next step.
