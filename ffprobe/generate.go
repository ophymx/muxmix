package ffprobe

// types.gen.go and ffprobe.schema.json are produced from the FFmpeg tags
// listed in ffprobe.versions (XSDs cached under xsd/) plus the captured
// ffprobe output under testdata/probe. See internal/gen for the rules.

//go:generate go run ./internal/gen
