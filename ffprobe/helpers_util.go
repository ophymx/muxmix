package ffprobe

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func splitComma(s string) []string { return strings.Split(s, ",") }

// numberOf converts a value from an Extra map (json.Number, float64, int or a
// numeric string) into a float64, or 0.
func numberOf(v any) float64 {
	switch n := v.(type) {
	case json.Number:
		f, _ := n.Float64()
		return f
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

// parseClockDuration parses "HH:MM:SS.fraction" as written by Matroska's
// DURATION tag and by ffmpeg's -t option.
func parseClockDuration(s string) (time.Duration, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) == 0 || len(parts) > 3 {
		return 0, fmt.Errorf("ffprobe: malformed duration %q", s)
	}
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, fmt.Errorf("ffprobe: malformed duration %q", s)
		}
		total = total*60 + v
	}
	return time.Duration(total * float64(time.Second)), nil
}
