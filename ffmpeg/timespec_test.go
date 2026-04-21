package ffmpeg_test

import (
	"testing"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
)

func TestParseTimeSpec(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		// HH:MM:SS
		{"00:00:00", 0, false},
		{"01:02:03", 1*time.Hour + 2*time.Minute + 3*time.Second, false},
		{"00:00:01", time.Second, false},
		{"00:01:00", time.Minute, false},
		{"01:00:00", time.Hour, false},
		{"10:30:45", 10*time.Hour + 30*time.Minute + 45*time.Second, false},
		// HH:MM:SS with fractional seconds
		{"00:00:01.5", 1500 * time.Millisecond, false},
		{"00:00:00.25", 250 * time.Millisecond, false},
		{"01:02:03.75", 1*time.Hour + 2*time.Minute + 3*time.Second + 750*time.Millisecond, false},
		// MM:SS
		{"01:30", 1*time.Minute + 30*time.Second, false},
		{"00:05", 5 * time.Second, false},
		{"01:30.5", 1*time.Minute + 30*time.Second + 500*time.Millisecond, false},
		// Seconds only
		{"90", 90 * time.Second, false},
		{"0", 0, false},
		{"3.14", 3*time.Second + 140*time.Millisecond, false},
		// Negative values
		{"-01:02:03", -(1*time.Hour + 2*time.Minute + 3*time.Second), false},
		{"-30", -30 * time.Second, false},
		{"-00:30", -30 * time.Second, false},
		// Errors
		{"", 0, true},
		{":::", 0, true},
		{"1:2:3:4", 0, true},
		{"ab:00:00", 0, true},
		{"00:xx:00", 0, true},
		{"00:00:zz", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ffmpeg.ParseTimeSpec(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseTimeSpec(%q): expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTimeSpec(%q): unexpected error: %v", tt.input, err)
			}
			diff := time.Duration(got) - tt.want
			if diff < -time.Millisecond || diff > time.Millisecond {
				t.Errorf("ParseTimeSpec(%q) = %v, want %v", tt.input, time.Duration(got), tt.want)
			}
		})
	}
}

func TestTimeSpecString(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "00:00:00.00"},
		{time.Second, "00:00:01.00"},
		{time.Minute, "00:01:00.00"},
		{time.Hour, "01:00:00.00"},
		{1*time.Hour + 2*time.Minute + 3*time.Second, "01:02:03.00"},
		{500 * time.Millisecond, "00:00:00.50"},
		{1*time.Hour + 2*time.Minute + 3*time.Second + 750*time.Millisecond, "01:02:03.75"},
		{-(1*time.Hour + 2*time.Minute + 3*time.Second), "-01:02:03.00"},
		{-30 * time.Second, "-00:00:30.00"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := ffmpeg.TimeSpec(tt.input).String()
			if got != tt.want {
				t.Errorf("TimeSpec(%v).String() = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTimeSpecRoundTrip(t *testing.T) {
	inputs := []string{
		"00:00:00.00",
		"01:02:03.00",
		"01:02:03.75",
		"-01:02:03.00",
		"00:00:00.50",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			ts, err := ffmpeg.ParseTimeSpec(input)
			if err != nil {
				t.Fatalf("ParseTimeSpec(%q): %v", input, err)
			}
			got := ts.String()
			if got != input {
				t.Errorf("round-trip: ParseTimeSpec(%q).String() = %q", input, got)
			}
		})
	}
}
