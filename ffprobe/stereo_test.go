package ffprobe

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestStereo3DLive makes Matroska files with a StereoMode element and
// checks the typed side data against ffprobe's own spellings.
func TestStereo3DLive(t *testing.T) {
	requireFFprobe(t)
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	for mode, want := range map[string]Stereo3DType{"left_right": StereoSideBySide, "top_bottom": StereoTopBottom} {
		out := filepath.Join(t.TempDir(), mode+".mkv")
		cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "testsrc=s=64x32:d=0.2", "-metadata:s:v:0", "stereo_mode="+mode, "-c:v", "mpeg4", out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("ffmpeg cannot write the sample: %v %s", err, b)
		}
		res, err := Probe(ctx, out)
		if err != nil {
			t.Fatal(err)
		}
		s3d := res.VideoStream().Stereo3D()
		if s3d == nil || s3d.Type != want || !s3d.Type.Packed() {
			t.Errorf("%s: stereo3d = %+v", mode, s3d)
		}
		if s3d != nil && s3d.View != "" && s3d.View != ViewPacked {
			t.Errorf("%s: view = %q", mode, s3d.View)
		}
	}
	if Stereo2D.Packed() || StereoFrameSequence.Packed() {
		t.Error("2D / frame alternate are not packed")
	}
}
