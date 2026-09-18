// Package hwaccel reports which hardware acceleration backends an ffmpeg
// build has, which of them actually work on this machine, and how to
// encode with one.
//
// "ffmpeg -hwaccels" only lists what was compiled in. Whether a backend
// runs depends on the host's drivers, device nodes and permissions, so
// Detect initialises each one for real by running a tiny pipeline and
// records the outcome per device:
//
//	sys, fromCache, err := hwaccel.DetectCached(ctx, nil, cachePath, 24*time.Hour, hwaccel.ProbeOptions{})
//
// Detection costs about a second, so do it once at startup and share the
// System; DetectCached keeps it on disk until the ffmpeg version, the
// cache's age or the host's device nodes change.
//
// Select resolves a codec to a Selection carrying the backend, device and
// encoder, and renders its own pieces of the command. The pipeline is
// software decode, upload, hardware encode, which works for any input:
//
//	sel, err := sys.Select("h264", hwaccel.CUDA, hwaccel.VAAPI)
//	sel.Apply(cmd) // -init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw
//	cmd.Output("out.mp4", sel.Opts("v:0", "scale=1280:-2")...)
//
// The zero Selection means software: Apply adds nothing and Opts omits
// the codec so the caller picks a software encoder. When no backend
// fits, the error is a *SelectError explaining every candidate.
//
// Callers that want a fallback rather than a failure state a Policy
// (Software, PreferHardware or RequireHardware) and Resolve it, which
// also returns the reason hardware was not used; package tasks takes one
// per job. Backends are pluggable through Backend and Register.
package hwaccel
