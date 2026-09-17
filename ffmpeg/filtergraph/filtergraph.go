// Package filtergraph builds and validates FFmpeg filtergraph strings.
//
// This package offers a type-safe, fluent API for building complex FFmpeg filter graphs
// with validation, common filter builders, and optimization capabilities.
//
// Basic usage:
//
// graph := filtergraph.NewFilterGraph()
// chain := graph.NewChain()
// chain.Scale(1920, 1080).FPS(30).Format("yuv420p")
// fmt.Println(graph.String()) // scale=w=1920:h=1080,fps=fps=30.00,format=yuv420p
//
// Advanced usage with multiple chains:
//
// graph := filtergraph.NewFilterGraph()
//
// // Video processing chain
// video := graph.NewChain()
// video.Input("0:v").Scale(1920, 1080).FPS(30).Output("v_out")
//
// // Audio processing chain
// audio := graph.NewChain()
// audio.Input("0:a").Volume(0.8).Output("a_out")
//
// fmt.Println(graph.String()) // [0:v]scale=w=1920:h=1080,fps=fps=30.00[v_out];[0:a]volume=volume=0.80[a_out]
package filtergraph

import (
	"fmt"
	"regexp"
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
	chain.parent = fg
	fg.Chains = append(fg.Chains, chain)
	return chain
}

// AddChain adds an existing chain to the graph
func (fg *FilterGraph) AddChain(chain *FilterChain) *FilterGraph {
	chain.parent = fg
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

	// Skip FFmpeg input stream specifiers like "0:v", "1:a", etc.
	streamSpecifierPattern := regexp.MustCompile(`^[0-9]+:[vaspd]$`)

	for label := range labelInputs {
		if streamSpecifierPattern.MatchString(label) {
			continue
		}
		if !labelOutputs[label] {
			return fmt.Errorf("input label has no corresponding output: %s", label)
		}
	}

	return nil
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
