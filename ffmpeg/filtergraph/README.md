FilterGraph Package
===================

A comprehensive, type-safe Go implementation for building FFmpeg filter graphs with validation and fluent API.

## Features

- **Type-Safe APIs**: Strongly typed interfaces with compile-time validation
- **Fluent Builder Pattern**: Chainable methods for easy filter construction
- **Comprehensive Validation**: Input validation with proper error messages
- **FFmpeg Compatibility**: Full support for FFmpeg filtergraph syntax
- **Common Filter Builders**: Pre-built methods for frequently used filters
- **JSON Serialization**: Full marshaling/unmarshaling support
- **Expression Support**: Mathematical expressions in filter parameters

## Basic Usage

### Simple Video Processing

```go
package main

import (
    "fmt"
    "github.com/ophymx/muxmix/ffmpeg/filtergraph"
)

func main() {
    // Create a new filtergraph
    graph := filtergraph.NewFilterGraph()
    
    // Create a simple processing chain
    chain := graph.NewChain()
    chain.Scale(1920, 1080).FPS(30.0).Format("yuv420p")
    
    fmt.Println(graph.String()) // scale=w=1920:h=1080,fps=fps=30.00,format=yuv420p
}
```

### Advanced Multi-Chain Processing

```go
func complexProcessing() {
    graph := filtergraph.NewFilterGraph().WithSwsFlags("lanczos")
    
    // Video processing chain
    video := graph.NewChain()
    video.Input("0:v").
          Scale(1920, 1080).
          FPS(30.0).
          Crop(100, 100, 10, 10).
          Format("yuv420p").
          Output("video_out")
    
    // Audio processing chain
    audio := graph.NewChain()
    audio.Input("0:a").
          Volume(0.8).
          Output("audio_out")
    
    fmt.Println(graph.String())
    // Output: sws_flags=lanczos;[0:v]scale=w=1920:h=1080,fps=fps=30.00,crop=w=100:h=100:x=10:y=10,format=yuv420p[video_out];[0:a]volume=volume=0.80[audio_out]
}
```

### Video Overlay Example

```go
func videoOverlay() {
    graph := filtergraph.NewFilterGraph()
    
    // Main video input
    graph.NewChain().Input("0:v").Output("main")
    
    // Overlay video - scale to smaller size
    graph.NewChain().Input("1:v").Scale(320, 240).Output("overlay")
    
    // Combine both videos with overlay
    graph.NewChain().
          Input("main", "overlay").
          Overlay("10", "10").  // Position at 10,10
          Output("result")
    
    fmt.Println(graph.String())
    // Output: [0:v]null[main];[1:v]null,scale=w=320:h=240[overlay];[main][overlay]null,overlay=x=10:y=10[result]
}
```

## Common Filter Methods

The package includes convenient builder methods for frequently used filters:

### Video Filters

```go
chain := filtergraph.NewFilterChain()

// Scale video to specific resolution
chain.Scale(1920, 1080)

// Scale using expressions
chain.ScaleExpression("iw/2", "ih/2") // Half size

// Set frame rate
chain.FPS(30.0)

// Crop video
chain.Crop(640, 480, 10, 20) // width, height, x, y

// Crop using expressions
chain.CropExpression("iw/4", "ih/4", "iw/8", "ih/8")

// Set pixel format
chain.Format("yuv420p")

// Add fade effect
chain.Fade("in", 0, 30) // type, start_frame, nb_frames

// Video overlay
chain.Overlay("10", "20") // x, y position
```

### Audio Filters

```go
// Adjust volume
chain.Volume(0.8) // 80% volume

// Volume with expression
chain.VolumeExpression("0.5*sin(2*PI*t)") // Sine wave modulation
```

## Custom Filters

For filters not covered by convenience methods, use the generic filter builder:

```go
// Using key-value arguments
filter := filtergraph.NewFilter("convolution").
    WithKVArgs(map[string]string{
        "0m": "1 0 -1",
        "0v": "1 0 -1", 
        "1m": "1 0 -1",
        "1v": "1 0 -1",
    })

// Using positional arguments  
filter := filtergraph.NewFilter("format").
    WithPositionalArgs([]string{"yuv420p", "yuv444p"})

// Add to chain
chain.Add(filter)
```

## Filter Instances

For complex graphs requiring filter reuse:

```go
filter := filtergraph.NewFilter("scale").
    WithInstance("main_scaler").
    WithKVArgs(map[string]string{"w": "1920", "h": "1080"})

// Results in: scale@main_scaler=w=1920:h=1080
```

## Validation

The package provides comprehensive validation:

```go
graph := filtergraph.NewFilterGraph()
chain := graph.NewChain()
chain.Scale(1920, 1080).FPS(30.0)

if err := graph.Validate(); err != nil {
    log.Fatalf("Invalid filtergraph: %v", err)
}
```

Validation checks include:
- Filter name validity
- Instance name format
- Input/output label format and matching
- Argument syntax
- Graph connectivity

## JSON Serialization

Full support for JSON marshaling/unmarshaling:

```go
graph := filtergraph.NewFilterGraph()
chain := graph.NewChain()
chain.Scale(1920, 1080).FPS(30.0)

// Serialize to JSON
data, err := json.MarshalIndent(graph, "", "  ")
if err != nil {
    log.Fatal(err)
}

// Deserialize from JSON
var newGraph filtergraph.FilterGraph
err = json.Unmarshal(data, &newGraph)
if err != nil {
    log.Fatal(err)
}
```

## Expression Support

Mathematical expressions are supported in filter parameters:

```go
chain := filtergraph.NewFilterChain()

// Scale to half the input size
chain.ScaleExpression("iw/2", "ih/2")

// Crop from center
chain.CropExpression("iw/4", "ih/4", "iw/8", "ih/8")

// Dynamic volume based on time
chain.VolumeExpression("0.5*sin(2*PI*t)")
```

## Error Handling

The package provides detailed error information for debugging:

```go
if err := graph.Validate(); err != nil {
    // Error messages include context:
    // "chain 0 validation failed: filter 1 validation failed: invalid filter name: bad-name"
    log.Printf("Validation error: %v", err)
}
```

## Performance Considerations

- Filter graphs are built in memory and converted to strings when needed
- Validation is performed separately from string generation
- Large graphs with many chains are supported efficiently
- JSON serialization preserves all graph structure and metadata

## Integration with FFmpeg

Use the generated filtergraph strings directly with FFmpeg:

```bash
ffmpeg -i input.mp4 -filter_complex "scale=w=1920:h=1080,fps=fps=30.00" output.mp4
```

Or programmatically:

```go
graph := filtergraph.NewFilterGraph()
// ... build graph ...

cmd := exec.Command("ffmpeg", 
    "-i", "input.mp4",
    "-filter_complex", graph.String(),
    "output.mp4")
```