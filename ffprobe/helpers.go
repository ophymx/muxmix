package ffprobe

import (
	"strconv"
	"strings"
	"time"
)

// Values of Stream.CodecType and Frame.MediaType.
const (
	MediaTypeVideo      = "video"
	MediaTypeAudio      = "audio"
	MediaTypeSubtitle   = "subtitle"
	MediaTypeData       = "data"
	MediaTypeAttachment = "attachment"
)

// ─── Result ────────────────────────────────────────────────────────────────

// StreamsOfType returns the streams whose codec_type matches.
func (r *Result) StreamsOfType(codecType string) []*Stream {
	var out []*Stream
	for _, s := range r.Streams {
		if s.CodecType == codecType {
			out = append(out, s)
		}
	}
	return out
}

// VideoStreams returns every video stream, including attached pictures.
func (r *Result) VideoStreams() []*Stream { return r.StreamsOfType(MediaTypeVideo) }

// AudioStreams returns every audio stream.
func (r *Result) AudioStreams() []*Stream { return r.StreamsOfType(MediaTypeAudio) }

// SubtitleStreams returns every subtitle stream.
func (r *Result) SubtitleStreams() []*Stream { return r.StreamsOfType(MediaTypeSubtitle) }

// VideoStream returns the main video stream: the first video stream that is
// not an attached picture (cover art), or nil.
func (r *Result) VideoStream() *Stream {
	for _, s := range r.VideoStreams() {
		if !s.IsAttachedPic() {
			return s
		}
	}
	return nil
}

// AudioStream returns the main audio stream: the first audio stream flagged
// default, else the first audio stream, or nil.
func (r *Result) AudioStream() *Stream {
	streams := r.AudioStreams()
	for _, s := range streams {
		if s.Disposition.Default.Bool() {
			return s
		}
	}
	if len(streams) > 0 {
		return streams[0]
	}
	return nil
}

// Stream returns the stream with the given index, or nil.
func (r *Result) Stream(index int) *Stream {
	for _, s := range r.Streams {
		if s.Index.Int() == index {
			return s
		}
	}
	return nil
}

// Duration returns the container duration, falling back to the longest
// stream duration when the format section lacks one.
func (r *Result) Duration() time.Duration {
	if r.Format.DurationSecs.Valid() {
		return r.Format.Duration()
	}
	var max time.Duration
	for _, s := range r.Streams {
		if d := s.Duration(); d > max {
			max = d
		}
	}
	return max
}

// HasVideo reports whether a real (non cover art) video stream exists.
func (r *Result) HasVideo() bool { return r.VideoStream() != nil }

// HasAudio reports whether an audio stream exists.
func (r *Result) HasAudio() bool { return len(r.AudioStreams()) > 0 }

// ─── Stream ────────────────────────────────────────────────────────────────

// IsVideo reports whether the stream is video (including attached pictures).
func (s *Stream) IsVideo() bool { return s.CodecType == MediaTypeVideo }

// IsAudio reports whether the stream is audio.
func (s *Stream) IsAudio() bool { return s.CodecType == MediaTypeAudio }

// IsSubtitle reports whether the stream is a subtitle track.
func (s *Stream) IsSubtitle() bool { return s.CodecType == MediaTypeSubtitle }

// IsAttachedPic reports whether the stream is cover art rather than a movie.
func (s *Stream) IsAttachedPic() bool { return s.Disposition.AttachedPic.Bool() }

// IsDefault reports the default disposition flag.
func (s *Stream) IsDefault() bool { return s.Disposition.Default.Bool() }

// IsForced reports the forced disposition flag.
func (s *Stream) IsForced() bool { return s.Disposition.Forced.Bool() }

// FrameRate returns the average frame rate when known, else the nominal
// r_frame_rate. Audio streams and attached pictures (whose r_frame_rate is
// the meaningless 90000/1 of their time base) return an invalid Rat.
func (s *Stream) FrameRate() Rat {
	if s.IsAttachedPic() {
		return Rat{}
	}
	if s.AvgFrameRate.Valid() && s.AvgFrameRate.Num() != 0 {
		return s.AvgFrameRate
	}
	return s.RFrameRate
}

// Resolution returns width and height in pixels (0, 0 when unknown).
func (s *Stream) Resolution() (width, height int) {
	return s.Width.Int(), s.Height.Int()
}

// Duration returns the stream duration: the duration field, else
// duration_ts scaled by the time base, else the DURATION tag Matroska
// writes (mkvmerge and ffmpeg both store it as "HH:MM:SS.nnnnnnnnn").
// It is 0 when none of them is present, as for raw elementary streams.
func (s *Stream) Duration() time.Duration {
	if s.DurationSecs.Valid() {
		return s.DurationSecs.Duration()
	}
	if s.DurationTS.Valid() && s.TimeBase.Valid() {
		return s.TimeBase.Duration(s.DurationTS)
	}
	if v, ok := s.Tags.Get("duration"); ok {
		if d, err := parseClockDuration(v); err == nil {
			return d
		}
	}
	return 0
}

// StartTime returns the stream start time: the start_time field, else
// start_pts scaled by the time base, else 0.
func (s *Stream) StartTime() time.Duration {
	if s.StartSecs.Valid() {
		return s.StartSecs.Duration()
	}
	if s.StartPTS.Valid() && s.TimeBase.Valid() {
		return s.TimeBase.Duration(s.StartPTS)
	}
	return 0
}

// Bitrate returns the stream bit rate in bits per second: the bit_rate
// field, else the BPS tag that mkvmerge writes into Matroska statistics
// (also spelled BPS-eng), else 0.
func (s *Stream) Bitrate() int64 {
	if s.BitRate.Valid() {
		return s.BitRate.Int64()
	}
	return s.tagInt("BPS")
}

// FrameCount returns the number of frames in the stream: nb_frames (which
// only some containers record), else nb_read_frames (filled by
// CountFrames), else the NUMBER_OF_FRAMES tag from Matroska statistics
// (also spelled NUMBER_OF_FRAMES-eng), else 0.
func (s *Stream) FrameCount() int64 {
	if s.NbFrames.Valid() {
		return s.NbFrames.Int64()
	}
	if s.NbReadFrames.Valid() {
		return s.NbReadFrames.Int64()
	}
	return s.tagInt("NUMBER_OF_FRAMES")
}

// tagInt reads an integer tag, also trying the "-eng" suffixed spelling
// mkvmerge uses for language-tagged statistics.
func (s *Stream) tagInt(key string) int64 {
	for _, k := range []string{key, key + "-eng"} {
		if v, ok := s.Tags.Get(k); ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return n
			}
		}
	}
	return 0
}

// Language returns the ISO 639 language tag, or "" when untagged.
func (s *Stream) Language() string { return s.Tags.Value("language") }

// Title returns the stream title tag, or "".
func (s *Stream) Title() string { return s.Tags.Value("title") }

// SideData returns the first side data entry of the given side_data_type
// (for example "Display Matrix"), or nil.
func (s *Stream) SideData(sideDataType string) *SideData {
	for _, sd := range s.SideDataList {
		if sd.SideDataType == sideDataType {
			return sd
		}
	}
	return nil
}

// Rotation returns the clockwise rotation, in degrees, that a player
// applies before display, normalised to 0, 90, 180 or 270. It reads the
// Display Matrix side data first and then the legacy "rotate" tag that
// ffprobe 4.4 and older MP4 files carry. Streams rotated by 90 or 270
// degrees display with Width and Height swapped.
func (s *Stream) Rotation() int {
	if dm := s.DisplayMatrix(); dm != nil && dm.Rotation.Valid() {
		return dm.Degrees()
	}
	if v, ok := s.Tags.Get("rotate"); ok {
		if deg, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return normaliseDegrees(int(deg))
		}
	}
	return 0
}

// normaliseDegrees maps any angle onto 0, 90, 180 or 270.
func normaliseDegrees(deg int) int {
	deg = ((deg % 360) + 360) % 360
	return (deg + 45) / 90 % 4 * 90
}

// ─── Format ────────────────────────────────────────────────────────────────

// Formats returns the comma-separated format_name split into its aliases,
// for example ["mov", "mp4", "m4a", "3gp", "3g2", "mj2"].
func (f *Format) Formats() []string {
	if f.FormatName == "" {
		return nil
	}
	var out []string
	for _, s := range splitComma(f.FormatName) {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Is reports whether the container matches one of the format_name aliases.
func (f *Format) Is(name string) bool {
	for _, s := range f.Formats() {
		if s == name {
			return true
		}
	}
	return false
}

// Duration returns the container duration, or 0 when ffprobe printed none.
func (f *Format) Duration() time.Duration { return f.DurationSecs.Duration() }

// StartTime returns the container start time, or 0 when ffprobe printed
// none.
func (f *Format) StartTime() time.Duration { return f.StartSecs.Duration() }

// ─── Chapter ───────────────────────────────────────────────────────────────

// StartTime returns the chapter start: start_time, else start scaled by
// the time base.
func (c *Chapter) StartTime() time.Duration {
	if c.StartSecs.Valid() {
		return c.StartSecs.Duration()
	}
	return c.TimeBase.Duration(c.Start)
}

// EndTime returns the chapter end: end_time, else end scaled by the time
// base.
func (c *Chapter) EndTime() time.Duration {
	if c.EndSecs.Valid() {
		return c.EndSecs.Duration()
	}
	return c.TimeBase.Duration(c.End)
}

// Duration returns the chapter length.
func (c *Chapter) Duration() time.Duration { return c.EndTime() - c.StartTime() }

// Title returns the chapter title tag, or "".
func (c *Chapter) Title() string { return c.Tags.Value("title") }

// ─── Program ───────────────────────────────────────────────────────────────

// StartTime returns the program start time, or 0 when absent.
func (p *Program) StartTime() time.Duration { return p.StartSecs.Duration() }

// EndTime returns the program end time, or 0 when absent.
func (p *Program) EndTime() time.Duration { return p.EndSecs.Duration() }

// ─── Frame ─────────────────────────────────────────────────────────────────

// IsVideo reports whether the frame belongs to a video stream.
func (f *Frame) IsVideo() bool { return f.MediaType == MediaTypeVideo }

// IsAudio reports whether the frame belongs to an audio stream.
func (f *Frame) IsAudio() bool { return f.MediaType == MediaTypeAudio }

// IsSubtitle reports whether the frame is a subtitle event.
func (f *Frame) IsSubtitle() bool { return f.MediaType == MediaTypeSubtitle }

// Time returns the presentation time. It prefers pts_time, then the
// pkt_pts_time that ffprobe 4.4 printed instead, then the best-effort
// timestamp.
func (f *Frame) Time() time.Duration {
	switch {
	case f.PTSSecs.Valid():
		return f.PTSSecs.Duration()
	case f.PktPTSSecs.Valid():
		return f.PktPTSSecs.Duration()
	default:
		return f.BestEffortTimestampSecs.Duration()
	}
}

// Duration returns the frame duration, reading duration_time (ffprobe 6.0
// and newer) or pkt_duration_time (older releases).
func (f *Frame) Duration() time.Duration {
	if f.DurationSecs.Valid() {
		return f.DurationSecs.Duration()
	}
	return f.PktDurationSecs.Duration()
}

// PacketPosition returns the byte offset of the packet the frame came from.
func (f *Frame) PacketPosition() int64 { return f.PktPos.Int64() }

// ─── Packet ────────────────────────────────────────────────────────────────

// Packet flag characters as ffprobe prints them in the flags field.
const (
	packetFlagKey     = 'K'
	packetFlagDiscard = 'D'
	packetFlagCorrupt = 'C'
)

// IsKeyframe reports whether the packet flags contain "K".
func (p *Packet) IsKeyframe() bool { return strings.ContainsRune(p.Flags, packetFlagKey) }

// IsDiscard reports whether the packet flags mark it for discard ("D"),
// as for the priming samples at the start of an AAC stream.
func (p *Packet) IsDiscard() bool { return strings.ContainsRune(p.Flags, packetFlagDiscard) }

// IsCorrupt reports whether the packet flags mark it as corrupt ("C").
func (p *Packet) IsCorrupt() bool { return strings.ContainsRune(p.Flags, packetFlagCorrupt) }

// Time returns the presentation time, preferring pts_time over dts_time.
func (p *Packet) Time() time.Duration {
	if p.PTSSecs.Valid() {
		return p.PTSSecs.Duration()
	}
	return p.DTSSecs.Duration()
}

// Duration returns the packet duration, or 0 when ffprobe printed none.
func (p *Packet) Duration() time.Duration { return p.DurationSecs.Duration() }
