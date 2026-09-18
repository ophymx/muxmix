package ffprobe

// types.gen.go and ffprobe.schema.json are produced from the FFmpeg tags
// listed in ffprobe.versions (XSDs cached under xsd/) plus the captured
// ffprobe output under testdata/probe. See internal/gen for the rules.

// The generator lives in its own module so that muxmix has no
// dependencies of its own; -dir points it back at this directory.

//go:generate go -C internal/gen run . -dir ../..
