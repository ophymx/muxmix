package ffprobe

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func decodeStream(t *testing.T, src string) *Stream {
	t.Helper()
	var s Stream
	if err := json.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	return &s
}

// Matroska statistics tags as mkvmerge writes them (ffmpeg's own muxer only
// writes DURATION, which the captures cover).
func TestStreamTagFallbacks(t *testing.T) {
	s := decodeStream(t, `{"codec_type":"audio","tags":{"BPS":"192000","NUMBER_OF_FRAMES":"1234","DURATION":"00:01:02.500000000"}}`)
	if got := s.Bitrate(); got != 192000 {
		t.Errorf("Bitrate() from BPS = %d", got)
	}
	if got := s.FrameCount(); got != 1234 {
		t.Errorf("FrameCount() from NUMBER_OF_FRAMES = %d", got)
	}
	if got := s.Duration(); got != 62500*time.Millisecond {
		t.Errorf("Duration() from DURATION = %v", got)
	}

	eng := decodeStream(t, `{"tags":{"BPS-eng":"768000","NUMBER_OF_FRAMES-eng":"99","_STATISTICS_TAGS-eng":"BPS DURATION NUMBER_OF_FRAMES NUMBER_OF_BYTES"}}`)
	if eng.Bitrate() != 768000 || eng.FrameCount() != 99 {
		t.Errorf("-eng spellings: bitrate=%d frames=%d", eng.Bitrate(), eng.FrameCount())
	}

	// The real fields win over the tags, and nb_read_frames over the tag.
	real := decodeStream(t, `{"bit_rate":"128000","nb_frames":"10","tags":{"BPS":"1","NUMBER_OF_FRAMES":"1"}}`)
	if real.Bitrate() != 128000 || real.FrameCount() != 10 {
		t.Errorf("fields should win: bitrate=%d frames=%d", real.Bitrate(), real.FrameCount())
	}
	counted := decodeStream(t, `{"nb_read_frames":"25","tags":{"NUMBER_OF_FRAMES":"1"}}`)
	if counted.FrameCount() != 25 {
		t.Errorf("nb_read_frames should win over the tag: %d", counted.FrameCount())
	}

	none := decodeStream(t, `{"codec_type":"video"}`)
	if none.Bitrate() != 0 || none.FrameCount() != 0 || none.Duration() != 0 || none.StartTime() != 0 {
		t.Errorf("nothing known should be zero: %d %d %v %v", none.Bitrate(), none.FrameCount(), none.Duration(), none.StartTime())
	}

	// Tick fallbacks.
	ticks := decodeStream(t, `{"time_base":"1/90000","start_pts":90000,"duration_ts":180000}`)
	if ticks.StartTime() != time.Second || ticks.Duration() != 2*time.Second {
		t.Errorf("tick fallbacks: start=%v duration=%v", ticks.StartTime(), ticks.Duration())
	}
}

func TestCaptureTagFallbacks(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			mkv := decodeCapture(t, filepath.Join(dir, "multi_mkv.basic.json"))
			for _, s := range mkv.Streams {
				if s.CodecType == MediaTypeAttachment {
					continue
				}
				if _, ok := s.Tags.Get("DURATION"); !ok {
					t.Fatalf("stream %d has no DURATION tag: %v", s.Index.Int(), s.Tags)
				}
				if d := s.Duration(); d < 900*time.Millisecond || d > 1100*time.Millisecond {
					t.Errorf("stream %d Duration() = %v", s.Index.Int(), d)
				}
			}
			// Format-level values are real fields; the helpers read them.
			if mkv.Format.Duration() < 900*time.Millisecond || mkv.Format.StartTime() > 0 {
				t.Errorf("format duration=%v start=%v", mkv.Format.Duration(), mkv.Format.StartTime())
			}
			if mkv.Duration() != mkv.Format.Duration() {
				t.Errorf("Result.Duration() = %v, want the format's %v", mkv.Duration(), mkv.Format.Duration())
			}
		})
	}
}

func TestRotation(t *testing.T) {
	// Display matrix rotation is counter-clockwise; Rotation() is clockwise.
	for _, tc := range []struct {
		src  string
		want int
	}{
		{`{"side_data_list":[{"side_data_type":"Display Matrix","rotation":-90}]}`, 90},
		{`{"side_data_list":[{"side_data_type":"Display Matrix","rotation":90}]}`, 270},
		{`{"side_data_list":[{"side_data_type":"Display Matrix","rotation":180}]}`, 180},
		{`{"side_data_list":[{"side_data_type":"Display Matrix","rotation":-180}]}`, 180},
		{`{"side_data_list":[{"side_data_type":"Display Matrix","rotation":0}]}`, 0},
		{`{"side_data_list":[{"side_data_type":"Display Matrix","rotation":-89.9}]}`, 90},
		// The legacy tag is already clockwise.
		{`{"tags":{"rotate":"90"}}`, 90},
		{`{"tags":{"rotate":"270"}}`, 270},
		{`{"tags":{"rotate":"-90"}}`, 270},
		{`{"tags":{"rotate":"0"}}`, 0},
		// Side data wins over the tag; a tag that ffmpeg 4.4 derived from
		// the matrix agrees with it anyway.
		{`{"tags":{"rotate":"270"},"side_data_list":[{"side_data_type":"Display Matrix","rotation":90}]}`, 270},
		{`{}`, 0},
	} {
		if got := decodeStream(t, tc.src).Rotation(); got != tc.want {
			t.Errorf("%s: Rotation() = %d, want %d", tc.src, got, tc.want)
		}
	}

	// The rotated fixture was made with -display_rotation 90 (counter-
	// clockwise), so every capture reports 270 clockwise, and 4.4's legacy
	// tag says the same.
	for _, dir := range captureDirs(t) {
		rot := decodeCapture(t, filepath.Join(dir, "rotated_mp4.basic.json"))
		v := rot.VideoStream()
		if v.Rotation() != 270 || v.DisplayMatrix().Degrees() != 270 || v.DisplayMatrix().Rotation.Float64() != 90 {
			t.Errorf("%s: Rotation()=%d Degrees()=%d raw=%v", filepath.Base(dir), v.Rotation(), v.DisplayMatrix().Degrees(), v.DisplayMatrix().Rotation)
		}
		if tag, ok := v.Tags.Get("rotate"); ok && tag != "270" {
			t.Errorf("%s: rotate tag = %q", filepath.Base(dir), tag)
		}
	}
}

func TestStreamID(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want int
		ok   bool
	}{
		// ffprobe prints the id in hex.
		{`{"id":"0x2"}`, 2, true},
		{`{"id":"0x100"}`, 256, true},
		{`{"id":"0x0"}`, 0, true},
		{`{"id":"0X1f"}`, 31, true},
		// Absent, or present and unknown, is not an id.
		{`{}`, 0, false},
		{`{"id":"N/A"}`, 0, false},
		{`{"id":""}`, 0, false},
		{`{"id":"nonsense"}`, 0, false},
		// A leading zero is not octal: decimal, for tools that mimic
		// ffprobe without the hex.
		{`{"id":"17"}`, 17, true},
		{`{"id":"017"}`, 17, true},
	} {
		got, ok := decodeStream(t, tc.src).StreamID()
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: StreamID() = %d, %v; want %d, %v", tc.src, got, ok, tc.want, tc.ok)
		}
	}

	// MPEG-TS PIDs are the ids -map 0:i:256 selects, and every ffprobe
	// the captures cover prints them.
	for _, dir := range captureDirs(t) {
		res := decodeCapture(t, filepath.Join(dir, "programs_ts.basic.json"))
		var got []int
		for _, s := range res.Streams {
			id, ok := s.StreamID()
			if !ok {
				t.Errorf("%s: stream %d has no id", filepath.Base(dir), s.Index.Int())
				continue
			}
			got = append(got, id)
		}
		if !slices.Equal(got, []int{0x100, 0x101, 0x102}) {
			t.Errorf("%s: stream ids = %v, want [256 257 258]", filepath.Base(dir), got)
		}
	}
}

func TestFrameRateAttachedPic(t *testing.T) {
	for _, dir := range captureDirs(t) {
		mp3 := decodeCapture(t, filepath.Join(dir, "cover_mp3.basic.json"))
		cover := mp3.VideoStreams()[0]
		if !cover.IsAttachedPic() || cover.RFrameRate.Float64() != 90000 {
			t.Fatalf("%s: unexpected cover stream %+v", filepath.Base(dir), cover)
		}
		if fr := cover.FrameRate(); fr.Valid() {
			t.Errorf("%s: attached picture FrameRate() = %v, want invalid", filepath.Base(dir), fr)
		}
		if fr := mp3.AudioStream().FrameRate(); fr.Valid() {
			t.Errorf("%s: audio FrameRate() = %v, want invalid", filepath.Base(dir), fr)
		}
	}
}

func TestPacketFlags(t *testing.T) {
	for _, tc := range []struct {
		flags                 string
		key, discard, corrupt bool
	}{
		{"K__", true, false, false},
		{"K_", true, false, false},
		{"_D_", false, true, false},
		{"KD_", true, true, false},
		{"__C", false, false, true},
		{"KDC", true, true, true},
		{"", false, false, false},
	} {
		p := &Packet{Flags: tc.flags}
		if p.IsKeyframe() != tc.key || p.IsDiscard() != tc.discard || p.IsCorrupt() != tc.corrupt {
			t.Errorf("%q: key=%v discard=%v corrupt=%v", tc.flags, p.IsKeyframe(), p.IsDiscard(), p.IsCorrupt())
		}
	}
}

func TestIntOrInt(t *testing.T) {
	if NewInt(7).OrInt(3) != 7 || (Int{}).OrInt(3) != 3 {
		t.Error("OrInt")
	}
}

func TestPointerSlicesAndValueFormat(t *testing.T) {
	// A streams-only probe leaves Format as an honest zero value.
	var res Result
	if err := json.Unmarshal([]byte(`{"streams":[{"index":0,"codec_type":"video","disposition":{"default":1}}]}`), &res); err != nil {
		t.Fatal(err)
	}
	if res.Format.BitRate.Valid() || res.Format.Duration() != 0 || res.Format.Is("mp4") {
		t.Errorf("zero Format = %+v", res.Format)
	}
	for _, s := range res.Streams {
		if !s.IsDefault() || s.IsAttachedPic() {
			t.Errorf("stream = %+v", s)
		}
	}
	if res.Stream(0) != res.Streams[0] || res.VideoStream() != res.Streams[0] {
		t.Error("helpers should return the same pointers the slice holds")
	}
	out, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"streams":[{"index":0,"codec_type":"video","disposition":{"default":1}}]}` {
		t.Errorf("absent sections should not marshal: %s", out)
	}
}

func TestChapterHelpers(t *testing.T) {
	var c Chapter
	if err := json.Unmarshal([]byte(`{"id":1,"time_base":"1/1000","start":500,"start_time":"0.500000","end":1000,"end_time":"1.000000","tags":{"title":"Outro"}}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.StartTime() != 500*time.Millisecond || c.EndTime() != time.Second || c.Duration() != 500*time.Millisecond || c.Title() != "Outro" {
		t.Errorf("chapter = %v %v %v %q", c.StartTime(), c.EndTime(), c.Duration(), c.Title())
	}
	ticks := Chapter{TimeBase: NewRat(1, 1000), Start: NewInt(250), End: NewInt(750)}
	if ticks.StartTime() != 250*time.Millisecond || ticks.EndTime() != 750*time.Millisecond {
		t.Errorf("tick fallback = %v %v", ticks.StartTime(), ticks.EndTime())
	}
}
