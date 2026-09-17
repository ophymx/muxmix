package ffmpeg_test

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
)

func TestParseStatsLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want ffmpeg.Progress
	}{
		{
			name: "typical video progress",
			line: "frame=  120 fps= 30 q=28.0 size=    512kB time=00:00:04.00 bitrate= 1024.0kbits/s speed=1.00x",
			want: ffmpeg.Progress{Frame: 120, FPS: 30, Quality: 28.0, Size: 512 * 1024, Time: 4 * time.Second, BitRate: 1024.0, Speed: 1.00},
		},
		{
			name: "lsize marks completion",
			line: "frame=  240 fps= 25 q=31.0 Lsize=    256kB time=00:00:09.60 bitrate= 213.5kbits/s speed=1.00x",
			want: ffmpeg.Progress{Frame: 240, FPS: 25, Quality: 31.0, Size: 256 * 1024, Time: 9*time.Second + 600*time.Millisecond, BitRate: 213.5, Speed: 1.00, Done: true},
		},
		{
			name: "n/a fields",
			line: "frame=    0 fps=0.0 q=0.0 size=N/A time=N/A bitrate=N/A speed=N/A",
			want: ffmpeg.Progress{Time: -1, BitRate: -1, Speed: -1},
		},
		{
			name: "kib suffix and dup/drop",
			line: "frame=   60 fps=60 q=25.0 size=    256KiB time=00:00:01.00 bitrate=2097.2kbits/s dup=3 drop=1 speed=2.00x",
			want: ffmpeg.Progress{Frame: 60, FPS: 60, Quality: 25.0, Size: 256 * 1024, Time: time.Second, BitRate: 2097.2, Speed: 2.00, DupFrames: 3, DropFrames: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ffmpeg.ParseStatsLine(tt.line)
			if err != nil {
				t.Fatal(err)
			}
			got.Fields = nil
			if diff := got.Time - tt.want.Time; diff < -time.Millisecond || diff > time.Millisecond {
				t.Errorf("Time = %v, want %v", got.Time, tt.want.Time)
			}
			got.Time, tt.want.Time = 0, 0
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestStatsReaderLineEndings(t *testing.T) {
	input := "some log line\n" +
		"frame=   60 fps=60 q=25.0 size=     256kB time=00:00:01.00 bitrate=2097.2kbits/s speed=2.00x\r" +
		"frame=  120 fps=60 q=25.0 size=     512kB time=00:00:02.00 bitrate=2097.2kbits/s speed=2.00x\r\n" +
		"frame=  180 fps=60 q=25.0 Lsize=     768kB time=00:00:03.00 bitrate=2097.2kbits/s speed=2.00x\n"
	r := ffmpeg.NewStatsReader(strings.NewReader(input))
	var frames []int64
	for {
		p, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, p.Frame)
		if p.Frame == 180 && !p.Done {
			t.Error("last line should be Done")
		}
	}
	if len(frames) != 3 || frames[0] != 60 || frames[2] != 180 {
		t.Errorf("frames = %v", frames)
	}
}

func TestStatsReaderMalformed(t *testing.T) {
	for _, line := range []string{"frame=abc fps=0", "frame=1 time=zz:yy"} {
		_, err := ffmpeg.NewStatsReader(strings.NewReader(line)).Read()
		if err == nil {
			t.Errorf("%q: expected error", line)
		}
	}
}

func TestProgressReader(t *testing.T) {
	input := "frame=25\nfps=0.00\nstream_0_0_q=23.0\nbitrate= 222.0kbits/s\ntotal_size=55494\nout_time_us=1000000\nout_time_ms=1000000\nout_time=00:00:01.000000\ndup_frames=1\ndrop_frames=2\nspeed=19.8x\nprogress=continue\n" +
		"frame=50\nfps=48.5\nstream_0_0_q=-1.0\nbitrate=N/A\ntotal_size=N/A\nout_time_us=2000000\nout_time_ms=2000000\nout_time=00:00:02.000000\ndup_frames=1\ndrop_frames=2\nspeed=N/A\nprogress=end\n"
	r := ffmpeg.NewProgressReader(strings.NewReader(input))
	first, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if first.Frame != 25 || first.Quality != 23 || first.BitRate != 222 || first.Size != 55494 || first.Time != time.Second || first.DupFrames != 1 || first.DropFrames != 2 || first.Speed != 19.8 || first.Done {
		t.Errorf("first = %+v", first)
	}
	if first.Fields["stream_0_0_q"] != "23.0" {
		t.Errorf("fields = %v", first.Fields)
	}
	second, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if second.Frame != 50 || second.FPS != 48.5 || second.Quality != -1 || second.BitRate != -1 || second.Size != 0 || second.Speed != -1 || second.Time != 2*time.Second || !second.Done {
		t.Errorf("second = %+v", second)
	}
	if _, err := r.Read(); err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}

	// A truncated final block is still delivered.
	r = ffmpeg.NewProgressReader(strings.NewReader("frame=7\nout_time_us=280000\n"))
	p, err := r.Read()
	if err != nil || p.Frame != 7 || p.Time != 280*time.Millisecond || p.Done {
		t.Errorf("truncated = %+v %v", p, err)
	}
}

// TestProgressCaptures decodes the -progress and stats output recorded from
// every ffmpeg version in the matrix and checks they agree.
func TestProgressCaptures(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("testdata", "capture", "*"))
	if len(dirs) == 0 {
		t.Skip("no captures")
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, "progress.txt"))
			if err != nil {
				t.Skip("no progress capture")
			}
			defer f.Close()
			r := ffmpeg.NewProgressReader(f)
			var last ffmpeg.Progress
			var n int
			for {
				p, err := r.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				n++
				last = p
			}
			if n == 0 || !last.Done {
				t.Fatalf("blocks=%d last=%+v", n, last)
			}
			if last.Frame != 100 || last.Time < 3900*time.Millisecond || last.Time > 4100*time.Millisecond || last.Size <= 0 {
				t.Errorf("final progress = %+v", last)
			}

			// The stats line on stderr reports the same run.
			sf, err := os.Open(filepath.Join(dir, "stderr.txt"))
			if err != nil {
				t.Fatal(err)
			}
			defer sf.Close()
			sr := ffmpeg.NewStatsReader(sf)
			var lastStat ffmpeg.Progress
			var m int
			for {
				p, err := sr.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				m++
				lastStat = p
			}
			if m == 0 || !lastStat.Done || lastStat.Frame != 100 {
				t.Errorf("stats blocks=%d last=%+v", m, lastStat)
			}
			if d := lastStat.Time - last.Time; d < -20*time.Millisecond || d > 20*time.Millisecond {
				t.Errorf("stats time %v vs progress time %v", lastStat.Time, last.Time)
			}
		})
	}
}
