// Package filtergraph builds and validates FFmpeg filtergraph strings.
//
// A graph is chains of filters; a chain is filters joined by commas, with
// optional [labels] linking chains. The builders render the common filters
// the way people write them by hand:
//
//	g := filtergraph.NewFilterGraph()
//	g.NewChain().Scale(1920, 1080).FPS(30).Format("yuv420p")
//	fmt.Println(g) // scale=w=1920:h=1080,fps=30,format=yuv420p
//
// Labelled chains for -filter_complex:
//
//	g := filtergraph.NewFilterGraph()
//	g.NewChain().Input("0:v").Scale(1920, 1080).Output("v")
//	g.NewChain().Input("0:a").Volume(0.8).Output("a")
//	fmt.Println(g) // [0:v]scale=w=1920:h=1080[v];[0:a]volume=0.8[a]
//	cmd.GlobalOptions(ffmpeg.FilterComplex(g))
//
// Input labels that start with a digit are ffmpeg stream specifiers and
// need no matching output. Any filter can be built with NewFilter and
// WithArg for options the builders do not cover. WithArg and
// WithPositionalArgs both append, in call order, so a filter written
// head-positional and tail-named comes out that way:
//
//	f := filtergraph.NewFilter("pad").
//		WithPositionalArgs("1280", "720", "-1", "-1").
//		WithArg("color", "black")
//	fmt.Println(f) // pad=1280:720:-1:-1:color=black
//
// Keys and values are escaped as ffmpeg's filter syntax requires. ffmpeg
// takes positional arguments only before the first key=value, the way
// Python takes positional arguments before keyword ones, so appending a
// positional argument after a named one fails Validate.
//
// ffmpeg has no bare-flag form in a filter's arguments: it reads every bare
// token as the value of the next option the filter declares. WithArg always
// writes a key, so a value with no key comes from WithPositionalArgs.
package filtergraph

import (
	"fmt"
	"strings"
)

// FilterGraph represents a complete filtergraph
type FilterGraph struct {
	Chains   []*FilterChain `json:"chains"`
	SwsFlags string         `json:"sws_flags,omitempty"`
}

// NewFilterGraph creates a new filter graph
func NewFilterGraph() *FilterGraph {
	return &FilterGraph{
		Chains: make([]*FilterChain, 0),
	}
}

// NewChain creates a new filter chain and adds it to the graph
func (fg *FilterGraph) NewChain() *FilterChain {
	chain := NewFilterChain()
	fg.Chains = append(fg.Chains, chain)
	return chain
}

// AddChain adds an existing chain to the graph
func (fg *FilterGraph) AddChain(chain *FilterChain) *FilterGraph {
	fg.Chains = append(fg.Chains, chain)
	return fg
}

// WithSwsFlags sets scaling flags for the filtergraph
func (fg *FilterGraph) WithSwsFlags(flags string) *FilterGraph {
	fg.SwsFlags = flags
	return fg
}

// Validate checks if the filter graph is valid
func (fg *FilterGraph) Validate() error {
	if len(fg.Chains) == 0 {
		return fmt.Errorf("filter graph cannot be empty")
	}

	for i, chain := range fg.Chains {
		if err := chain.Validate(); err != nil {
			return fmt.Errorf("chain %d validation failed: %w", i, err)
		}
	}

	labelOutputs := make(map[string]bool)
	labelInputs := make(map[string]bool)

	for _, chain := range fg.Chains {
		for _, filter := range chain.Filters {
			for _, label := range filter.OutputLabels {
				if labelOutputs[label] {
					return fmt.Errorf("duplicate output label: %s", label)
				}
				labelOutputs[label] = true
			}
			for _, label := range filter.InputLabels {
				labelInputs[label] = true
			}
		}
	}

	// ffmpeg treats any input label that starts with a digit as an input
	// stream specifier ("0", "0:v", "0:a:1", "1:a?", "0:m:language:eng"),
	// so those never need a matching output label.
	for label := range labelInputs {
		if isStreamSpecifier(label) {
			continue
		}
		if !labelOutputs[label] {
			return fmt.Errorf("input label has no corresponding output: %s", label)
		}
	}

	return nil
}

// isStreamSpecifier reports whether a label names an ffmpeg input stream
// rather than a filter output.
func isStreamSpecifier(label string) bool {
	return label != "" && label[0] >= '0' && label[0] <= '9'
}

// String returns the string representation of the filter graph
func (fg *FilterGraph) String() string {
	if len(fg.Chains) == 0 {
		return ""
	}

	var sb strings.Builder

	if fg.SwsFlags != "" {
		fmt.Fprintf(&sb, "sws_flags=%s;", fg.SwsFlags)
	}

	chainStrs := make([]string, 0, len(fg.Chains))
	for _, chain := range fg.Chains {
		if s := chain.String(); s != "" {
			chainStrs = append(chainStrs, s)
		}
	}

	sb.WriteString(strings.Join(chainStrs, ";"))
	return sb.String()
}
