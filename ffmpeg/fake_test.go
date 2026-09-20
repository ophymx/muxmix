package ffmpeg_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
)

// The test binary doubles as a fake ffmpeg: when MUXMIX_FAKE_FFMPEG names a
// scenario, TestMain plays it instead of running tests. This works on every
// platform, unlike a shell script, so the Windows stats-line fallback is
// exercised in CI.

const fakeEnv = "MUXMIX_FAKE_FFMPEG"

func TestMain(m *testing.M) {
	if scenario := os.Getenv(fakeEnv); scenario != "" {
		os.Exit(fakeFFmpeg(scenario, os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeRunner returns a runner whose "ffmpeg" is this test binary playing the
// given scenario.
func fakeRunner(t *testing.T, scenario string) ffmpeg.Runner {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), fakeEnv+"="+scenario)
	return ffmpeg.New(ffmpeg.WithBinary(exe), ffmpeg.WithEnv(env...))
}

func fakeFFmpeg(scenario string, args []string) int {
	has := func(flag string) bool {
		for _, a := range args {
			if a == flag {
				return true
			}
		}
		return false
	}
	switch scenario {
	case "progress":
		fmt.Println("fake stdout")
		fmt.Fprintln(os.Stderr, "some log line")
		if has("pipe:3") {
			pipe := os.NewFile(3, "progress")
			if pipe == nil {
				fmt.Fprintln(os.Stderr, "fd 3 not inherited")
				return 3
			}
			fmt.Fprint(pipe, "frame=1\nout_time_us=40000\nprogress=continue\n")
			fmt.Fprint(pipe, "frame=100\nout_time_us=4000000\ntotal_size=204800\nprogress=end\n")
			pipe.Close()
			return 0
		}
		if has("-stats") {
			fmt.Fprint(os.Stderr, "frame=    1 fps=0.0 q=-0.0 size=N/A time=00:00:00.04 bitrate=N/A speed=N/A\r")
			fmt.Fprint(os.Stderr, "frame=  100 fps=25.0 q=20.0 Lsize=    200kB time=00:00:04.00 bitrate= 409.6kbits/s speed=1.00x\r")
			return 0
		}
		fmt.Fprintln(os.Stderr, "no progress transport requested")
		return 3
	case "stats":
		fmt.Fprint(os.Stderr, "frame=    1 fps=0.0 q=-0.0 size=N/A time=00:00:00.04 bitrate=N/A speed=N/A\r")
		fmt.Fprint(os.Stderr, "frame=  100 fps=25.0 q=20.0 Lsize=    200kB time=00:00:04.00 bitrate= 409.6kbits/s speed=1.00x\r")
		return 0
	case "fail":
		fmt.Fprintln(os.Stderr, "[in#0] Error opening input: No such file or directory")
		fmt.Fprintln(os.Stderr, "Error opening input file missing.mp4.")
		fmt.Fprintln(os.Stderr, "Conversion failed!")
		return 254
	case "chatty":
		// A long run's log: many lines, the ones that matter last.
		for i := range 2000 {
			fmt.Fprintf(os.Stderr, "log line %04d filler filler filler filler\n", i)
		}
		fmt.Fprintln(os.Stderr, "[in#0] Error opening input: No such file or directory")
		fmt.Fprintln(os.Stderr, "Conversion failed!")
		return 254
	case "report":
		spec := os.Getenv("FFREPORT")
		if !strings.HasPrefix(spec, "file=") {
			fmt.Fprintln(os.Stderr, "no FFREPORT:", spec)
			return 2
		}
		path := strings.TrimPrefix(spec, "file=")
		if i := strings.Index(path, ":level="); i >= 0 {
			path = path[:i]
		}
		path = strings.ReplaceAll(path, `\:`, ":")
		path = strings.ReplaceAll(path, `\\`, `\`)
		if err := os.WriteFile(path, []byte("report body\n"), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown scenario", scenario)
	return 1
}
