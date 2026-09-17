package ffprobe

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The captures under testdata/probe/<version>/ are produced by
// matrix/capture.sh from the media in testdata/media using every ffprobe
// release line the package supports. Every capture must decode, must decode
// strictly (no key ffprobe printed is unknown to the generated types), and
// must survive a marshal/unmarshal round trip.

func captureDirs(t *testing.T) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join("testdata", "probe", "*"))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no captures under testdata/probe: %v", err)
	}
	sort.Strings(dirs)
	return dirs
}

func readCapture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decodeCapture(t *testing.T, path string) *Result {
	t.Helper()
	var res Result
	if err := json.Unmarshal(readCapture(t, path), &res); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return &res
}

func TestCapturesDecode(t *testing.T) {
	for _, dir := range captureDirs(t) {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		if len(files) == 0 {
			t.Errorf("%s: no captures", dir)
		}
		for _, f := range files {
			t.Run(filepath.Base(dir)+"/"+filepath.Base(f), func(t *testing.T) {
				data := readCapture(t, f)

				var lenient Result
				if err := json.Unmarshal(data, &lenient); err != nil {
					t.Fatalf("lenient decode: %v", err)
				}

				dec := json.NewDecoder(bytes.NewReader(data))
				dec.DisallowUnknownFields()
				var strict Result
				if err := dec.Decode(&strict); err != nil {
					t.Errorf("strict decode (a key ffprobe printed is missing from the generated types): %v", err)
				}

				out, err := json.Marshal(&lenient)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
				var again Result
				if err := json.Unmarshal(out, &again); err != nil {
					t.Fatalf("decode after marshal: %v", err)
				}
				out2, _ := json.Marshal(&again)
				if !bytes.Equal(out, out2) {
					t.Errorf("round trip is not stable")
				}
			})
		}
	}
}

// TestCapturesAgree checks the same facts about the same media across every
// captured ffprobe version, which is what version tolerance means in practice.
func TestCapturesAgree(t *testing.T) {
	for _, dir := range captureDirs(t) {
		ver := filepath.Base(dir)
		t.Run(ver, func(t *testing.T) {
			res := decodeCapture(t, filepath.Join(dir, "basic_mp4.basic.json"))
			if !res.Format.Is("mp4") {
				t.Fatalf("format = %+v", res.Format)
			}
			if got := res.Format.Tags.Value("title"); got != "Muxmix Test" {
				t.Errorf("title = %q", got)
			}
			if d := res.Duration(); d < 900*time.Millisecond || d > 1200*time.Millisecond {
				t.Errorf("duration = %v", d)
			}
			if res.Format.Size.Int64() <= 0 || res.Format.BitRate.Int64() <= 0 {
				t.Errorf("size/bit_rate = %v/%v", res.Format.Size, res.Format.BitRate)
			}
			v := res.VideoStream()
			if v == nil {
				t.Fatal("no video stream")
			}
			if w, h := v.Resolution(); w != 32 || h != 32 {
				t.Errorf("resolution = %dx%d", w, h)
			}
			if fr := v.FrameRate(); !fr.Valid() || fr.Num() != 25 || fr.Den() != 1 {
				t.Errorf("frame rate = %v", fr)
			}
			if v.CodecName != "h264" || v.PixFmt != "yuv420p" {
				t.Errorf("codec = %s/%s", v.CodecName, v.PixFmt)
			}
			if !v.TimeBase.Valid() || v.TimeBase.Den() != 12800 {
				t.Errorf("time_base = %v", v.TimeBase)
			}
			if !v.IsDefault() {
				t.Error("video should be default")
			}
			a := res.AudioStream()
			if a == nil || a.CodecName != "aac" || a.SampleRate.Int() != 48000 || a.Channels.Int() != 1 {
				t.Errorf("audio = %+v", a)
			}
			if len(res.Chapters) != 2 {
				t.Fatalf("chapters = %d", len(res.Chapters))
			}
			ch := res.Chapters[1]
			if ch.Tags.Value("title") != "Outro" || ch.StartTime() != 500*time.Millisecond || ch.EndTime() != time.Second {
				t.Errorf("chapter = %+v", ch)
			}
			if !ch.TimeBase.Valid() || ch.TimeBase.Duration(ch.Start) != 500*time.Millisecond {
				t.Errorf("chapter start via time base = %v", ch.TimeBase.Duration(ch.Start))
			}

			// Matroska: languages, dispositions, subtitle, attachment.
			mkv := decodeCapture(t, filepath.Join(dir, "multi_mkv.basic.json"))
			if len(mkv.Streams) != 5 {
				t.Fatalf("mkv streams = %d", len(mkv.Streams))
			}
			langs := []string{}
			for _, s := range mkv.AudioStreams() {
				langs = append(langs, s.Language())
			}
			if strings.Join(langs, ",") != "eng,deu" {
				t.Errorf("audio languages = %v", langs)
			}
			if mkv.AudioStream().Title() != "English" {
				t.Errorf("default audio = %q", mkv.AudioStream().Title())
			}
			subs := mkv.SubtitleStreams()
			if len(subs) != 1 || !subs[0].IsForced() || subs[0].CodecName != "subrip" {
				t.Errorf("subtitles = %+v", subs)
			}
			if att := mkv.StreamsOfType(MediaTypeAttachment); len(att) != 1 || att[0].Tags.Value("mimetype") != "text/plain" {
				t.Errorf("attachment = %+v", att)
			}
			if d := mkv.AudioStream().Duration(); d < 900*time.Millisecond || d > 1100*time.Millisecond {
				t.Errorf("mkv audio duration (from DURATION tag) = %v", d)
			}

			// MPEG-TS programs nest a streams array.
			ts := decodeCapture(t, filepath.Join(dir, "programs_ts.basic.json"))
			if len(ts.Programs) != 2 || len(ts.Programs[0].Streams) != 2 || ts.Programs[1].Tags.Value("service_name") != "Second" {
				t.Errorf("programs = %+v", ts.Programs)
			}

			// Display matrix side data with variable keys.
			rot := decodeCapture(t, filepath.Join(dir, "rotated_mp4.basic.json"))
			if r := rot.VideoStream().Rotation(); r != 270 {
				t.Errorf("rotation = %d (side data %+v)", r, rot.VideoStream().SideDataList)
			}

			// Cover art is not the main video.
			mp3 := decodeCapture(t, filepath.Join(dir, "cover_mp3.basic.json"))
			if mp3.HasVideo() || len(mp3.VideoStreams()) != 1 || !mp3.VideoStreams()[0].IsAttachedPic() {
				t.Errorf("cover art detection failed: %+v", mp3.VideoStreams())
			}

			// Raw elementary stream: no timing at all.
			raw := decodeCapture(t, filepath.Join(dir, "raw_h264.basic.json"))
			if raw.Format.DurationSecs.Valid() || raw.VideoStream().DurationSecs.Valid() {
				t.Errorf("raw h264 should have no duration")
			}
			if raw.Format.Size.Int64() <= 0 {
				t.Errorf("raw h264 size = %v", raw.Format.Size)
			}

			// Packets and frames are numeric strings in ffprobe's JSON.
			pk := decodeCapture(t, filepath.Join(dir, "basic_mp4.packets.json"))
			if len(pk.Packets) == 0 {
				t.Fatal("no packets")
			}
			var firstVideo *Packet
			for i := range pk.Packets {
				if pk.Packets[i].CodecType == MediaTypeVideo {
					firstVideo = pk.Packets[i]
					break
				}
			}
			if firstVideo == nil || !firstVideo.Size.Valid() || !firstVideo.Pos.Valid() || !firstVideo.IsKeyframe() || firstVideo.Time() != 0 {
				t.Errorf("first video packet = %+v", firstVideo)
			}
			if a := pk.Packets[0]; a.CodecType != MediaTypeAudio || !a.IsDiscard() {
				t.Errorf("first packet should be the audio priming packet flagged discard: %+v", a)
			}
			fr := decodeCapture(t, filepath.Join(dir, "basic_mp4.frames.json"))
			var video, audio int
			for _, f := range fr.Frames {
				switch {
				case f.IsVideo():
					video++
					if !f.PktSize.Valid() || !f.PktPos.Valid() || f.Width.Int() != 32 {
						t.Errorf("video frame = %+v", f)
					}
					if video == 2 && f.Time() != 40*time.Millisecond {
						t.Errorf("second video frame time = %v", f.Time())
					}
					if f.Duration() != 40*time.Millisecond {
						t.Errorf("video frame duration = %v", f.Duration())
					}
				case f.IsAudio():
					audio++
					if !f.NbSamples.Valid() {
						t.Errorf("audio frame = %+v", f)
					}
				}
			}
			if video != 25 || audio == 0 {
				t.Errorf("frames video=%d audio=%d", video, audio)
			}
			if !fr.Frames[0].KeyFrame.Bool() {
				t.Error("first frame should be a keyframe")
			}
			both := decodeCapture(t, filepath.Join(dir, "basic_mp4.both.json"))
			var packets, frames int
			for _, pf := range both.PacketsAndFrames {
				switch {
				case pf.Packet != nil && pf.Frame == nil:
					packets++
				case pf.Frame != nil && pf.Packet == nil:
					frames++
				default:
					t.Errorf("packets_and_frames entry has type %q with neither/both set", pf.Type)
				}
			}
			if packets != len(pk.Packets) || frames != len(fr.Frames) {
				t.Errorf("packets_and_frames = %d/%d, want %d/%d", packets, frames, len(pk.Packets), len(fr.Frames))
			}

			// Subtitle frames share the Frame type.
			mkvFrames := decodeCapture(t, filepath.Join(dir, "multi_mkv.frames.json"))
			var subFrames int
			for _, f := range mkvFrames.Frames {
				if f.IsSubtitle() {
					subFrames++
					if !f.NumRects.Valid() {
						t.Errorf("subtitle frame = %+v", f)
					}
				}
			}
			if subFrames != 2 {
				t.Errorf("subtitle frames = %d", subFrames)
			}

			// -show_entries omits everything else; nothing is required.
			ent := decodeCapture(t, filepath.Join(dir, "basic_mp4.entries.json"))
			if ent.Format.Filename == "" || len(ent.Streams) != 3 || ent.Streams[0].Width.Valid() || ent.Streams[0].Language() != "und" {
				t.Errorf("entries = %+v", ent)
			}

			// Errors decode into ProbeError with sentinel mapping.
			missing := decodeCapture(t, filepath.Join(dir, "error-missing.json"))
			if missing.Error == nil || !isNotExist(missing.Error) {
				t.Errorf("missing file error = %+v", missing.Error)
			}
			invalid := decodeCapture(t, filepath.Join(dir, "error-invalid.json"))
			if invalid.Error == nil || !IsUnsupported(invalid.Error) {
				t.Errorf("invalid file error = %+v", invalid.Error)
			}

			// Version info.
			vinfo := decodeCapture(t, filepath.Join(dir, "versions.json"))
			if vinfo.ProgramVersion.Version == "" || !strings.HasPrefix(vinfo.ProgramVersion.Version, ver[:3]) || len(vinfo.LibraryVersions) < 5 {
				t.Errorf("versions = %+v", vinfo.ProgramVersion)
			}
			pix := decodeCapture(t, filepath.Join(dir, "pixel_formats.json"))
			var yuv420p *PixelFormat
			for i := range pix.PixelFormats {
				if pix.PixelFormats[i].Name == "yuv420p" {
					yuv420p = pix.PixelFormats[i]
				}
			}
			if yuv420p == nil || yuv420p.NbComponents.Int() != 3 || len(yuv420p.Components) != 3 || !yuv420p.Flags.Planar.Bool() {
				t.Errorf("yuv420p = %+v", yuv420p)
			}
		})
	}
}

// TestCapturesVersionSpecific covers sections only some releases produce.
func TestCapturesVersionSpecific(t *testing.T) {
	for _, dir := range captureDirs(t) {
		ver := filepath.Base(dir)
		t.Run(ver, func(t *testing.T) {
			hdr := decodeCapture(t, filepath.Join(dir, "hdr_mp4.basic.json"))
			v := hdr.VideoStream()
			if v.ColorSpace != "bt2020nc" || v.PixFmt != "yuv420p10le" || v.CodecName != "hevc" {
				t.Errorf("hdr stream = %s/%s/%s", v.CodecName, v.PixFmt, v.ColorSpace)
			}
			frames := decodeCapture(t, filepath.Join(dir, "hdr_mp4.frames.json"))
			var mastering, cll int
			for _, f := range frames.Frames {
				for _, sd := range f.SideDataList {
					switch sd.SideDataType {
					case "Mastering display metadata":
						mastering++
						if _, ok := sd.Extra["max_luminance"]; !ok {
							t.Errorf("mastering display side data lacks max_luminance: %+v", sd.Extra)
						}
					case "Content light level metadata":
						cll++
						if numberOf(sd.Extra["max_content"]) != 1000 {
							t.Errorf("content light level = %+v", sd.Extra)
						}
					}
				}
			}
			if mastering != 25 || cll != 25 {
				t.Errorf("hdr side data: mastering=%d cll=%d", mastering, cll)
			}

			if groups, err := os.ReadFile(filepath.Join(dir, "groups_iamf.basic.json")); err == nil {
				var res Result
				if err := json.Unmarshal(groups, &res); err != nil {
					t.Fatal(err)
				}
				if res.Error == nil { // ffprobe < 7.0 cannot read IAMF
					if len(res.StreamGroups) != 2 {
						t.Fatalf("stream groups = %d", len(res.StreamGroups))
					}
					g := res.StreamGroups[0]
					if g.Type != "IAMF Audio Element" || g.NbStreams.Int() != 2 || len(g.Streams) != 2 || len(g.Components) != 1 {
						t.Errorf("audio element = %+v", g)
					}
					if len(g.Components[0].Subcomponents) != 4 {
						t.Errorf("subcomponents = %+v", g.Components[0].Subcomponents)
					}
					if _, ok := g.Components[0].Extra["nb_layers"]; !ok {
						t.Errorf("component extra = %+v", g.Components[0].Extra)
					}
				}
			}
		})
	}
}

// TestSamplesDecode covers real-world captures of unknown ffprobe version.
func TestSamplesDecode(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "samples", "*.json"))
	if len(files) == 0 {
		t.Skip("no samples")
	}
	for _, f := range files {
		res := decodeCapture(t, f)
		if res.Format.Filename == "" || res.VideoStream() == nil || res.Duration() == 0 {
			t.Errorf("%s: format=%q video=%v duration=%v", f, res.Format.Filename, res.VideoStream() != nil, res.Duration())
		}
	}
}

func TestVariableExtraRoundTrip(t *testing.T) {
	in := `{"side_data_type":"Display Matrix","displaymatrix":"\n00000000: 0 65536 0\n","rotation":-90}`
	var sd SideData
	if err := json.Unmarshal([]byte(in), &sd); err != nil {
		t.Fatal(err)
	}
	if sd.SideDataType != "Display Matrix" || numberOf(sd.Extra["rotation"]) != -90 || len(sd.Extra) != 2 {
		t.Errorf("decoded %+v", sd)
	}
	out, err := json.Marshal(sd)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(out, &back)
	if len(back) != 3 || back["rotation"] != float64(-90) {
		t.Errorf("round trip: %s", out)
	}
}
