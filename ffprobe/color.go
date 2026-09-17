package ffprobe

import (
	"context"
	"strconv"
)

// ColorInfo summarises a video stream's colour signalling and HDR
// metadata. Mastering, LightLevel, DolbyVision and HDR10Plus come from the
// stream header when the container carries them, otherwise from the first
// decoded frame (see FromFrame).
type ColorInfo struct {
	PixFmt    string
	Range     string // "tv", "pc"
	Space     string // "bt709", "bt2020nc", ...
	Primaries string // "bt709", "bt2020", ...
	Transfer  string // "bt709", "smpte2084" (PQ), "arib-std-b67" (HLG), ...

	Mastering   *MasteringDisplay
	LightLevel  *ContentLightLevel
	DolbyVision *DolbyVisionConfig
	HDR10Plus   bool

	// FromFrame reports that the first frame was decoded to fill in
	// metadata the stream header did not carry.
	FromFrame bool
}

// IsHDR reports a PQ or HLG transfer, HDR mastering or light level
// metadata, Dolby Vision or HDR10+.
func (c *ColorInfo) IsHDR() bool {
	if c == nil {
		return false
	}
	switch c.Transfer {
	case "smpte2084", "arib-std-b67":
		return true
	}
	return c.Mastering != nil || c.LightLevel != nil || c.DolbyVision != nil || c.HDR10Plus
}

// hasStaticHDR reports whether any HDR side data has been found yet.
func (c *ColorInfo) hasStaticHDR() bool {
	return c.Mastering != nil || c.LightLevel != nil || c.DolbyVision != nil || c.HDR10Plus
}

// colorOf reads the stream-level colour fields and side data.
func colorOf(s *Stream) *ColorInfo {
	return &ColorInfo{
		PixFmt:      s.PixFmt,
		Range:       s.ColorRange,
		Space:       s.ColorSpace,
		Primaries:   s.ColorPrimaries,
		Transfer:    s.ColorTransfer,
		Mastering:   s.MasteringDisplay(),
		LightLevel:  s.ContentLightLevel(),
		DolbyVision: s.DolbyVision(),
		HDR10Plus:   s.HDR10Plus(),
	}
}

// mergeFrame fills fields the stream did not provide from a decoded frame.
func (c *ColorInfo) mergeFrame(f *Frame) {
	c.FromFrame = true
	if c.PixFmt == "" {
		c.PixFmt = f.PixFmt
	}
	if c.Range == "" {
		c.Range = f.ColorRange
	}
	if c.Space == "" {
		c.Space = f.ColorSpace
	}
	if c.Primaries == "" {
		c.Primaries = f.ColorPrimaries
	}
	if c.Transfer == "" {
		c.Transfer = f.ColorTransfer
	}
	if c.Mastering == nil {
		c.Mastering = f.MasteringDisplay()
	}
	if c.LightLevel == nil {
		c.LightLevel = f.ContentLightLevel()
	}
	if !c.HDR10Plus {
		c.HDR10Plus = f.HDR10Plus()
	}
}

// Color describes the colour and HDR signalling of the main video stream.
// Static HDR metadata (mastering display, content light level, HDR10+) is
// often carried only as per-frame SEI, which the stream section never
// shows; when the stream header has none, Color decodes the first frame of
// that stream and reads it from there. Options such as InputFormat or
// Probesize are passed to both probes.
//
//	c, err := ffprobe.Color(ctx, "movie.mkv")
//	if err == nil && c.IsHDR() { ... }
func (p *Prober) Color(ctx context.Context, input string, opts ...Option) (*ColorInfo, error) {
	res, err := p.Probe(ctx, input, append(opts, ShowStreams())...)
	if err != nil {
		return nil, err
	}
	s := res.VideoStream()
	if s == nil {
		return nil, nil
	}
	c := colorOf(s)
	if c.hasStaticHDR() {
		return c, nil
	}
	frameOpts := append(opts,
		SelectStreams(strconv.FormatInt(s.Index.Int64(), 10)),
		ReadIntervals("%+#1"))
	for f, err := range p.Frames(ctx, input, frameOpts...) {
		if err != nil {
			return nil, err
		}
		c.mergeFrame(f)
		break
	}
	return c, nil
}

// Color runs the default Prober's Color.
func Color(ctx context.Context, input string, opts ...Option) (*ColorInfo, error) {
	return Default.Color(ctx, input, opts...)
}
