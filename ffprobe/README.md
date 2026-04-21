# FFProbe Package

A Go package that provides a typed interface to [FFprobe](https://ffmpeg.org/ffprobe.html) for analyzing multimedia files.

## Overview

This package generates Go types from the official FFprobe XSD schema and provides a convenient API for probing media files. It returns structured data that exactly matches FFprobe's JSON output format.

## Features

- **Type-safe**: Generated types from official FFprobe XSD schema
- **Flexible API**: Support for all FFprobe show options
- **Error handling**: Proper error types for common failure modes
- **Validation**: Built-in FFprobe installation validation

## Usage

### Basic Probing

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/ophymx/muxmix/ffprobe"
)

func main() {
    // Check if ffprobe is installed
    if err := ffprobe.ValidateInstall(); err != nil {
        log.Fatal("ffprobe not found:", err)
    }
    
    // Probe a media file (shows format and streams by default)
    result, err := ffprobe.Probe("/path/to/media.mp4")
    if err != nil {
        if ffprobe.IsUnsupported(err) {
            log.Fatal("unsupported file format")
        }
        log.Fatal("probe failed:", err)
    }
    
    // Access the results
    if result.Format != nil {
        fmt.Printf("Filename: %s\n", result.Format.Filename)
        fmt.Printf("Duration: %f seconds\n", result.Format.Duration)
    }
    
    for _, stream := range result.Streams {
        if stream.CodecType != nil {
            fmt.Printf("Stream %d: %s\n", stream.Index, *stream.CodecType)
        }
    }
}
```

### Advanced Probing with Options

```go
// Probe with specific options
result, err := ffprobe.ProbeWithOptions("/path/to/media.mp4",
    ffprobe.WithShowFormat(),
    ffprobe.WithShowStreams(),
    ffprobe.WithShowChapters(),
    ffprobe.WithShowFrames(),
    ffprobe.WithAdditionalArgs("-count_frames"),
)
if err != nil {
    log.Fatal("probe failed:", err)
}

// Access frames information
for _, frame := range result.Frames {
    if frame.MediaType != nil && *frame.MediaType == "video" {
        fmt.Printf("Frame: type=%s, size=%dx%d\n", 
            *frame.MediaType, *frame.Width, *frame.Height)
    }
}
```

### Custom Prober

```go
// Create a custom prober instance
prober := ffprobe.DefaultProber

// Use the prober
result, err := prober.ProbeWithOptions("/path/to/media.mp4",
    ffprobe.WithShowFormat(),
    ffprobe.WithShowStreams(),
)
```

## Available Options

- `WithShowFormat()` - Include format/container information
- `WithShowStreams()` - Include stream information  
- `WithShowPackets()` - Include packet information
- `WithShowFrames()` - Include frame information (can be large!)
- `WithShowChapters()` - Include chapter information
- `WithShowPrograms()` - Include program information
- `WithShowLibraryVersions()` - Include FFmpeg library versions
- `WithShowPixelFormats()` - Include available pixel formats
- `WithShowStreamGroups()` - Include stream group information
- `WithAdditionalArgs(args...)` - Add custom FFprobe arguments

## Error Handling

The package provides specific error types:

- `ErrUnsupportedFile` - File format not supported by FFprobe
- `ErrFFProbeNotFound` - FFprobe executable not found in PATH

Use the `IsUnsupported(error)` helper to check for unsupported files:

```go
result, err := ffprobe.Probe("file.unknown")
if err != nil {
    if ffprobe.IsUnsupported(err) {
        fmt.Println("This file format is not supported")
        return
    }
    log.Fatal("Other error:", err)
}
```

## Generated Types

The package includes comprehensive types generated from the official FFprobe XSD:

- `FfprobeType` - Root container for all probe data
- `FormatType` - Container/format information
- `StreamType` - Individual stream information (video, audio, subtitle, etc.)
- `ChapterType` - Chapter/segment information
- `PacketType` - Packet-level information
- `FrameType` - Individual frame information
- And many more specific types for different data structures

## Schema Source

The target FFprobe tag is stored in ffprobe.version.

The generator derives the download URL in code and caches the schema locally as
`ffprobe-<version>.xsd`.

## Generate Go Types

```sh
go generate ./ffprobe
```

If the cached `ffprobe-<version>.xsd` file is missing, the generator downloads it automatically.

## Requirements

- FFprobe (part of FFmpeg) must be installed and available in PATH
- Go 1.19 or later

## Testing

Run tests with:

```sh
go test ./ffprobe
```

Note: Some tests require FFprobe to be installed and may be skipped if not available.