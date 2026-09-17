package filtergraph

import (
	"fmt"
	"strconv"
	"strings"
)

// FilterChain represents a sequence of connected filters
type FilterChain struct {
	Filters []*Filter `json:"filters"`
	parent  *FilterGraph
	// pendingInputs holds labels given by Input before any filter exists;
	// they are attached to the first filter added.
	pendingInputs []string
}

// NewFilterChain creates a new filter chain
func NewFilterChain() *FilterChain {
	return &FilterChain{
		Filters: make([]*Filter, 0),
	}
}

// Add adds a filter to the chain
func (fc *FilterChain) Add(filter *Filter) *FilterChain {
	if len(fc.Filters) == 0 && len(fc.pendingInputs) > 0 {
		filter.InputLabels = append(fc.pendingInputs, filter.InputLabels...)
		fc.pendingInputs = nil
	}
	fc.Filters = append(fc.Filters, filter)
	return fc
}

// Filter adds a generic filter with the given name
func (fc *FilterChain) Filter(name string) *FilterChain {
	fc.Filters = append(fc.Filters, NewFilter(name))
	return fc
}

// Input sets input labels for the chain (on the first filter). Labels given
// before any filter is added are attached to the first filter added.
func (fc *FilterChain) Input(labels ...string) *FilterChain {
	if len(fc.Filters) == 0 {
		fc.pendingInputs = append(fc.pendingInputs, labels...)
		return fc
	}
	fc.Filters[0].InputLabels = append(fc.Filters[0].InputLabels, labels...)
	return fc
}

// Output sets output labels for the chain (on the last filter). On an empty
// chain a pass-through "null" filter is added to carry the labels.
func (fc *FilterChain) Output(labels ...string) *FilterChain {
	if len(fc.Filters) == 0 {
		fc.Add(NewFilter("null"))
	}
	lastIdx := len(fc.Filters) - 1
	fc.Filters[lastIdx].OutputLabels = append(fc.Filters[lastIdx].OutputLabels, labels...)
	return fc
}

// Validate checks if the filter chain is valid
func (fc *FilterChain) Validate() error {
	if len(fc.Filters) == 0 {
		return fmt.Errorf("filter chain cannot be empty")
	}

	for i, filter := range fc.Filters {
		if err := filter.Validate(); err != nil {
			return fmt.Errorf("filter %d validation failed: %w", i, err)
		}
	}

	return nil
}

// String returns the string representation of the filter chain
func (fc *FilterChain) String() string {
	if len(fc.Filters) == 0 {
		return ""
	}

	filterStrs := make([]string, len(fc.Filters))
	for i, filter := range fc.Filters {
		filterStrs[i] = filter.String()
	}

	return strings.Join(filterStrs, ",")
}

// Scale adds a scale filter to the chain
func (fc *FilterChain) Scale(width, height int) *FilterChain {
	return fc.Add(NewFilter("scale").WithNamedArgs(map[string]string{
		"w": strconv.Itoa(width),
		"h": strconv.Itoa(height),
	}))
}

// ScaleExpression adds a scale filter with expression-based dimensions
func (fc *FilterChain) ScaleExpression(widthExpr, heightExpr string) *FilterChain {
	return fc.Add(NewFilter("scale").WithNamedArgs(map[string]string{
		"w": widthExpr,
		"h": heightExpr,
	}))
}

// FPS adds an fps filter to the chain
func (fc *FilterChain) FPS(fps float64) *FilterChain {
	return fc.Add(NewFilter("fps").WithNamedArgs(map[string]string{
		"fps": fmt.Sprintf("%.2f", fps),
	}))
}

// Format adds a format filter to the chain
func (fc *FilterChain) Format(pixelFormat string) *FilterChain {
	return fc.Add(NewFilter("format").WithPositionalArgs(pixelFormat))
}

// Crop adds a crop filter to the chain
func (fc *FilterChain) Crop(width, height, x, y int) *FilterChain {
	return fc.Add(NewFilter("crop").WithNamedArgs(map[string]string{
		"w": strconv.Itoa(width),
		"h": strconv.Itoa(height),
		"x": strconv.Itoa(x),
		"y": strconv.Itoa(y),
	}))
}

// CropExpression adds a crop filter with expressions
func (fc *FilterChain) CropExpression(w, h, x, y string) *FilterChain {
	return fc.Add(NewFilter("crop").WithNamedArgs(map[string]string{
		"w": w,
		"h": h,
		"x": x,
		"y": y,
	}))
}

// Fade adds a fade filter to the chain
func (fc *FilterChain) Fade(fadeType string, startFrame, duration int) *FilterChain {
	return fc.Add(NewFilter("fade").WithNamedArgs(map[string]string{
		"type":        fadeType,
		"start_frame": strconv.Itoa(startFrame),
		"nb_frames":   strconv.Itoa(duration),
	}))
}

// Volume adds a volume filter to the chain (for audio)
func (fc *FilterChain) Volume(volume float64) *FilterChain {
	return fc.Add(NewFilter("volume").WithNamedArgs(map[string]string{
		"volume": fmt.Sprintf("%.2f", volume),
	}))
}

// Overlay adds an overlay filter to the chain
func (fc *FilterChain) Overlay(x, y string) *FilterChain {
	return fc.Add(NewFilter("overlay").WithNamedArgs(map[string]string{
		"x": x,
		"y": y,
	}))
}

// Split adds a split filter to the chain
func (fc *FilterChain) Split(outputs int) *FilterChain {
	if outputs <= 1 {
		outputs = 2 // Default to 2 outputs
	}
	return fc.Add(NewFilter("split").WithPositionalArgs(strconv.Itoa(outputs)))
}

// DrawText adds a drawtext filter to the chain
func (fc *FilterChain) DrawText(text, fontfile string, fontSize int, x, y, color string) *FilterChain {
	return fc.Add(NewFilter("drawtext").WithNamedArgs(map[string]string{
		"text":      text,
		"fontfile":  fontfile,
		"fontsize":  strconv.Itoa(fontSize),
		"x":         x,
		"y":         y,
		"fontcolor": color,
	}))
}

// SetPixelFormat is an alias for Format for clarity
func (fc *FilterChain) SetPixelFormat(format string) *FilterChain {
	return fc.Format(format)
}

// SetFrameRate is an alias for FPS for clarity
func (fc *FilterChain) SetFrameRate(fps float64) *FilterChain {
	return fc.FPS(fps)
}

// Resize is an alias for Scale for clarity
func (fc *FilterChain) Resize(width, height int) *FilterChain {
	return fc.Scale(width, height)
}
