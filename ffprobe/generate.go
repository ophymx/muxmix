package ffprobe

// The target FFprobe tag is stored in ffprobe.version.
// The versioned XSD is downloaded and cached locally when missing.

//go:generate go run ./cmd/gen-schema ffprobe.schema.json
//go:generate go-jsonschema -p ffprobe -o types.gen.go ffprobe.schema.json
