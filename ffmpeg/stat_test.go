package ffmpeg_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
)

func TestStatParserRead(t *testing.T) {
	tests := []struct {
		name string
		line string
		want ffmpeg.ProgressStat
	}{
		{
			name: "typical video progress",
			line: "frame=  120 fps= 30 q=28.0 size=    512kB time=00:00:04.00 bitrate= 1024.0kbits/s speed=1.00x",
			want: ffmpeg.ProgressStat{
				Frame:   120,
				FPS:     30,
				Quality: 28.0,
				Size:    512 * 1024,
				Time:    4 * time.Second,
				BitRate: 1024.0,
				Speed:   1.00,
			},
		},
		{
			name: "lsize instead of size",
			line: "frame=  240 fps= 25 q=31.0 Lsize=    256kB time=00:00:09.60 bitrate= 213.5kbits/s speed=1.00x",
			want: ffmpeg.ProgressStat{
				Frame:   240,
				FPS:     25,
				Quality: 31.0,
				Size:    256 * 1024,
				Time:    9*time.Second + 600*time.Millisecond,
				BitRate: 213.5,
				Speed:   1.00,
			},
		},
		{
			name: "n/a fields",
			line: "frame=    0 fps=0.0 q=0.0 size=N/A time=N/A bitrate=N/A speed=N/A",
			want: ffmpeg.ProgressStat{
				Frame:   0,
				FPS:     0,
				Quality: 0,
				Size:    0,
				Time:    -1,
				BitRate: -1,
				Speed:   -1,
			},
		},
		{
			name: "kib suffix",
			line: "frame=   60 fps=60 q=25.0 size=    256KiB time=00:00:01.00 bitrate=2097.2kbits/s speed=2.00x",
			want: ffmpeg.ProgressStat{
				Frame:   60,
				FPS:     60,
				Quality: 25.0,
				Size:    256 * 1024,
				Time:    time.Second,
				BitRate: 2097.2,
				Speed:   2.00,
			},
		},
		{
			name: "early n/a size",
			line: "frame=    1 fps= 0 q=-0.0 size=N/A time=00:00:00.04 bitrate=N/A speed=N/A",
			want: ffmpeg.ProgressStat{
				Frame:   1,
				FPS:     0,
				Quality: 0,
				Size:    0,
				Time:    40 * time.Millisecond,
				BitRate: -1,
				Speed:   -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ffmpeg.NewStatParser(strings.NewReader(tt.line + "\r"))
			got, err := p.Read()
			if err != nil {
				t.Fatalf("Read(): unexpected error: %v", err)
			}
			if got.Frame != tt.want.Frame {
				t.Errorf("Frame: got %d, want %d", got.Frame, tt.want.Frame)
			}
			if got.FPS != tt.want.FPS {
				t.Errorf("FPS: got %f, want %f", got.FPS, tt.want.FPS)
			}
			if got.Quality != tt.want.Quality {
				t.Errorf("Quality: got %f, want %f", got.Quality, tt.want.Quality)
			}
			if got.Size != tt.want.Size {
				t.Errorf("Size: got %d, want %d", got.Size, tt.want.Size)
			}
			diff := got.Time - tt.want.Time
			if diff < -time.Millisecond || diff > time.Millisecond {
				t.Errorf("Time: got %v, want %v", got.Time, tt.want.Time)
			}
			if got.BitRate != tt.want.BitRate {
				t.Errorf("BitRate: got %f, want %f", got.BitRate, tt.want.BitRate)
			}
			if got.Speed != tt.want.Speed {
				t.Errorf("Speed: got %f, want %f", got.Speed, tt.want.Speed)
			}
		})
	}
}

func TestStatParserMultipleLines(t *testing.T) {
	input := "" +
		"frame=   60 fps=60 q=25.0 size=    256kB time=00:00:01.00 bitrate=2048.0kbits/s speed=1.00x\r" +
		"frame=  120 fps=60 q=25.0 size=    512kB time=00:00:02.00 bitrate=2048.0kbits/s speed=1.00x\r"

	p := ffmpeg.NewStatParser(strings.NewReader(input))

	first, err := p.Read()
	if err != nil {
		t.Fatalf("Read() #1: unexpected error: %v", err)
	}
	if first.Frame != 60 {
		t.Errorf("Read() #1: Frame = %d, want 60", first.Frame)
	}

	second, err := p.Read()
	if err != nil {
		t.Fatalf("Read() #2: unexpected error: %v", err)
	}
	if second.Frame != 120 {
		t.Errorf("Read() #2: Frame = %d, want 120", second.Frame)
	}
}

func TestParseProgressLineEmpty(t *testing.T) {
	_, err := ffmpeg.ParseProgressLine("")
	if err != nil {
		t.Errorf("ParseProgressLine(\"\"): unexpected error: %v", err)
	}
}

func TestStatParserMalformed(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"bad frame", "frame=abc\r"},
		{"bad fps", "fps=xyz\r"},
		{"bad time", "time=notatime\r"},
		{"bad bitrate", "bitrate=notanumber\r"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := ffmpeg.NewStatParser(strings.NewReader(tt.line))
			_, err := p.Read()
			if err == nil {
				t.Errorf("Read(): expected error for input %q, got nil", tt.line)
			}
		})
	}
}
