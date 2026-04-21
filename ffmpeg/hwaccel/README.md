HWAccel Package
===============

This package helps answer two different questions about FFmpeg hardware acceleration:

1. What hardware acceleration backends and encoders was this FFmpeg binary built with?
2. Which of those backends can actually be initialized and used on the current machine?

That distinction matters because `ffmpeg -hwaccels` only reports compiled-in libraries. It does not prove that the host has the right driver stack, device nodes, permissions, or runtime libraries needed to use a backend successfully.

What The Package Does
---------------------

The package is split into two layers.

### Build-Time Detection

`Detect` runs FFmpeg discovery commands and parses their output into a `Support` value.

- `-hwaccels` is parsed into supported backend kinds such as `vaapi`, `cuda`, `qsv`, and `videotoolbox`
- `-encoders` is parsed into the available video encoder names

This answers whether the FFmpeg binary knows about a backend and whether codec-specific encoders such as `h264_vaapi` or `hevc_nvenc` exist.

### Runtime Detection

`DetectSystem` builds on `Detect` and then runs backend-specific probe commands through FFmpeg itself.

Each probe attempts to initialize a hardware device and execute a tiny synthetic pipeline. A backend is considered usable only if that probe succeeds.

For example:

- `vaapi` is probed with `-init_hw_device vaapi=...` and a small `hwupload` pipeline
- `cuda` is probed with `-init_hw_device cuda=...` and a small `hwupload_cuda` pipeline
- `qsv` is probed with `-init_hw_device qsv=...`

This catches the practical failures that `-hwaccels` alone misses, such as:

- missing `/dev/dri/renderD*` nodes
- missing permissions on GPU device nodes
- driver/runtime mismatches
- backend initialization failures even though FFmpeg was compiled with support

Core Types
----------

### `Kind`

Normalized hardware acceleration backend name.

- `VAAPI`
- `CUDA`
- `QSV`
- `VideoToolbox`
- `Auto`
- `None`

### `Support`

Represents build-time FFmpeg support.

- `Accels` is the set of compiled-in backends
- `Encoders` is the set of compiled-in video encoders

Useful methods:

- `Has(kind)`
- `SupportsCodec(kind, codec)`
- `Kinds()`

### `ProbeResult`

Describes the outcome of a runtime probe for one backend/device candidate.

- `Kind` is the backend being tested
- `Device` is the device candidate, when relevant
- `Args` is the probe FFmpeg command args that were attempted
- `Available` reports probe success
- `Error` contains stderr or failure text when probing fails

### `SystemSupport`

Represents runtime-usable support.

- `Built` keeps the original build-time `Support`
- `Probes` stores all probe attempts and outcomes grouped by backend

Useful methods:

- `Available(kind)`
- `Device(kind)`
- `SupportsCodec(kind, codec)`
- `Select(codec, preferred...)`

`SystemSupport.SupportsCodec` is stricter than `Support.SupportsCodec`: it only returns true when the encoder exists and the backend actually probed successfully on this machine.

Common Flow
-----------

Typical usage should look like this:

1. Run `DetectSystem`.
2. Ask `SystemSupport` which backend is usable for the target codec.
3. Build the FFmpeg command args for that backend.

Example:

```go
ctx := context.Background()

system, err := hwaccel.DetectSystem(ctx, ffmpeg.DefaultRunner, hwaccel.ProbeOptions{})
if err != nil {
    return err
}

kind, device, err := system.Select("h264", hwaccel.VAAPI, hwaccel.CUDA, hwaccel.QSV)
if err != nil {
    return err
}

args, err := hwaccel.BuildEncodeArgs(kind, "h264", device, "scale=1280:-2")
if err != nil {
    return err
}

// Append input/output args around the hardware-specific args.
_ = args
```

Builder Helpers
---------------

Once a backend is chosen, the package provides helpers to build consistent FFmpeg argument fragments.

- `BuildInputArgs(kind, device)`
- `BuildFilter(kind, filters...)`
- `BuildVideoCodec(kind, codec)`
- `BuildEncodeArgs(kind, codec, device, filters...)`

These helpers encode the backend-specific conventions already needed by FFmpeg, for example:

- VAAPI uses `-vaapi_device` plus `format=nv12,hwupload`
- CUDA uses `-hwaccel cuda` and `hwupload_cuda`
- QSV uses `-hwaccel qsv` plus `format=nv12,hwupload`

Device Discovery
----------------

`DefaultDevices` returns backend-specific device candidates for probing.

Current behavior:

- `vaapi` and `qsv` on Linux look for `/dev/dri/renderD*`
- `cuda` and `videotoolbox` currently use a single empty-device probe candidate

`ProbeOptions` lets callers override or restrict that behavior:

- `Kinds` limits which backends are tested
- `Devices` supplies explicit per-backend device candidates

What This Package Does Not Do
-----------------------------

This package does not try to benchmark backends or choose the fastest one.

It also does not build a complete FFmpeg command line for an entire transcode job. It only builds the hardware-acceleration-specific pieces so the calling code can compose them with normal input, mapping, audio, and output options.

Current Goal
------------

The package is meant to be the narrow place where hardware acceleration policy lives:

- normalize backend names
- detect what FFmpeg says it supports
- verify what the current system can actually use
- select a usable backend for a target codec
- build the backend-specific FFmpeg arguments consistently