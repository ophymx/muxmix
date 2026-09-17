package ffprobe

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestColorFromCaptures(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			hdr := decodeCapture(t, filepath.Join(dir, "hdr_mp4.basic.json"))
			c := colorOf(hdr.VideoStream())
			if c.IsHDR() || c.hasStaticHDR() {
				t.Fatalf("stream section should carry no HDR metadata: %+v", c)
			}
			frames := decodeCapture(t, filepath.Join(dir, "hdr_mp4.frames.json"))
			c.mergeFrame(&frames.Frames[0])
			if !c.FromFrame || !c.IsHDR() {
				t.Fatalf("frame merge did not yield HDR: %+v", c)
			}
			if c.Mastering == nil || c.Mastering.MaxNits() != 1000 {
				t.Errorf("mastering = %+v", c.Mastering)
			}
			if c.LightLevel == nil || c.LightLevel.MaxContent.Int() != 1000 {
				t.Errorf("light level = %+v", c.LightLevel)
			}
			if c.PixFmt != "yuv420p10le" {
				t.Errorf("pix_fmt = %q", c.PixFmt)
			}

			sdr := decodeCapture(t, filepath.Join(dir, "basic_mp4.basic.json"))
			if colorOf(sdr.VideoStream()).IsHDR() {
				t.Error("basic.mp4 reported as HDR")
			}
		})
	}
}

func TestColorLive(t *testing.T) {
	requireFFprobe(t)
	ctx := context.Background()

	c, err := Color(ctx, media("hdr.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsHDR() || !c.FromFrame || c.Mastering == nil || c.LightLevel == nil {
		t.Errorf("hdr.mp4: %+v", c)
	}

	c, err = Color(ctx, media("basic.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if c.IsHDR() {
		t.Errorf("basic.mp4 reported as HDR: %+v", c)
	}

	c, err = Color(ctx, media("audio.wav"))
	if err != nil || c != nil {
		t.Errorf("audio-only: c=%+v err=%v", c, err)
	}
}

func TestMissingBinary(t *testing.T) {
	ctx := context.Background()
	for _, bin := range []string{"ffprobe-does-not-exist", filepath.Join(t.TempDir(), "ffprobe")} {
		p := New(WithBinary(bin))
		if err := p.ValidateInstall(); !errors.Is(err, ErrFFProbeNotFound) {
			t.Errorf("ValidateInstall(%s) = %v", bin, err)
		}
		_, err := p.Probe(ctx, media("basic.mp4"))
		check := func(what string, err error) {
			t.Helper()
			if !errors.Is(err, ErrFFProbeNotFound) {
				t.Errorf("%s(%s) = %v, want ErrFFProbeNotFound", what, bin, err)
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s(%s) looks like a missing input: %v", what, bin, err)
			}
			var exit *ExitError
			if errors.As(err, &exit) {
				t.Errorf("%s(%s) reported as ExitError: %v", what, bin, err)
			}
		}
		check("Probe", err)
		_, err = p.Version(ctx)
		check("Version", err)
		for _, err := range p.Frames(ctx, media("basic.mp4")) {
			check("Frames", err)
		}
	}
}
