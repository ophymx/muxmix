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

// ProgressStat holds a single FFmpeg progress update.
type ProgressStat struct {
	Frame    int64
	FPS      float64
	Quality  float64
	Size     int64
	Time     time.Duration
	BitRate  float64
	Speed    float64
	Complete bool
}

// OnProgressFunc is invoked for each parsed ffmpeg progress update.
type OnProgressFunc func(ProgressStat)

var statParamRegex = regexp.MustCompile(`([^ =]+)=[\s]*([^ ]+)[ ]?`)

// StatParser reads FFmpeg progress lines from a reader, accepting \r, \n, and \r\n line endings.
type StatParser struct {
	scanner *bufio.Scanner
}

func NewStatParser(reader io.Reader) *StatParser {
	s := bufio.NewScanner(reader)
	s.Split(splitAnyLine)
	return &StatParser{scanner: s}
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
					return i + 2, data[:i], nil // \r\n
				}
				return i + 1, data[:i], nil // bare \r
			}
			if atEOF {
				return i + 1, data[:i], nil
			}
			// Need one more byte to distinguish \r from \r\n.
			return 0, nil, nil
		case '\n':
			return i + 1, data[:i], nil // bare \n
		}
	}
	if atEOF {
		return len(data), data, nil // unterminated final token
	}
	return 0, nil, nil
}

func (p *StatParser) Read() (ProgressStat, error) {
	for p.scanner.Scan() {
		if line := p.scanner.Text(); line != "" {
			return ParseProgressLine(line)
		}
	}
	if err := p.scanner.Err(); err != nil {
		return ProgressStat{}, err
	}
	return ProgressStat{}, io.EOF
}

// ParseProgressLine parses a single FFmpeg progress line into a ProgressStat struct.
func ParseProgressLine(line string) (ProgressStat, error) {
	var stat ProgressStat
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
		case "size", "lsize": // lsize is the final written size
			if key == "lsize" {
				stat.Complete = true
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
		}
		if err != nil {
			return stat, fmt.Errorf("malformed stat: %s=%s: %w", key, value, err)
		}
	}
	return stat, nil
}

// normaliseSuffix accounts for FFmpeg using both "kb" and "kib" interchangeably.
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
