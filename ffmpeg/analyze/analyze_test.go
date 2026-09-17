package analyze_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/analyze"
)

func captureDirs(t *testing.T) []string {
	t.Helper()
	dirs, _ := filepath.Glob(filepath.Join("..", "testdata", "capture", "*"))
	if len(dirs) == 0 {
		t.Skip("no captures")
	}
	return dirs
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Skipf("%s missing", name)
	}
	return string(b)
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func nearD(a, b, tol time.Duration) bool { return a-b <= tol && b-a <= tol }

func TestParseCaptures(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			ver, _ := ffmpeg.ParseVersion(read(t, dir, "version.txt"))

			ln, err := analyze.ParseLoudnorm(read(t, dir, "analysis-loudnorm.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !near(ln.InputI, -4.4, 0.3) || !near(ln.InputTP, 0, 0.2) || ln.NormalizationType == "" || ln.InputThresh > -10 {
				t.Errorf("loudnorm = %+v", ln)
			}
			second := ln.SecondPass(analyze.DefaultLoudnormTargets)
			if !strings.HasPrefix(second, "loudnorm=I=-16:TP=-1.5:LRA=11:measured_I=-4.") || !strings.Contains(second, ":linear=true") {
				t.Errorf("second pass = %s", second)
			}

			eb, err := analyze.ParseEbur128(read(t, dir, "analysis-ebur128.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !near(eb.Integrated, -4.4, 0.3) || !near(eb.Threshold, -14.6, 0.5) || eb.Range < 15 || !near(eb.SamplePeak, 0, 0.2) || !near(eb.TruePeak, 0, 0.5) {
				t.Errorf("ebur128 = %+v", eb)
			}

			vol, err := analyze.ParseVolumeDetect(read(t, dir, "analysis-volumedetect.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if vol.Samples != 144000 || !near(vol.MaxDB, 0, 0.1) || !near(vol.MeanDB, -4.8, 0.3) || vol.Histogram[0] == 0 || vol.Headroom() != -vol.MaxDB {
				t.Errorf("volumedetect = %+v", vol)
			}

			sil := analyze.ParseSilence(read(t, dir, "analysis-silencedetect.txt"))
			if len(sil) != 1 || !nearD(sil[0].Start, time.Second, 50*time.Millisecond) || !nearD(sil[0].End, 2*time.Second, 50*time.Millisecond) || sil[0].Open || !nearD(sil[0].Duration(), time.Second, 50*time.Millisecond) {
				t.Errorf("silence = %+v", sil)
			}

			as, err := analyze.ParseAStats(read(t, dir, "analysis-astats.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if !near(as.PeakLevelDB, 0, 0.1) || !near(as.RMSLevelDB, -4.77, 0.2) || as.Samples != 144000 || as.MaxLevel < 1 || as.BitDepth == "" {
				t.Errorf("astats = %+v", as)
			}

			black := analyze.ParseBlack(read(t, dir, "analysis-blackdetect.txt"))
			if len(black) != 1 || !nearD(black[0].Start, time.Second, 50*time.Millisecond) || !nearD(black[0].End, 2040*time.Millisecond, 60*time.Millisecond) {
				t.Errorf("black = %+v", black)
			}

			frames := analyze.ParseBlackFrames(read(t, dir, "analysis-blackframe.txt"))
			if len(frames) < 24 || len(frames) > 27 || frames[0].PercentBlack < 98 || !nearD(frames[0].Time, time.Second, 50*time.Millisecond) || frames[0].PictType == "" {
				t.Errorf("blackframe = %d frames, first %+v", len(frames), frames[0])
			}

			crop, err := analyze.ParseCropDetect(read(t, dir, "analysis-cropdetect.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if *crop != (analyze.Crop{Width: 320, Height: 240, X: 20, Y: 20}) || crop.Filter() != "crop=320:240:20:20" || crop.IsFull(360, 280) {
				t.Errorf("crop = %+v", crop)
			}

			scenes := analyze.ParseSceneScores(read(t, dir, "analysis-scene.txt"))
			if len(scenes) < 2 || len(scenes) > 4 {
				t.Errorf("scene changes = %+v", scenes)
			} else {
				var cut bool
				for _, s := range scenes {
					if nearD(s.Time, 3*time.Second, 50*time.Millisecond) && s.Score > 0.3 {
						cut = true
					}
				}
				if !cut {
					t.Errorf("no cut at 3s: %+v", scenes)
				}
			}
			if ver.AtLeast(5, 0) {
				sc := analyze.ParseScdet(read(t, dir, "analysis-scdet.txt"))
				if len(sc) < 2 || sc[len(sc)-1].Score <= 0 {
					t.Errorf("scdet = %+v", sc)
				}
			}

			idet, err := analyze.ParseIdet(read(t, dir, "analysis-idet.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if idet.Verdict() != analyze.TopFirst || idet.MultiFrame.TFF < 20 {
				t.Errorf("idet = %+v verdict %s", idet, idet.Verdict())
			}

			fr := analyze.ParseFreeze(read(t, dir, "analysis-freezedetect.txt"))
			if len(fr) != 1 || !nearD(fr[0].Start, time.Second, 60*time.Millisecond) || fr[0].Open || !nearD(fr[0].End, 3*time.Second, 60*time.Millisecond) {
				t.Errorf("freeze = %+v", fr)
			}
		})
	}
}

func TestParseEdgeCases(t *testing.T) {
	open := analyze.ParseSilence("[silencedetect @ 0x1] silence_start: 4.5\n")
	if len(open) != 1 || !open[0].Open || open[0].Start != 4500*time.Millisecond || open[0].Duration() != 0 {
		t.Errorf("open interval = %+v", open)
	}
	if _, err := analyze.ParseLoudnorm("nothing here"); err == nil {
		t.Error("expected loudnorm error")
	}
	if _, err := analyze.ParseCropDetect(""); err == nil {
		t.Error("expected cropdetect error")
	}
	if _, err := analyze.ParseIdet(""); err == nil {
		t.Error("expected idet error")
	}
	if v := (&analyze.Interlace{MultiFrame: analyze.FieldCounts{Progressive: 5, TFF: 1}}).Verdict(); v != analyze.Progressive {
		t.Errorf("verdict = %s", v)
	}
}

func TestLive(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	tone := `aevalsrc=sin(440*2*PI*t)*(lt(t\,1)+gt(t\,2)):s=48000:d=3`
	sil, err := analyze.Silence(ctx, nil, tone, analyze.SilenceOptions{NoiseDB: -50, MinDuration: 500 * time.Millisecond}, ffmpeg.Lavfi())
	if err != nil || len(sil) != 1 || !nearD(sil[0].Start, time.Second, 50*time.Millisecond) {
		t.Errorf("silence = %+v %v", sil, err)
	}
	ln, err := analyze.Loudnorm(ctx, nil, tone, analyze.DefaultLoudnormTargets, ffmpeg.Lavfi())
	if err != nil || !near(ln.InputI, -4.4, 0.5) {
		t.Errorf("loudnorm = %+v %v", ln, err)
	}
	vol, err := analyze.VolumeDetect(ctx, nil, tone, ffmpeg.Lavfi())
	if err != nil || vol.Samples != 144000 {
		t.Errorf("volume = %+v %v", vol, err)
	}
	video := "testsrc2=size=64x64:rate=25:duration=1,pad=80:80:8:8"
	crop, err := analyze.CropDetect(ctx, nil, video, analyze.CropOptions{Round: 2}, ffmpeg.Lavfi())
	if err != nil || crop.Width != 64 || crop.X != 8 {
		t.Errorf("crop = %+v %v", crop, err)
	}
	black, err := analyze.Black(ctx, nil, "color=c=black:size=64x64:rate=25:duration=1", analyze.BlackOptions{MinDuration: 500 * time.Millisecond}, ffmpeg.Lavfi())
	if err != nil || len(black) != 1 || black[0].Start != 0 {
		t.Errorf("black = %+v %v", black, err)
	}
	if _, err := analyze.Black(ctx, nil, filepath.Join(t.TempDir(), "missing.mp4"), analyze.BlackOptions{}); err == nil {
		t.Error("expected error for missing input")
	}
}
