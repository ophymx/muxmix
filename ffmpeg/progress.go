package ffmpeg

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Progress is one ffmpeg progress update. It is filled either from the
// machine-readable -progress stream (preferred) or, where that is not
// available, from the human-readable stats line on stderr.
type Progress struct {
	Frame      int64         // frames encoded so far
	FPS        float64       // encoding speed in frames per second
	Quality    float64       // quantizer of the first video stream (stream_0_0_q), -1 when unknown
	BitRate    float64       // output bit rate in kbit/s, -1 when unknown
	Size       int64         // bytes written so far
	Time       time.Duration // output timestamp reached
	DupFrames  int64
	DropFrames int64
	Speed      float64 // realtime multiple, -1 when unknown
	Done       bool    // final update ("progress=end" or the Lsize stats line)

	// Fraction of the output written, 0 to 1, and the estimated time
	// remaining. Both are -1 unless the run was given TotalDuration; the
	// readers below leave them zero.
	Fraction float64
	ETA      time.Duration

	// Fields holds every key=value pair of a -progress block, including
	// per-stream quantizers such as stream_0_0_q. It is nil for updates
	// parsed from the stats line.
	Fields map[string]string
}

// ─── -progress key=value stream ────────────────────────────────────────────

// ProgressReader decodes ffmpeg's -progress output, which is a sequence of
// key=value lines terminated by a "progress=continue" or "progress=end"
// line.
type ProgressReader struct {
	scanner *bufio.Scanner
	fields  map[string]string
}

// NewProgressReader wraps the -progress stream.
func NewProgressReader(r io.Reader) *ProgressReader {
	return &ProgressReader{scanner: bufio.NewScanner(r), fields: map[string]string{}}
}

// Read returns the next complete progress block, or io.EOF.
func (p *ProgressReader) Read() (Progress, error) {
	for p.scanner.Scan() {
		line := strings.TrimSpace(p.scanner.Text())
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "progress" {
			p.fields[key] = value
			continue
		}
		fields := p.fields
		p.fields = map[string]string{}
		prog := ProgressFromFields(fields)
		prog.Done = value == "end"
		return prog, nil
	}
	if err := p.scanner.Err(); err != nil {
		return Progress{}, err
	}
	if len(p.fields) > 0 {
		// Stream ended mid-block (ffmpeg killed); surface what we have.
		fields := p.fields
		p.fields = map[string]string{}
		return ProgressFromFields(fields), nil
	}
	return Progress{}, io.EOF
}

// ProgressFromFields converts one -progress block into a Progress. Unknown
// values ("N/A") become -1 for rates and 0 for counters.
func ProgressFromFields(fields map[string]string) Progress {
	prog := Progress{Fields: fields, Quality: -1, BitRate: -1, Speed: -1}
	prog.Frame = fieldInt(fields, "frame")
	prog.FPS = fieldFloat(fields, "fps", 0)
	if q, ok := fields["stream_0_0_q"]; ok {
		prog.Quality = parseFloatOr(q, -1)
	}
	if v, ok := fields["bitrate"]; ok {
		prog.BitRate = parseFloatOr(strings.TrimSuffix(strings.TrimSpace(v), "kbits/s"), -1)
	}
	prog.Size = fieldInt(fields, "total_size")
	prog.DupFrames = fieldInt(fields, "dup_frames")
	prog.DropFrames = fieldInt(fields, "drop_frames")
	if v, ok := fields["speed"]; ok {
		prog.Speed = parseFloatOr(strings.TrimSuffix(strings.TrimSpace(v), "x"), -1)
	}
	prog.Time = progressTime(fields)
	return prog
}

// progressTime prefers out_time_us. out_time_ms has always held
// microseconds despite its name, so it is treated the same way; out_time is
// the HH:MM:SS.micro fallback.
func progressTime(fields map[string]string) time.Duration {
	for _, key := range []string{"out_time_us", "out_time_ms"} {
		if v, ok := fields[key]; ok && v != "N/A" {
			if us, err := strconv.ParseInt(v, 10, 64); err == nil {
				return time.Duration(us) * time.Microsecond
			}
		}
	}
	if v, ok := fields["out_time"]; ok && v != "N/A" {
		if ts, err := ParseTimeSpec(v); err == nil {
			return time.Duration(ts)
		}
	}
	return 0
}

func fieldInt(fields map[string]string, key string) int64 {
	v, ok := fields[key]
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func fieldFloat(fields map[string]string, key string, def float64) float64 {
	v, ok := fields[key]
	if !ok {
		return def
	}
	return parseFloatOr(v, def)
}

func parseFloatOr(s string, def float64) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return def
	}
	return f
}

// ─── stats line on stderr ──────────────────────────────────────────────────

var statParamRegex = regexp.MustCompile(`([^ =]+)=[\s]*([^ ]+)[ ]?`)

// StatsReader parses the "frame=... fps=... time=..." lines ffmpeg prints on
// stderr with -stats. Lines are separated by \r, \n or \r\n.
type StatsReader struct {
	scanner *bufio.Scanner
}

// NewStatsReader wraps ffmpeg's stderr.
func NewStatsReader(r io.Reader) *StatsReader {
	s := bufio.NewScanner(r)
	s.Split(splitAnyLine)
	return &StatsReader{scanner: s}
}

// Read returns the next progress update, skipping lines that are not stats
// lines, or io.EOF.
func (p *StatsReader) Read() (Progress, error) {
	for p.scanner.Scan() {
		line := p.scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "frame=") && !strings.HasPrefix(strings.TrimSpace(line), "size=") {
			continue
		}
		return ParseStatsLine(line)
	}
	if err := p.scanner.Err(); err != nil {
		return Progress{}, err
	}
	return Progress{}, io.EOF
}

// splitAnyLine is a bufio.SplitFunc that splits on \r\n, \r, or \n.
func splitAnyLine(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := range data {
		switch data[i] {
		case '\r':
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i + 2, data[:i], nil
				}
				return i + 1, data[:i], nil
			}
			if atEOF {
				return i + 1, data[:i], nil
			}
			return 0, nil, nil
		case '\n':
			return i + 1, data[:i], nil
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// ParseStatsLine parses a single stats line such as
// "frame=  120 fps= 30 q=28.0 size=     512kB time=00:00:04.00 bitrate= 1024.0kbits/s speed=1.00x".
func ParseStatsLine(line string) (Progress, error) {
	stat := Progress{Quality: -1, BitRate: -1, Speed: -1}
	var err error
	for _, kv := range statParamRegex.FindAllStringSubmatch(line, -1) {
		key := strings.ToLower(kv[1])
		value := strings.ToLower(kv[2])
		switch key {
		case "frame":
			stat.Frame, err = parseStatInt(value, "")
		case "fps":
			stat.FPS, err = parseStatFloat(value, "")
		case "q":
			stat.Quality, err = parseStatFloat(value, "")
		case "size", "lsize":
			if key == "lsize" {
				stat.Done = true
			}
			var kb int64
			kb, err = parseStatInt(value, "kb")
			if kb > 0 {
				stat.Size = kb * 1024
			}
		case "time":
			stat.Time, err = parseStatTime(value)
		case "bitrate":
			stat.BitRate, err = parseStatFloat(value, "kbits/s")
		case "speed":
			stat.Speed, err = parseStatFloat(value, "x")
		case "dup":
			stat.DupFrames, err = parseStatInt(value, "")
		case "drop":
			stat.DropFrames, err = parseStatInt(value, "")
		}
		if err != nil {
			return stat, fmt.Errorf("malformed stat: %s=%s: %w", key, value, err)
		}
	}
	return stat, nil
}

// normaliseSuffix accounts for ffmpeg using both "kB" and "KiB".
func normaliseSuffix(value, suffix string) string {
	if suffix == "kb" && strings.HasSuffix(value, "kib") {
		suffix = "kib"
	}
	return strings.TrimSuffix(value, suffix)
}

func parseStatFloat(value, suffix string) (float64, error) {
	if value == "n/a" {
		return -1, nil
	}
	return strconv.ParseFloat(normaliseSuffix(value, suffix), 64)
}

func parseStatInt(value, suffix string) (int64, error) {
	if value == "n/a" {
		return -1, nil
	}
	return strconv.ParseInt(normaliseSuffix(value, suffix), 0, 64)
}

func parseStatTime(value string) (time.Duration, error) {
	if value == "n/a" {
		return -1, nil
	}
	ts, err := ParseTimeSpec(value)
	return time.Duration(ts), err
}
