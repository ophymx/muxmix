package ffmpeg_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
)

func TestParseVersion(t *testing.T) {
	v, err := ffmpeg.ParseVersion(`ffmpeg version 7.1.5-0+deb13u1 Copyright (c) 2000-2026 the FFmpeg developers
built with gcc 14 (Debian 14.2.0-19)
configuration: --prefix=/usr --enable-gpl --enable-libx264 --enable-vaapi
libavutil      59. 39.100 / 59. 39.100
libavcodec     61. 19.101 / 61. 19.101
`)
	if err != nil {
		t.Fatal(err)
	}
	if v.Version != "7.1.5-0+deb13u1" || v.Major != 7 || v.Minor != 1 {
		t.Errorf("version = %+v", v)
	}
	if !v.AtLeast(7, 1) || !v.AtLeast(6, 9) || v.AtLeast(7, 2) || v.AtLeast(8, 0) {
		t.Error("AtLeast")
	}
	if !v.Enabled("libx264") || v.Enabled("libx265") || !v.Enabled("gpl") {
		t.Error("Enabled")
	}
	if v.Library("libavcodec") != "61.19.101" || v.Library("libavfilter") != "" {
		t.Errorf("libraries = %+v", v.Libraries)
	}
	if !strings.HasPrefix(v.Built, "gcc 14") {
		t.Errorf("built = %q", v.Built)
	}

	if _, err := ffmpeg.ParseVersion("nonsense"); err == nil {
		t.Error("expected error")
	}
	git, err := ffmpeg.ParseVersion("ffmpeg version N-118000-gabcdef Copyright\n")
	if err != nil || git.Major != 0 || git.Version != "N-118000-gabcdef" {
		t.Errorf("git build = %+v %v", git, err)
	}
	n, _ := ffmpeg.ParseVersion("ffmpeg version n8.0 Copyright\n")
	if n.Major != 8 || n.Minor != 0 {
		t.Errorf("n-prefixed = %+v", n)
	}
}

func TestParseVersionCaptures(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "capture", "*", "version.txt"))
	if len(files) == 0 {
		t.Skip("no captures")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		v, err := ffmpeg.ParseVersion(string(b))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		dir := filepath.Base(filepath.Dir(f))
		if !strings.HasPrefix(v.Version, dir) || v.Major == 0 || len(v.Libraries) < 6 || len(v.Configuration) == 0 {
			t.Errorf("%s: %+v", f, v)
		}
	}
}
