// Package hwaccel reports which hardware acceleration backends an ffmpeg
// build has, which of them actually work on this machine, and how to
// encode with one.
//
// "ffmpeg -hwaccels" only lists what was compiled in, and neither it nor
// the build's encoder list says what the silicon implements: a build
// carries av1_vaapi on a chip with no AV1 encoder, and av1_nvenc on a GPU
// older than Ada. So Detect initialises each backend for real by running a
// tiny pipeline, and on each device that works encodes one frame with
// every encoder the backend offers -- in 8-bit and in 10-bit, since a
// device that encodes HEVC at all may still only encode it 8-bit --
// recording every outcome:
//
//	sys, fromCache, err := hwaccel.DetectCached(ctx, nil, cachePath, 24*time.Hour, hwaccel.ProbeOptions{})
//
// Detection costs a few seconds, so do it once at startup and share the
// System; DetectCached keeps it on disk until the ffmpeg version, the
// cache's age or the host's device nodes change. ProbeOptions.Codecs
// narrows the encoder probes to the codecs an application encodes, and
// NoEncoderProbe skips them for a faster, less certain answer.
//
// Select resolves a codec to a Selection carrying the backend, device and
// encoder, and renders its own pieces of the command. The pipeline is
// software decode, upload, hardware encode, which works for any input:
//
//	sel, err := sys.Select("h264", hwaccel.CUDA, hwaccel.VAAPI)
//	sel.Apply(cmd) // -init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw
//	cmd.Output("out.mp4", sel.Opts("v:0", "scale=1280:-2")...)
//
// A hardware pipeline uploads 8-bit unless told otherwise, which would
// flatten a 10-bit master on a device that could have kept it, so a
// Selection for a deep source goes through PreserveDepth:
//
//	sel = sys.PreserveDepth(sel, stream.PixFmt) // hevc_vaapi p010 on /dev/dri/renderD128
//
// It sets the upload format the probes proved, and flattens to nv12
// explicitly when the encoder has no 10-bit path. Package tasks calls it
// for every stream it encodes.
//
// The zero Selection means software: Apply adds nothing and Opts omits
// the codec so the caller picks a software encoder. When no backend
// fits, the error is a *SelectError explaining every candidate, down to
// the driver's own words for an encoder that did not run.
//
// Callers that want a fallback rather than a failure state a Policy
// (Software, PreferHardware or RequireHardware) and Resolve it, which
// also returns the reason hardware was not used; package tasks takes one
// per job. Backends are pluggable through Backend and Register.
package hwaccel
