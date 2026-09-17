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

## Transcode

`Transcode` turns a description of the result into the right `-map`,
codec and filter options for every stream, and tells you what it decided:

```go
plan, err := tasks.Transcode(ctx, "in.mkv", "out.mp4", tasks.TranscodeOptions{
    Video: tasks.VideoRule{
        CopyCodecs: tasks.AllCodecs,                                  // keep H.264/HEVC as-is
        Encode:     encode.Video{Codec: encode.H264, Quality: 22},    // otherwise
        MaxHeight:  1080,                                             // scale down 4K
    },
    Audio: tasks.AudioRule{CopyCodecs: tasks.AllCodecs, Languages: []string{"eng", "jpn"}},
    Caps:  set, // choose encoders from this build and validate before running
})
fmt.Print(plan)
// 0 video h264: copy (codec accepted by container)
// 1 audio aac [eng]: copy (codec accepted by container)
// 2 audio opus [deu]: drop (language not selected)
// 3 subtitle subrip [eng]: encode → mov_text (converted for container)
// 4 attachment : drop (attachments are not carried)
```

It knows which codecs each container accepts for copying (MP4 will not
take Opus or FLAC without player trouble, WebM takes only VP8/VP9/AV1 and
Opus/Vorbis), converts text subtitles to the container's format and drops
bitmap ones it cannot hold, skips cover art unless asked, keeps only the
main video and optionally the main audio, filters by language, carries
global metadata and chapters unless told not to, and adds `+faststart` for
MP4. `PlanTranscode` returns the plan and `Command` without running, so
you can inspect or adjust it first; `BuildTranscodePlan` works from an
existing probe result.

## HLS and DASH packaging

`Package` encodes a bitrate ladder in one pass and writes the playlists:

```go
res, err := tasks.Package(ctx, "movie.mkv", "out/movie", tasks.PackageOptions{
    Format:         tasks.HLS,              // or DASH, or CMAF for both over one segment set
    Renditions:     tasks.DefaultLadder(1080), // 1080p/720p/480p/360p, or your own rungs
    AudioLanguages: []string{"eng", "jpn"}, // alternate audio renditions; default: main audio
    Video:          encode.Video{Speed: encode.Fast},
    Caps:           set,
})
// res.Master → out/movie/master.m3u8, res.Renditions[i].Playlist, res.Audio[j].Playlist
```

The source is decoded once and split, each rung is scaled and encoded
with its own bit rate and VBV settings, keyframes are forced on every
segment boundary so renditions switch cleanly, audio goes into an
`EXT-X-MEDIA` group rather than being duplicated per variant, and each
variant lands in its own directory with an `init` segment and numbered
fMP4 (or MPEG-TS) segments. DASH output uses segment templates with a
timeline; CMAF adds the HLS playlists to the same segments.

What each of the others handles for you:

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
