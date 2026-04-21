package ffmpeg

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type TimeSpec time.Duration

// ParseTimeSpec parses an FFmpeg timestamp in HH:MM:SS[.ms], MM:SS[.ms], or S[.ms] format.
func ParseTimeSpec(value string) (TimeSpec, error) {
	if len(value) == 0 {
		return 0, fmt.Errorf("malformed timestamp: %q", value)
	}
	original := value
	sign := time.Duration(1)
	if value[0] == '-' {
		value = value[1:]
		sign = -1
	}
	parts := strings.Split(value, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("malformed timestamp: %q", original)
	}
	var hour, minute int64
	var second float64
	var err error
	switch len(parts) {
	case 1:
		second, err = strconv.ParseFloat(parts[0], 64)
	case 2:
		minute, err = strconv.ParseInt(parts[0], 10, 64)
		if err == nil {
			second, err = strconv.ParseFloat(parts[1], 64)
		}
	case 3:
		hour, err = strconv.ParseInt(parts[0], 10, 64)
		if err == nil {
			minute, err = strconv.ParseInt(parts[1], 10, 64)
		}
		if err == nil {
			second, err = strconv.ParseFloat(parts[2], 64)
		}
	}
	if err != nil {
		return 0, fmt.Errorf("malformed timestamp %q: %w", original, err)
	}
	return TimeSpec((time.Duration(hour)*time.Hour +
		time.Duration(minute)*time.Minute +
		time.Duration(second*float64(time.Second))) * sign), nil
}

func (ts TimeSpec) String() string {
	dur := time.Duration(ts)
	sign := ""
	if ts < 0 {
		sign = "-"
		dur *= -1
	}
	hours := dur / time.Hour
	dur = dur - hours*time.Hour
	minutes := dur / time.Minute
	seconds := (dur - minutes*time.Minute).Seconds()
	return fmt.Sprintf("%s%02d:%02d:%05.2f", sign, hours, minutes, seconds)
}

func (ts TimeSpec) MarshalJSON() ([]byte, error) {
	return []byte(`"` + ts.String() + `"`), nil
}

func (ts *TimeSpec) UnmarshalJSON(data []byte) error {
	s := string(data)
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return fmt.Errorf("TimeSpec: expected a JSON string, got %s", s)
	}
	parsed, err := ParseTimeSpec(s[1 : len(s)-1])
	if err != nil {
		return err
	}
	*ts = parsed
	return nil
}
