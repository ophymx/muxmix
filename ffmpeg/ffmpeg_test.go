package ffmpeg_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
)

func writeFakeFFmpeg(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-ffmpeg.sh")
	script := "#!/usr/bin/env bash\nset -euo pipefail\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("failed to write fake ffmpeg script: %v", err)
	}
	return path
}

func TestRunnerRunWithOptionsSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}

	fake := writeFakeFFmpeg(t, `
echo "fake stdout" 
printf 'frame=    1 fps=0.0 q=-0.0 size=N/A time=00:00:00.04 bitrate=N/A speed=N/A\r' >&2
printf 'frame=  100 fps=25.0 q=20.0 Lsize=    200kB time=00:00:04.00 bitrate= 409.6kbits/s speed=1.00x\r' >&2
`)

	r := ffmpeg.New(ffmpeg.WithBinary(fake), ffmpeg.WithDefaultArgs())

	var stats []ffmpeg.ProgressStat
	result, err := r.RunWithOptions(context.Background(), ffmpeg.RunOptions{
		Args: []string{"-i", "input.mp4", "output.mp4"},
		OnProgress: func(s ffmpeg.ProgressStat) {
			stats = append(stats, s)
		},
	})
	if err != nil {
		t.Fatalf("RunWithOptions() error = %v", err)
	}

	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if len(result.Args) != 3 {
		t.Fatalf("len(Args) = %d, want 3", len(result.Args))
	}
	if string(result.Stdout) == "" {
		t.Fatal("Stdout was not captured")
	}
	if result.LastStat.Frame != 100 {
		t.Fatalf("LastStat.Frame = %d, want 100", result.LastStat.Frame)
	}
	if !result.LastStat.Complete {
		t.Fatal("LastStat.Complete = false, want true")
	}
	if len(stats) < 2 {
		t.Fatalf("progress callback count = %d, want at least 2", len(stats))
	}
}

func TestRunnerRunWithOptionsFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}

	fake := writeFakeFFmpeg(t, `
printf 'frame=  10 fps=10.0 q=25.0 size=     10kB time=00:00:01.00 bitrate=  81.9kbits/s speed=1.00x\r' >&2
echo "simulated ffmpeg failure" >&2
exit 5
`)

	r := ffmpeg.New(ffmpeg.WithBinary(fake), ffmpeg.WithDefaultArgs())
	result, err := r.Run(context.Background(), "-i", "input.mp4", "output.mp4")
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}

	var runErr *ffmpeg.Error
	if !errors.As(err, &runErr) {
		t.Fatalf("error type = %T, want *ffmpeg.Error", err)
	}
	if runErr.Result == nil {
		t.Fatal("runErr.Result = nil")
	}
	if result.ExitCode != 5 {
		t.Fatalf("ExitCode = %d, want 5", result.ExitCode)
	}
	if len(result.Report) == 0 {
		t.Fatal("Report is empty, expected stderr fallback")
	}
	if !strings.Contains(string(result.Report), "simulated ffmpeg failure") {
		t.Fatalf("Report = %q, expected failure message", string(result.Report))
	}
}

func TestValidateInstallUsesBinary(t *testing.T) {
	r := ffmpeg.New(ffmpeg.WithBinary("definitely-missing-binary-12345"))
	err := r.ValidateInstall()
	if !errors.Is(err, ffmpeg.ErrFFmpegNotFound) {
		t.Fatalf("ValidateInstall() error = %v, want ErrFFmpegNotFound", err)
	}
}
