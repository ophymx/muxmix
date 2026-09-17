# tasks

The common media jobs that take a surprising amount of ffmpeg know-how to
get right. Each probes the input first, then runs one ffmpeg command.

```go
// Poster frame 10% in, fitted to 640 wide, JPEG by extension
at, err := tasks.Thumbnail(ctx, "movie.mkv", "poster.jpg", tasks.ThumbnailOptions{Width: 640})

// Twelve evenly spaced frames
files, err := tasks.Thumbnails(ctx, "movie.mkv", "thumbs/%02d.jpg", tasks.ThumbnailsOptions{Count: 12, Width: 320})

// Scrubber previews: sprite sheets plus the WebVTT index players read
tp, err := tasks.Trickplay(ctx, "movie.mkv", "out/trick", tasks.TrickplayOptions{BaseURL: "/media/123/trick/"})

// Three-second muted preview from 30% in, 320 wide, as MP4, animated WebP or GIF
err = tasks.Preview(ctx, "movie.mkv", "preview.webp", tasks.PreviewOptions{})

// Waveform image of the audio
err = tasks.Waveform(ctx, "podcast.mp3", "wave.png", tasks.WaveformOptions{Width: 1200, Height: 160, Color: "#3b82f6"})
```

What each one handles for you:

- **Thumbnail** seeks before decoding (fast), defaults to 10% in to skip
  leaders, can let ffmpeg's `thumbnail` filter pick a representative frame,
  fits the output inside a bounding box with even dimensions, and writes
  full-range JPEG so browsers render it correctly.
- **Thumbnails** spaces frames evenly from the middle of each slot, or at a
  fixed interval, and returns the path and timestamp of every file.
- **Trickplay** picks an interval that keeps the tile count reasonable,
  derives the tile height from the display aspect ratio after rotation,
  tiles the frames onto sheets, and writes a WebVTT file whose cues point at
  `sheet-001.jpg#xywh=x,y,w,h`. Video.js, JW Player, hls.js and most other
  players consume that format directly.
- **Preview** cuts an excerpt at input-seek speed, scales it, and encodes it
  as H.264 at a fast preset, animated WebP, or GIF with a two-pass palette
  in a single filtergraph (no ugly default dithering).
- **Waveform** mixes to mono unless you want lanes per channel, and draws a
  filled envelope with `showwavespic`.

Sizing respects rotation metadata: a phone video recorded upright is
treated as portrait, which is what ffmpeg produces since it auto-rotates on
decode.

`Tools` lets you supply your own runner and prober, and run options such as
`ffmpeg.OnProgress` that apply to every task.
