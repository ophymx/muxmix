package ffprobe

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSideDataAccessors(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			rot := decodeCapture(t, filepath.Join(dir, "rotated_mp4.basic.json"))
			dm := rot.VideoStream().DisplayMatrix()
			if dm == nil {
				t.Fatalf("no display matrix: %+v", rot.VideoStream().SideDataList)
			}
			if deg := dm.Degrees(); deg != 90 && deg != 270 {
				t.Errorf("degrees = %d (rotation %v)", deg, dm.Rotation)
			}
			vals := dm.Values()
			if len(vals) != 9 || vals[8] != 1<<30 {
				t.Errorf("matrix values = %v", vals)
			}
			if rot.VideoStream().Rotation() != int(dm.Rotation.Float64()) {
				t.Errorf("Rotation() = %d vs %v", rot.VideoStream().Rotation(), dm.Rotation)
			}
			if rot.VideoStream().Stereo3D() != nil || rot.VideoStream().IsHDR() {
				t.Error("unexpected side data")
			}

			hdr := decodeCapture(t, filepath.Join(dir, "hdr_mp4.frames.json"))
			var md *MasteringDisplay
			var cll *ContentLightLevel
			for i := range hdr.Frames {
				if md == nil {
					md = hdr.Frames[i].MasteringDisplay()
				}
				if cll == nil {
					cll = hdr.Frames[i].ContentLightLevel()
				}
			}
			if md == nil || !md.HasPrimaries() || !md.HasLuminance() || md.MaxNits() != 1000 || md.MinNits() != 0.0001 || md.RedX.String() != "34000/50000" {
				t.Errorf("mastering display = %+v", md)
			}
			if cll == nil || cll.MaxContent.Int() != 1000 || cll.MaxAverage.Int() != 400 {
				t.Errorf("content light level = %+v", cll)
			}

			pk := decodeCapture(t, filepath.Join(dir, "basic_mp4.packets.json"))
			var skip *SkipSamples
			for i := range pk.Packets {
				if skip = pk.Packets[i].SkipSamples(); skip != nil {
					break
				}
			}
			if skip == nil || skip.SkipSamples.Int() != 1024 {
				t.Errorf("skip samples = %+v", skip)
			}

			// Generic decode of an entry with no typed accessor.
			for _, sd := range pk.Packets[0].SideDataList {
				var raw map[string]any
				if err := DecodeSideData(&sd, &raw); err != nil || len(raw) == 0 {
					t.Errorf("DecodeSideData = %v %v", raw, err)
				}
			}
		})
	}
}

func TestParseSectionsCaptures(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, "sections.txt"))
			if err != nil {
				t.Skip("no sections capture")
			}
			root, err := ParseSections(string(b))
			if err != nil {
				t.Fatal(err)
			}
			if root.Name != "root" || !root.Wrapper || len(root.Children) < 8 {
				t.Errorf("root = %+v", root)
			}
			streams := root.Child("streams")
			if streams == nil || !streams.Array || len(streams.Children) != 1 || streams.Children[0].Name != "stream" {
				t.Errorf("streams = %+v", streams)
			}
			tags := streams.Children[0].Child("tags")
			if tags == nil || !tags.Variable || tags.UniqueName != "stream_tags" {
				t.Errorf("stream tags = %+v", tags)
			}
			// Find is depth-first, so a bare "streams" hits the one under programs.
			if nested := root.Find("streams"); nested == nil || nested.UniqueName != "program_streams" {
				t.Errorf("Find(streams) = %+v", nested)
			}
			if root.Find("stream_tags") != tags || !root.Has("library_versions") || !root.Has("pixel_formats") || root.Has("nonexistent") {
				t.Error("Find/Has")
			}
			var count int
			root.Walk(func(*Section, int) { count++ })
			if count < 30 {
				t.Errorf("walked %d sections", count)
			}
			ver := filepath.Base(dir)
			if (ver >= "7") != root.Has("stream_groups") {
				t.Errorf("stream_groups presence on %s: %v", ver, root.Has("stream_groups"))
			}
		})
	}
	if _, err := ParseSections("nothing"); err == nil {
		t.Error("expected error")
	}
}

func TestSectionsLive(t *testing.T) {
	requireFFprobe(t)
	root, err := Default.Sections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !root.Has("streams") || !root.Has("format") {
		t.Errorf("sections = %+v", root)
	}
}
