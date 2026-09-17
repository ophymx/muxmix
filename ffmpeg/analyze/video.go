package analyze

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
)

// ─── blackdetect ───────────────────────────────────────────────────────────

// BlackOptions configures black segment detection.
type BlackOptions struct {
	MinDuration    time.Duration // shortest black run to report; default 2s
	PictureBlack   float64       // fraction of pixels that must be black (0-1); default 0.98
	PixelThreshold float64       // luma level below which a pixel is black (0-1); default 0.10
}

// Black runs blackdetect and returns the black intervals.
func Black(ctx context.Context, r ffmpeg.Runner, input string, o BlackOptions, inputOpts ...ffmpeg.Opt) ([]Interval, error) {
	if o.MinDuration == 0 {
		o.MinDuration = 2 * time.Second
	}
	if o.PictureBlack == 0 {
		o.PictureBlack = 0.98
	}
	if o.PixelThreshold == 0 {
		o.PixelThreshold = 0.10
	}
	filter := fmt.Sprintf("blackdetect=d=%s:pic_th=%s:pix_th=%s", formatFloat(o.MinDuration.Seconds()), formatFloat(o.PictureBlack), formatFloat(o.PixelThreshold))
	log, err := run(ctx, r, input, true, filter, inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseBlack(log), nil
}

// ParseBlack reads black_start / black_end lines.
func ParseBlack(log string) []Interval {
	return parseIntervals(log, "black_start", "black_end")
}

// ─── freezedetect ──────────────────────────────────────────────────────────

// FreezeOptions configures frozen-frame detection.
type FreezeOptions struct {
	NoiseDB     float64       // difference below which frames count as identical; default -60 dB
	MinDuration time.Duration // shortest freeze to report; default 2s
}

// Freeze runs freezedetect and returns the frozen intervals.
func Freeze(ctx context.Context, r ffmpeg.Runner, input string, o FreezeOptions, inputOpts ...ffmpeg.Opt) ([]Interval, error) {
	if o.NoiseDB == 0 {
		o.NoiseDB = -60
	}
	if o.MinDuration == 0 {
		o.MinDuration = 2 * time.Second
	}
	filter := fmt.Sprintf("freezedetect=n=%sdB:d=%s", formatFloat(o.NoiseDB), formatFloat(o.MinDuration.Seconds()))
	log, err := run(ctx, r, input, true, filter, inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseFreeze(log), nil
}

// ParseFreeze reads lavfi.freezedetect.freeze_start / freeze_end lines.
func ParseFreeze(log string) []Interval {
	return parseIntervals(log, "lavfi.freezedetect.freeze_start", "lavfi.freezedetect.freeze_end")
}

// ─── blackframe ────────────────────────────────────────────────────────────

// BlackFrame is one frame reported by the blackframe filter.
type BlackFrame struct {
	Frame        int64
	PercentBlack int
	Time         time.Duration
	PictType     string
	LastKeyframe int64
}

// BlackFrames runs blackframe and returns every frame at least amount
// percent black (default 98).
func BlackFrames(ctx context.Context, r ffmpeg.Runner, input string, amount int, inputOpts ...ffmpeg.Opt) ([]BlackFrame, error) {
	if amount == 0 {
		amount = 98
	}
	log, err := run(ctx, r, input, true, fmt.Sprintf("blackframe=amount=%d", amount), inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseBlackFrames(log), nil
}

// ParseBlackFrames reads "frame:N pblack:P pts:… t:… type:T last_keyframe:K" lines.
func ParseBlackFrames(log string) []BlackFrame {
	var out []BlackFrame
	for _, ln := range lines(log) {
		if !strings.Contains(ln.instance, "blackframe") {
			continue
		}
		kv := keyValues(ln.text)
		if _, ok := kv["pblack"]; !ok {
			continue
		}
		out = append(out, BlackFrame{
			Frame:        integer(kv["frame"]),
			PercentBlack: int(integer(kv["pblack"])),
			Time:         seconds(kv["t"]),
			PictType:     kv["type"],
			LastKeyframe: integer(kv["last_keyframe"]),
		})
	}
	return out
}

// ─── cropdetect ────────────────────────────────────────────────────────────

// Crop is the region cropdetect settled on.
type Crop struct {
	Width, Height int
	X, Y          int
}

// Filter returns the crop filter that applies this region.
func (c Crop) Filter() string { return fmt.Sprintf("crop=%d:%d:%d:%d", c.Width, c.Height, c.X, c.Y) }

// IsFull reports whether nothing would be cropped from a frame of the given size.
func (c Crop) IsFull(width, height int) bool {
	return c.X == 0 && c.Y == 0 && c.Width == width && c.Height == height
}

// CropOptions configures crop detection.
type CropOptions struct {
	Limit int // black threshold (0-255, or 0-1 as a fraction on newer ffmpeg); default 24
	Round int // width and height are rounded to a multiple of this; default 16
}

// CropDetect runs cropdetect over the input and returns the final region.
func CropDetect(ctx context.Context, r ffmpeg.Runner, input string, o CropOptions, inputOpts ...ffmpeg.Opt) (*Crop, error) {
	if o.Limit == 0 {
		o.Limit = 24
	}
	if o.Round == 0 {
		o.Round = 16
	}
	filter := fmt.Sprintf("cropdetect=limit=%d:round=%d:reset=0", o.Limit, o.Round)
	log, err := run(ctx, r, input, true, filter, inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseCropDetect(log)
}

var cropRe = regexp.MustCompile(`crop=(\d+):(\d+):(\d+):(\d+)`)

// ParseCropDetect returns the last crop=W:H:X:Y that cropdetect printed.
func ParseCropDetect(log string) (*Crop, error) {
	all := cropRe.FindAllStringSubmatch(log, -1)
	if len(all) == 0 {
		return nil, fmt.Errorf("analyze: no cropdetect output in log")
	}
	m := all[len(all)-1]
	return &Crop{
		Width:  int(integer(m[1])),
		Height: int(integer(m[2])),
		X:      int(integer(m[3])),
		Y:      int(integer(m[4])),
	}, nil
}

// ─── scene changes ─────────────────────────────────────────────────────────

// SceneChange is a frame whose difference from the previous frame exceeded
// the threshold.
type SceneChange struct {
	Time  time.Duration
	Score float64 // 0-1 from the select filter's scene score
}

// SceneChanges finds cuts using the select filter's scene score, which is
// available on every ffmpeg version. threshold is 0-1; 0.3 to 0.5 suits
// most content.
func SceneChanges(ctx context.Context, r ffmpeg.Runner, input string, threshold float64, inputOpts ...ffmpeg.Opt) ([]SceneChange, error) {
	if threshold == 0 {
		threshold = 0.4
	}
	filter := fmt.Sprintf("select='gt(scene,%s)',metadata=print", formatFloat(threshold))
	log, err := run(ctx, r, input, true, filter, inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseSceneScores(log), nil
}

// ParseSceneScores reads metadata=print output following a select filter:
// "frame:N pts:P pts_time:T" lines followed by "lavfi.scene_score=S".
func ParseSceneScores(log string) []SceneChange {
	var out []SceneChange
	var pending *SceneChange
	for _, ln := range lines(log) {
		if !strings.Contains(ln.instance, "metadata") {
			continue
		}
		if strings.HasPrefix(ln.text, "frame:") {
			kv := keyValues(ln.text)
			out = append(out, SceneChange{Time: seconds(kv["pts_time"])})
			pending = &out[len(out)-1]
			continue
		}
		if v, ok := strings.CutPrefix(ln.text, "lavfi.scene_score="); ok && pending != nil {
			pending.Score = float(v)
			pending = nil
		}
	}
	return out
}

// ParseScdet reads the scdet filter's "lavfi.scd.score: S, lavfi.scd.time: T"
// lines (ffmpeg 5.0 and newer). Scores are on scdet's 0-100 scale.
func ParseScdet(log string) []SceneChange {
	var out []SceneChange
	for _, ln := range lines(log) {
		if !strings.Contains(ln.instance, "scdet") {
			continue
		}
		kv := keyValues(ln.text)
		if s, ok := kv["lavfi.scd.score"]; ok {
			out = append(out, SceneChange{Score: float(s), Time: seconds(kv["lavfi.scd.time"])})
		}
	}
	return out
}

// ─── idet ──────────────────────────────────────────────────────────────────

// FieldCounts is one row of the idet summary.
type FieldCounts struct {
	TFF, BFF, Progressive, Undetermined int64
}

// Interlace is the idet summary.
type Interlace struct {
	RepeatedNeither, RepeatedTop, RepeatedBottom int64
	SingleFrame                                  FieldCounts
	MultiFrame                                   FieldCounts
}

// FieldOrder is the interlacing verdict.
type FieldOrder string

const (
	Progressive  FieldOrder = "progressive"
	TopFirst     FieldOrder = "tff"
	BottomFirst  FieldOrder = "bff"
	Undetermined FieldOrder = "undetermined"
)

// Verdict returns the majority result of multi-frame detection.
func (i *Interlace) Verdict() FieldOrder {
	c := i.MultiFrame
	best, order := c.Undetermined, Undetermined
	if c.Progressive > best {
		best, order = c.Progressive, Progressive
	}
	if c.TFF > best {
		best, order = c.TFF, TopFirst
	}
	if c.BFF > best {
		order = BottomFirst
	}
	return order
}

// IdetDetect runs the idet filter.
func IdetDetect(ctx context.Context, r ffmpeg.Runner, input string, inputOpts ...ffmpeg.Opt) (*Interlace, error) {
	log, err := run(ctx, r, input, true, "idet", inputOpts)
	if err != nil {
		return nil, err
	}
	return ParseIdet(log)
}

// ParseIdet reads the three summary lines idet prints. The last instance
// in the log wins, since ffmpeg may print an empty summary for a filter
// replaced during graph reconfiguration.
func ParseIdet(log string) (*Interlace, error) {
	var result *Interlace
	var current string
	for _, ln := range lines(log) {
		if !strings.Contains(ln.instance, "idet") {
			continue
		}
		if ln.instance != current {
			current = ln.instance
			result = &Interlace{}
		}
		body, ok := strings.CutPrefix(ln.text, "Repeated Fields:")
		if ok {
			kv := keyValues(body)
			result.RepeatedNeither = integer(kv["Neither"])
			result.RepeatedTop = integer(kv["Top"])
			result.RepeatedBottom = integer(kv["Bottom"])
			continue
		}
		counts := func(body string) FieldCounts {
			kv := keyValues(body)
			return FieldCounts{TFF: integer(kv["TFF"]), BFF: integer(kv["BFF"]), Progressive: integer(kv["Progressive"]), Undetermined: integer(kv["Undetermined"])}
		}
		if body, ok := strings.CutPrefix(ln.text, "Single frame detection:"); ok {
			result.SingleFrame = counts(body)
		} else if body, ok := strings.CutPrefix(ln.text, "Multi frame detection:"); ok {
			result.MultiFrame = counts(body)
		}
	}
	if result == nil {
		return nil, fmt.Errorf("analyze: no idet output in log")
	}
	return result, nil
}
