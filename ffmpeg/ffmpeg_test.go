package ffmpeg_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffprobe"
)

func TestRunProgressPipe(t *testing.T) {
	r := fakeRunner(t, "progress")
	var mu sync.Mutex
	var updates []ffmpeg.Progress
	cmd := ffmpeg.NewCommand().Input("input.mp4").Output("output.mp4")
	res, err := r.Run(context.Background(), cmd, ffmpeg.OnProgress(func(p ffmpeg.Progress) {
		mu.Lock()
		updates = append(updates, p)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatalf("Run: %v (stderr %q)", err, res.Stderr)
	}
	if res.ExitCode != 0 || string(res.Stdout) != "fake stdout\n" || !strings.Contains(string(res.Stderr), "some log line") {
		t.Errorf("result = %+v", res)
	}
	if len(updates) != 2 || !res.Progress.Done || res.Progress.Frame != 100 || res.Progress.Time != 4*time.Second {
		t.Errorf("updates=%d last=%+v", len(updates), res.Progress)
	}
	transport := []string{"-progress", "pipe:3"}
	if runtime.GOOS == "windows" {
		transport = []string{"-stats"} // no inheritable pipes; stderr stats line instead
	} else if res.Progress.Size != 204800 {
		t.Errorf("size via pipe = %d", res.Progress.Size)
	}
	want := append([]string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y"}, transport...)
	want = append(want, "-i", "input.mp4", "output.mp4")
	if strings.Join(res.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v", res.Args)
	}
}

// TestRunCaptureLimit covers the cap on Result.Stderr: a run that never
// ends must not grow it without bound, and what it keeps has to be the
// end, where ffmpeg puts the error.
func TestRunCaptureLimit(t *testing.T) {
	const limit = 4096
	cmd := ffmpeg.NewCommand().Input("in.mp4").Output("out.mp4")

	// What the scenario writes, measured rather than counted by hand.
	uncapped, _ := fakeRunner(t, "chatty").Run(context.Background(), cmd, ffmpeg.CaptureLimit(0))
	total := int64(len(uncapped.Stderr))
	if total <= limit || uncapped.StderrDropped != 0 {
		t.Fatalf("uncapped run: %d bytes, dropped %d", total, uncapped.StderrDropped)
	}

	// Both stderr paths: plain capture, and the stats reader that tees
	// through the same sink.
	for _, tc := range []struct {
		name string
		opts []ffmpeg.RunOption
	}{
		{"plain", nil},
		{"stats reader", []ffmpeg.RunOption{ffmpeg.OnProgress(func(ffmpeg.Progress) {}), ffmpeg.NoProgressPipe()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fakeRunner(t, "chatty")
			res, err := r.Run(context.Background(), cmd, append(tc.opts, ffmpeg.CaptureLimit(limit))...)
			if err == nil {
				t.Fatal("want the scenario's failure")
			}
			if len(res.Stderr) > limit {
				t.Errorf("kept %d bytes, limit %d", len(res.Stderr), limit)
			}
			if res.StderrDropped == 0 {
				t.Error("StderrDropped = 0, want the dropped head counted")
			}
			if got := int64(len(res.Stderr)) + res.StderrDropped; got != total {
				t.Errorf("kept+dropped = %d, want %d", got, total)
			}
			// The tail is the half worth keeping: the error survives.
			if !strings.Contains(err.Error(), "Error opening input") {
				t.Errorf("error lost the message: %v", err)
			}
			if strings.Contains(string(res.Stderr), "log line 0000") {
				t.Error("kept the oldest line, so it did not drop from the front")
			}
		})
	}

	t.Run("the default leaves an ordinary log alone", func(t *testing.T) {
		r := fakeRunner(t, "fail")
		res, _ := r.Run(context.Background(), cmd)
		if res.StderrDropped != 0 || !strings.Contains(string(res.Stderr), "Error opening input") {
			t.Errorf("dropped=%d stderr=%q", res.StderrDropped, res.Stderr)
		}
	})
}

func TestRunStatsFallback(t *testing.T) {
	r := fakeRunner(t, "stats")
	var n int
	res, err := r.RunArgs(context.Background(), []string{"-i", "in", "out"},
		ffmpeg.OnProgress(func(p ffmpeg.Progress) { n++ }), ffmpeg.NoProgressPipe())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || res.Progress.Frame != 100 || !res.Progress.Done {
		t.Errorf("n=%d progress=%+v", n, res.Progress)
	}
	if !strings.Contains(strings.Join(res.Args, " "), "-stats -i in out") {
		t.Errorf("args = %v", res.Args)
	}
}

func TestRunFailure(t *testing.T) {
	r := fakeRunner(t, "fail")
	res, err := r.Run(context.Background(), ffmpeg.NewCommand().Input("missing.mp4").Output("x.mp4"))
	if err == nil {
		t.Fatal("expected error")
	}
	var ferr *ffmpeg.Error
	if !errors.As(err, &ferr) || ferr.Result != res || res.ExitCode != 254 {
		t.Fatalf("err = %v, result = %+v", err, res)
	}
	if got := err.Error(); got != "ffmpeg exited with code 254: [in#0] Error opening input: No such file or directory; Error opening input file missing.mp4." {
		t.Errorf("Error() = %q", got)
	}
	var exitErr interface{ ExitCode() int }
	if !errors.As(err, &exitErr) {
		t.Error("should unwrap to *exec.ExitError")
	}
}

func TestRunReport(t *testing.T) {
	r := fakeRunner(t, "report")
	res, err := r.RunArgs(context.Background(), []string{"-i", "in", "out"}, ffmpeg.Report(""))
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(res.ReportPath)
	if res.ReportPath == "" || string(res.Report) != "report body\n" {
		t.Errorf("report = %q at %q", res.Report, res.ReportPath)
	}
}

func TestValidateInstallUsesBinary(t *testing.T) {
	r := ffmpeg.New(ffmpeg.WithBinary("definitely-missing-binary-12345"))
	if err := r.ValidateInstall(); !errors.Is(err, ffmpeg.ErrFFmpegNotFound) {
		t.Fatalf("ValidateInstall() = %v", err)
	}
	_, err := r.Run(context.Background(), ffmpeg.NewCommand().Input("a").Output("b"))
	if !errors.Is(err, ffmpeg.ErrFFmpegNotFound) {
		t.Fatalf("Run() = %v", err)
	}
}

// ─── live tests against a real ffmpeg ──────────────────────────────────────

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}
}

func TestLiveEncodeWithProgress(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mp4")
	cmd := ffmpeg.NewCommand().
		Input("testsrc2=size=64x64:rate=25:duration=2", ffmpeg.Lavfi()).
		Input("sine=frequency=440:duration=2", ffmpeg.Lavfi()).
		Output(out, ffmpeg.Map("0:v"), ffmpeg.Map("1:a"),
			ffmpeg.VideoCodec("libx264"), ffmpeg.Preset("ultrafast"), ffmpeg.PixFmt("yuv420p"),
			ffmpeg.AudioCodec("aac"), ffmpeg.Metadata("title", "live"), ffmpeg.MovFlags("+faststart"))
	var updates int
	res, err := ffmpeg.Run(context.Background(), cmd,
		ffmpeg.OnProgress(func(p ffmpeg.Progress) { updates++ }),
		ffmpeg.ProgressInterval(50*time.Millisecond))
	if err != nil {
		t.Fatalf("%v\n%s", err, res.Stderr)
	}
	if updates == 0 || !res.Progress.Done || res.Progress.Frame != 50 {
		t.Errorf("updates=%d progress=%+v", updates, res.Progress)
	}
	info, err := ffprobe.Probe(context.Background(), out)
	if err != nil {
		t.Fatal(err)
	}
	if info.VideoStream() == nil || info.AudioStream() == nil || info.Format.Tags.Value("title") != "live" {
		t.Errorf("probe = %+v", info)
	}
}

func TestLiveCancelWritesTrailer(t *testing.T) {
	requireFFmpeg(t)
	out := filepath.Join(t.TempDir(), "out.mkv")
	ctx, cancel := context.WithCancel(context.Background())
	cmd := ffmpeg.NewCommand().
		Input("testsrc2=size=64x64:rate=25", ffmpeg.Lavfi(), ffmpeg.ReadRate()).
		Output(out, ffmpeg.VideoCodec("libx264"), ffmpeg.Preset("ultrafast"))
	res, err := ffmpeg.Run(ctx, cmd, ffmpeg.OnProgress(func(p ffmpeg.Progress) {
		if p.Time >= 500*time.Millisecond {
			cancel()
		}
	}), ffmpeg.ProgressInterval(50*time.Millisecond))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v\n%s", err, res.Stderr)
	}
	// SIGINT let ffmpeg finish the file: it must be probeable with a duration.
	info, err := ffprobe.Probe(context.Background(), out)
	if err != nil {
		t.Fatalf("output after cancel is not readable: %v", err)
	}
	if d := info.Duration(); d < 300*time.Millisecond {
		t.Errorf("duration after cancel = %v", d)
	}
}

func TestLiveErrorAndVersion(t *testing.T) {
	requireFFmpeg(t)
	_, err := ffmpeg.Run(context.Background(), ffmpeg.NewCommand().Input(filepath.Join(t.TempDir(), "nope.mp4")).Output("-", ffmpeg.NullOutput()))
	var ferr *ffmpeg.Error
	if !errors.As(err, &ferr) || !strings.Contains(err.Error(), "nope.mp4") {
		t.Errorf("err = %v", err)
	}
	v, err := ffmpeg.Version(context.Background())
	if err != nil || v.Major < 4 || len(v.Libraries) == 0 {
		t.Errorf("version = %+v %v", v, err)
	}
}
