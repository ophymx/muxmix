package filtergraph

import (
	"fmt"
	"strconv"
	"strings"
)

// FilterChain represents a sequence of connected filters
type FilterChain struct {
	Filters []*Filter `json:"filters"`
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

// Scale adds scale=w=W:h=H. A negative value such as -2 keeps the aspect
// ratio and rounds to a multiple of its magnitude.
func (fc *FilterChain) Scale(width, height int) *FilterChain {
	return fc.Add(NewFilter("scale").WithArg("w", strconv.Itoa(width)).WithArg("h", strconv.Itoa(height)))
}

// ScaleExpression adds a scale filter with expression-based dimensions
// such as "iw/2".
func (fc *FilterChain) ScaleExpression(widthExpr, heightExpr string) *FilterChain {
	return fc.Add(NewFilter("scale").WithArg("w", widthExpr).WithArg("h", heightExpr))
}

// FPS adds fps=RATE, printed exactly (23.976 stays 23.976).
func (fc *FilterChain) FPS(fps float64) *FilterChain {
	return fc.Add(NewFilter("fps").WithPositionalArgs(formatNumber(fps)))
}

// Format adds a format filter to the chain
func (fc *FilterChain) Format(pixelFormat string) *FilterChain {
	return fc.Add(NewFilter("format").WithPositionalArgs(pixelFormat))
}

// Crop adds crop=w=W:h=H:x=X:y=Y.
func (fc *FilterChain) Crop(width, height, x, y int) *FilterChain {
	return fc.CropExpression(strconv.Itoa(width), strconv.Itoa(height), strconv.Itoa(x), strconv.Itoa(y))
}

// CropExpression adds a crop filter with expressions such as "iw/4".
func (fc *FilterChain) CropExpression(w, h, x, y string) *FilterChain {
	return fc.Add(NewFilter("crop").WithArg("w", w).WithArg("h", h).WithArg("x", x).WithArg("y", y))
}

// Fade adds fade=type=T:start_frame=S:nb_frames=N.
func (fc *FilterChain) Fade(fadeType string, startFrame, duration int) *FilterChain {
	return fc.Add(NewFilter("fade").WithArg("type", fadeType).WithArg("start_frame", strconv.Itoa(startFrame)).WithArg("nb_frames", strconv.Itoa(duration)))
}

// Volume adds volume=FACTOR for audio, printed exactly (0.8 stays 0.8).
func (fc *FilterChain) Volume(volume float64) *FilterChain {
	return fc.Add(NewFilter("volume").WithPositionalArgs(formatNumber(volume)))
}

// Overlay adds overlay=x=X:y=Y; it takes two inputs, the base and the
// overlay.
func (fc *FilterChain) Overlay(x, y string) *FilterChain {
	return fc.Add(NewFilter("overlay").WithArg("x", x).WithArg("y", y))
}

// Split adds a split filter to the chain
func (fc *FilterChain) Split(outputs int) *FilterChain {
	if outputs <= 1 {
		outputs = 2 // Default to 2 outputs
	}
	return fc.Add(NewFilter("split").WithPositionalArgs(strconv.Itoa(outputs)))
}

// DrawText adds a drawtext filter; text is quoted as needed.
func (fc *FilterChain) DrawText(text, fontfile string, fontSize int, x, y, color string) *FilterChain {
	return fc.Add(NewFilter("drawtext").
		WithArg("text", text).WithArg("fontfile", fontfile).WithArg("fontsize", strconv.Itoa(fontSize)).
		WithArg("x", x).WithArg("y", y).WithArg("fontcolor", color))
}
