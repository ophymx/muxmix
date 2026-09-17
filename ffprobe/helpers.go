package ffprobe

import (
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
	for i := range r.Streams {
		if r.Streams[i].CodecType == codecType {
			out = append(out, &r.Streams[i])
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
		if s.Disposition != nil && s.Disposition.Default.Bool() {
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
	for i := range r.Streams {
		if r.Streams[i].Index.Int() == index {
			return &r.Streams[i]
		}
	}
	return nil
}

// Duration returns the container duration, falling back to the longest
// stream duration when the format section lacks one.
func (r *Result) Duration() time.Duration {
	if r.Format != nil && r.Format.Duration.Valid() {
		return r.Format.Duration.Duration()
	}
	var max time.Duration
	for i := range r.Streams {
		if d := r.Streams[i].DurationOf(); d > max {
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
func (s *Stream) IsAttachedPic() bool {
	return s.Disposition != nil && s.Disposition.AttachedPic.Bool()
}

// IsDefault reports the default disposition flag.
func (s *Stream) IsDefault() bool {
	return s.Disposition != nil && s.Disposition.Default.Bool()
}

// IsForced reports the forced disposition flag.
func (s *Stream) IsForced() bool {
	return s.Disposition != nil && s.Disposition.Forced.Bool()
}

// FrameRate returns the average frame rate when known, else the nominal
// r_frame_rate. Audio streams return an invalid Rat.
func (s *Stream) FrameRate() Rat {
	if s.AvgFrameRate.Valid() && s.AvgFrameRate.Num() != 0 {
		return s.AvgFrameRate
	}
	return s.RFrameRate
}

// Resolution returns width and height in pixels (0, 0 when unknown).
func (s *Stream) Resolution() (width, height int) {
	return s.Width.Int(), s.Height.Int()
}

// DurationOf returns the stream duration, derived from duration_ts and the
// time base when the seconds field is absent, or the DURATION tag Matroska
// writes when neither is present.
func (s *Stream) DurationOf() time.Duration {
	if s.Duration.Valid() {
		return s.Duration.Duration()
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

// StartOf returns the stream start time, using start_pts and the time base
// when the seconds field is absent.
func (s *Stream) StartOf() time.Duration {
	if s.StartTime.Valid() {
		return s.StartTime.Duration()
	}
	if s.StartPTS.Valid() && s.TimeBase.Valid() {
		return s.TimeBase.Duration(s.StartPTS)
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
	for i := range s.SideDataList {
		if s.SideDataList[i].SideDataType == sideDataType {
			return &s.SideDataList[i]
		}
	}
	return nil
}

// Rotation returns the display rotation in degrees from Display Matrix side
// data, or 0. ffmpeg reports counter-clockwise rotation as negative; see
// DisplayMatrix.Degrees for a normalised clockwise value.
func (s *Stream) Rotation() int {
	if dm := s.DisplayMatrix(); dm != nil {
		return int(dm.Rotation.Float64())
	}
	return 0
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
	case f.PTSTime.Valid():
		return f.PTSTime.Duration()
	case f.PktPTSTime.Valid():
		return f.PktPTSTime.Duration()
	default:
		return f.BestEffortTimestampTime.Duration()
	}
}

// DurationOf returns the frame duration, reading duration_time (ffprobe 6.0
// and newer) or pkt_duration_time (older releases).
func (f *Frame) DurationOf() time.Duration {
	if f.DurationTime.Valid() {
		return f.DurationTime.Duration()
	}
	return f.PktDurationTime.Duration()
}

// PacketPosition returns the byte offset of the packet the frame came from.
func (f *Frame) PacketPosition() int64 { return f.PktPos.Int64() }

// ─── Packet ────────────────────────────────────────────────────────────────

// IsKeyframe reports whether the packet flags contain "K".
func (p *Packet) IsKeyframe() bool { return len(p.Flags) > 0 && p.Flags[0] == 'K' }

// IsDiscard reports whether the packet flags mark it for discard ("D").
func (p *Packet) IsDiscard() bool { return len(p.Flags) > 1 && p.Flags[1] == 'D' }

// Time returns the presentation time, preferring pts_time over dts_time.
func (p *Packet) Time() time.Duration {
	if p.PTSTime.Valid() {
		return p.PTSTime.Duration()
	}
	return p.DTSTime.Duration()
}
