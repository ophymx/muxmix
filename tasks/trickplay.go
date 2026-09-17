package tasks

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/encode"
)

// TrickplayOptions configures scrubber preview sprite sheets.
type TrickplayOptions struct {
	// Interval between tiles. Zero picks one that yields at most MaxTiles
	// tiles, rounded to a whole number of seconds (minimum 1s).
	Interval time.Duration
	// MaxTiles bounds the tile count when Interval is zero (default 400).
	MaxTiles int
	// TileWidth in pixels (default 160); height follows the aspect ratio.
	TileWidth int
	// Columns and Rows per sheet (default 10 x 10).
	Columns, Rows int
	// SheetPattern names the sheets inside the output directory
	// (default "sheet-%03d.jpg"); its extension picks the image format.
	SheetPattern string
	// VTTName names the index file (default "thumbnails.vtt").
	VTTName string
	// BaseURL prefixes sheet names in the VTT, for example
	// "https://cdn.example.com/v/123/". Empty means relative names.
	BaseURL string
	Image   encode.Image
	// Filters run before scaling, as one filter chain, for example
	// "crop=iw/2:ih:0:0" to keep one eye of side-by-side stereo. The tile
	// height then follows the filtered frame's aspect ratio and is read
	// back from the first sheet.
	Filters []string
	// Info is an already probed description of the input, from Inspect;
	// nil probes it. Reuse it when the same file gets several tasks.
	Info *Info
}

// TrickplayResult describes generated sprite sheets.
type TrickplayResult struct {
	Sheets     []string      // sheet paths in order
	VTT        string        // path of the WebVTT index
	Interval   time.Duration // time per tile
	TileWidth  int
	TileHeight int
	Columns    int
	Rows       int
	Count      int // tiles
	Duration   time.Duration
}

// TileRect returns which sheet holds tile i and its pixel rectangle.
func (tp *TrickplayResult) TileRect(i int) (sheet int, x, y int) {
	perSheet := tp.Columns * tp.Rows
	sheet = i / perSheet
	idx := i % perSheet
	return sheet, (idx % tp.Columns) * tp.TileWidth, (idx / tp.Columns) * tp.TileHeight
}

// Trickplay generates sprite sheets and a WebVTT index into outDir.
func Trickplay(ctx context.Context, input, outDir string, o TrickplayOptions) (*TrickplayResult, error) {
	return Default.Trickplay(ctx, input, outDir, o)
}

// Trickplay generates sprite sheets and a WebVTT index into outDir.
func (t *Tools) Trickplay(ctx context.Context, input, outDir string, o TrickplayOptions) (*TrickplayResult, error) {
	info, err := t.info(ctx, input, o.Info)
	if err != nil {
		return nil, err
	}
	if !info.HasVideo {
		return nil, fmt.Errorf("tasks: %s has no video stream", input)
	}
	if info.Duration <= 0 {
		return nil, fmt.Errorf("tasks: %s has no known duration", input)
	}
	if o.MaxTiles <= 0 {
		o.MaxTiles = 400
	}
	if o.TileWidth <= 0 {
		o.TileWidth = 160
	}
	if o.Columns <= 0 {
		o.Columns = 10
	}
	if o.Rows <= 0 {
		o.Rows = 10
	}
	if o.SheetPattern == "" {
		o.SheetPattern = "sheet-%03d.jpg"
	}
	if o.VTTName == "" {
		o.VTTName = "thumbnails.vtt"
	}
	interval := o.Interval
	if interval <= 0 {
		secs := math.Ceil(info.Duration.Seconds() / float64(o.MaxTiles))
		if secs < 1 {
			secs = 1
		}
		interval = time.Duration(secs) * time.Second
	}
	img := o.Image
	if img.Format == "" {
		img.Format = encode.ImageFormatFor(o.SheetPattern)
	}
	if img.Format == encode.JPEG && img.Quality == 0 {
		img.Quality = 75
	}

	tileW := even(o.TileWidth)
	_, tileH := fitSize(info.Width, info.Height, tileW, 0)
	count := int(math.Ceil(info.Duration.Seconds() / interval.Seconds()))
	if count < 1 {
		count = 1
	}
	perSheet := o.Columns * o.Rows
	sheets := (count + perSheet - 1) / perSheet

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	pattern := filepath.Join(outDir, o.SheetPattern)
	scale := fmt.Sprintf("scale=%d:%d", tileW, tileH)
	if len(o.Filters) > 0 {
		// The filters may change the aspect ratio, so let the height follow
		// and measure it afterwards.
		scale = fmt.Sprintf("scale=%d:-2", tileW)
	}
	filters := append([]string{fmt.Sprintf("fps=%s", strconv.FormatFloat(1/interval.Seconds(), 'f', -1, 64))}, o.Filters...)
	filters = append(filters, scale, fmt.Sprintf("tile=%dx%d", o.Columns, o.Rows))
	cmd := ffmpeg.NewCommand().
		Input(input).
		Output(pattern, append(append(outputImageOpts(filters, sheets), ffmpeg.Set("start_number", "1")), img.Opts()...)...)
	if err := t.run(ctx, cmd); err != nil {
		return nil, err
	}
	if len(o.Filters) > 0 {
		sheet, err := t.prober().Probe(ctx, fmt.Sprintf(pattern, 1))
		if err != nil {
			return nil, err
		}
		if v := sheet.VideoStream(); v != nil {
			_, h := v.Resolution()
			tileH = h / o.Rows
		}
	}

	tp := &TrickplayResult{
		Interval: interval, TileWidth: tileW, TileHeight: tileH,
		Columns: o.Columns, Rows: o.Rows, Count: count, Duration: info.Duration,
		VTT: filepath.Join(outDir, o.VTTName),
	}
	for i := 1; i <= sheets; i++ {
		tp.Sheets = append(tp.Sheets, fmt.Sprintf(pattern, i))
	}
	vtt := tp.WebVTT(o.BaseURL, o.SheetPattern)
	if err := os.WriteFile(tp.VTT, []byte(vtt), 0o644); err != nil {
		return nil, err
	}
	return tp, nil
}

// WebVTT renders the thumbnail index: one cue per tile whose text is the
// sheet URL with an #xywh fragment, the form players such as Video.js,
// JW Player and hls.js consume.
func (tp *TrickplayResult) WebVTT(baseURL, sheetPattern string) string {
	var sb strings.Builder
	sb.WriteString("WEBVTT\n\n")
	for i := 0; i < tp.Count; i++ {
		start := time.Duration(i) * tp.Interval
		end := start + tp.Interval
		if end > tp.Duration {
			end = tp.Duration
		}
		sheet, x, y := tp.TileRect(i)
		fmt.Fprintf(&sb, "%s --> %s\n%s%s#xywh=%d,%d,%d,%d\n\n",
			vttTime(start), vttTime(end), baseURL, fmt.Sprintf(sheetPattern, sheet+1), x, y, tp.TileWidth, tp.TileHeight)
	}
	return sb.String()
}

func vttTime(d time.Duration) string {
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	ms := (d - s*time.Second) / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
