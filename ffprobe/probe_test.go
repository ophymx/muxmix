package ffprobe

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTimeoutLive(t *testing.T) {
	requireFFprobe(t)
	ctx := context.Background()
	in := media("basic.mp4")

	check := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("%s: err = %v, want context.DeadlineExceeded", what, err)
		}
		if err == nil || !strings.HasPrefix(err.Error(), "ffprobe: ") || !strings.Contains(err.Error(), in) {
			t.Errorf("%s: err = %v, want the ffprobe prefix and the input path", what, err)
		}
	}

	// Per-call option, with -show_frames so ffprobe has work to interrupt.
	_, err := Probe(ctx, in, ShowFrames(), Timeout(time.Nanosecond))
	check("Timeout option", err)

	// Prober default, overridable per call.
	p := New(WithTimeout(time.Nanosecond))
	_, err = p.Probe(ctx, in)
	check("WithTimeout", err)
	if _, err := p.Probe(ctx, in, Timeout(30*time.Second)); err != nil {
		t.Errorf("per-call Timeout should override WithTimeout: %v", err)
	}
	if _, err := p.Probe(ctx, in, Timeout(0)); err != nil {
		t.Errorf("Timeout(0) should disable WithTimeout: %v", err)
	}

	// Streaming iterators report the same error.
	var got error
	for _, err := range p.Frames(ctx, in) {
		got = err
	}
	check("Frames", got)

	// The caller's own deadline is reported the same way, minus the
	// "timed out after" clause.
	dctx, cancel := context.WithTimeout(ctx, time.Nanosecond)
	defer cancel()
	_, err = Probe(dctx, in)
	check("caller deadline", err)
	if strings.Contains(err.Error(), "timed out after") {
		t.Errorf("caller deadline reported as the timeout option: %v", err)
	}
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

func requireFFprobe(t *testing.T) {
	t.Helper()
	if err := ValidateInstall(); err != nil {
		t.Skip("ffprobe not installed")
	}
}

func media(name string) string { return filepath.Join("testdata", "media", name) }

func TestBuildArgs(t *testing.T) {
	var c config
	c.apply([]Option{ShowChapters(), SelectStreams("v:0"), InputFormat("h264"), Args("-x", "1")})
	got := strings.Join(Default.buildArgs(&c, "in.mp4"), " ")
	want := "-hide_banner -loglevel error -print_format json -show_error -show_chapters -select_streams v:0 -x 1 -f h264 -i in.mp4"
	if got != want {
		t.Errorf("args\n got %s\nwant %s", got, want)
	}
	c = config{}
	got = strings.Join(Default.buildArgs(&c, "x"), " ")
	if !strings.Contains(got, "-show_format -show_streams") {
		t.Errorf("default sections missing: %s", got)
	}
}

func TestProbeLive(t *testing.T) {
	requireFFprobe(t)
	ctx := context.Background()

	res, err := Probe(ctx, media("basic.mp4"), ShowFormat(), ShowStreams(), ShowChapters())
	if err != nil {
		t.Fatal(err)
	}
	if res.VideoStream() == nil || len(res.Chapters) != 2 || !res.Format.Is("mp4") {
		t.Errorf("unexpected result %+v", res)
	}
	// Sections are exactly what was asked for.
	only, err := Probe(ctx, media("basic.mp4"), ShowChapters())
	if err != nil || only.Format.Filename != "" || len(only.Streams) != 0 || len(only.Chapters) != 2 {
		t.Errorf("chapters only: %+v %v", only, err)
	}

	_, err = Probe(ctx, media("does-not-exist.mp4"))
	if !isNotExist(err) {
		t.Errorf("missing file: %v", err)
	}
	var pe *ProbeError
	if !errors.As(err, &pe) || pe.Code.Errno() != syscall.ENOENT || pe.Code != -2 {
		t.Errorf("expected ProbeError ENOENT, got %v", err)
	}

	_, err = Probe(ctx, "probe.go")
	if !IsUnsupported(err) {
		t.Errorf("invalid data: %v", err)
	}

	_, err = Probe(ctx, media("basic.mp4"), Args("-not-an-option"))
	var xe *ExitError
	if !errors.As(err, &xe) || xe.ExitCode == 0 {
		t.Errorf("bad option: %v", err)
	}
}

func TestProbeReaderLive(t *testing.T) {
	requireFFprobe(t)
	f, err := os.Open(media("raw.h264"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	res, err := ProbeReader(context.Background(), f, InputFormat("h264"))
	if err != nil {
		t.Fatal(err)
	}
	if v := res.VideoStream(); v == nil || v.CodecName != "h264" {
		t.Errorf("stream = %+v", res.Streams)
	}
}

func TestVersionLive(t *testing.T) {
	requireFFprobe(t)
	v, err := Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Program.Version == "" || len(v.Libraries) == 0 {
		t.Errorf("version = %+v", v)
	}
	pix, err := Default.PixelFormats(context.Background())
	if err != nil || len(pix) < 50 {
		t.Errorf("pixel formats = %d %v", len(pix), err)
	}
}

func TestFramesStreamLive(t *testing.T) {
	requireFFprobe(t)
	ctx := context.Background()
	var n int
	for f, err := range Default.Frames(ctx, media("basic.mp4"), SelectStreams("v")) {
		if err != nil {
			t.Fatal(err)
		}
		if !f.IsVideo() {
			t.Errorf("non-video frame: %+v", f)
		}
		n++
	}
	if n != 25 {
		t.Errorf("frames = %d", n)
	}

	// Early exit kills ffprobe and does not hang or leak an error.
	n = 0
	for _, err := range Default.Packets(ctx, media("basic.mp4")) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		if n == 3 {
			break
		}
	}

	// Mixed listing.
	var packets, frames int
	for pf, err := range Default.PacketsAndFrames(ctx, media("basic.mp4"), SelectStreams("v")) {
		if err != nil {
			t.Fatal(err)
		}
		if pf.Packet != nil {
			packets++
		}
		if pf.Frame != nil {
			frames++
		}
	}
	if packets != 25 || frames != 25 {
		t.Errorf("packets=%d frames=%d", packets, frames)
	}

	// Errors reach the loop.
	var got error
	for _, err := range Default.Frames(ctx, media("nope.mp4")) {
		got = err
	}
	if !isNotExist(got) {
		t.Errorf("error from stream = %v", got)
	}

	// Context cancellation ends iteration.
	cctx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	time.Sleep(20 * time.Millisecond)
	for _, err := range Default.Frames(cctx, media("basic.mp4")) {
		if err == nil {
			continue
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("cancelled stream error = %v", err)
		}
	}
}
