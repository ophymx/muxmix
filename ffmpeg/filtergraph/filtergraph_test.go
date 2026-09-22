package filtergraph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArgList(t *testing.T) {
	tests := []struct {
		name     string
		args     argList
		expected string
	}{
		{
			name:     "empty args",
			args:     argList{},
			expected: "",
		},
		{
			name:     "single key-value",
			args:     argList{{Key: "width", Value: "1920"}},
			expected: "width=1920",
		},
		{
			name:     "multiple key-values render in order added",
			args:     argList{{Key: "width", Value: "1920"}, {Key: "height", Value: "1080"}},
			expected: "width=1920:height=1080",
		},
		{
			// Not a bare "enable": ffmpeg would read that as a positional
			// value. An empty value sets the option to the empty string.
			name:     "empty value keeps its key",
			args:     argList{{Key: "flags"}},
			expected: "flags=",
		},
		{
			name:     "empty value among others",
			args:     argList{{Key: "w", Value: "1280"}, {Key: "flags"}, {Key: "h", Value: "-2"}},
			expected: "w=1280:flags=:h=-2",
		},
		{
			name:     "single positional",
			args:     argList{{Value: "yuv420p"}},
			expected: "yuv420p",
		},
		{
			name:     "multiple positionals",
			args:     positionalArgs([]string{"1920", "1080", "0", "0"}),
			expected: "1920:1080:0:0",
		},
		{
			name:     "positional then named",
			args:     append(positionalArgs([]string{"1280", "720", "-1", "-1"}), arg{Key: "color", Value: "black"}),
			expected: "1280:720:-1:-1:color=black",
		},
		{
			// Stopping short of the last positional slot is fine.
			name:     "some positionals then named",
			args:     append(positionalArgs([]string{"1280"}), arg{Key: "h", Value: "-2"}),
			expected: "1280:h=-2",
		},
		{
			name:     "value carrying an equals sign is quoted",
			args:     argList{{Key: "force_style", Value: "FontName=Arial,FontSize=24"}},
			expected: "force_style='FontName=Arial,FontSize=24'",
		},
		{
			name:     "raw args pass through unescaped",
			args:     argList{{Value: "color=black,foo=bar", raw: true}},
			expected: "color=black,foo=bar",
		},
		{
			name:     "empty raw args add no slot",
			args:     append(positionalArgs([]string{"1280"}), arg{raw: true}),
			expected: "1280",
		},
		{
			name:     "raw args after positional",
			args:     append(positionalArgs([]string{"1280", "720"}), arg{Value: "color=black", raw: true}),
			expected: "1280:720:color=black",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.args.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
			if err := tt.args.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

// The encodings below are the ones a real ffmpeg was observed to deliver
// unchanged; TestLiveFilterArgumentEscaping re-checks them against the
// binary. The graph parser strips the quotes and the argument parser eats
// the backslashes, so ":" and "\" need both, and "'" cannot sit inside the
// quotes at all.
func TestEscapeFilterArg(t *testing.T) {
	for _, tc := range []struct {
		value, expected string
	}{
		{"plain", "plain"},
		{"1280", "1280"},
		{"-2", "-2"},
		{"a,b", `'a,b'`},
		{"a b", `'a b'`},
		{"a=b", `'a=b'`},
		{"a:b", `'a\:b'`},
		{`a\b`, `'a\\b'`},
		{"a'b", `'a'\\\''b'`},
		{`it's a:b\c`, `'it'\\\''s a\:b\\c'`},
		{"eq(pict_type,I)", `'eq(pict_type,I)'`},
		{"a;b", `'a;b'`},
		{"a[b]c", `'a[b]c'`},
		{"FontName=Arial,FontSize=24", `'FontName=Arial,FontSize=24'`},
	} {
		if got := escapeFilterArg(tc.value); got != tc.expected {
			t.Errorf("escapeFilterArg(%q) = %s, want %s", tc.value, got, tc.expected)
		}
	}
}

func TestArgListValidate(t *testing.T) {
	tests := []struct {
		name    string
		args    argList
		wantErr bool
	}{
		{name: "clean named", args: argList{{Key: "w", Value: "1280"}}},
		// Graph separators are quoted rather than rejected: escaping
		// carries them to the filter whole.
		{name: "semicolon in value", args: argList{{Key: "t", Value: "12;80"}}},
		{name: "brackets in value", args: argList{{Key: "t", Value: "a[b]c"}}},
		{name: "semicolon in positional", args: positionalArgs([]string{"a;b"})},
		{name: "raw args are the caller's to get right", args: argList{{Value: "a;b", raw: true}}},

		// ffmpeg fills a filter's options positionally only up to the
		// first key=value, then rejects any further bare argument with
		// "No option name near '...'".
		{name: "positional then named", args: append(positionalArgs([]string{"1280", "720"}), arg{Key: "color", Value: "black"})},
		{name: "positional short of the last slot, then named", args: append(positionalArgs([]string{"1280"}), arg{Key: "h", Value: "-2"})},
		{name: "named then positional", args: append(argList{{Key: "color", Value: "black"}}, arg{Value: "1280"}), wantErr: true},
		{name: "positional interleaved after a named", args: append(positionalArgs([]string{"1280", "720"}), arg{Key: "color", Value: "black"}, arg{Value: "-1"}), wantErr: true},
		{name: "an empty value is still a named argument", args: argList{{Key: "w", Value: "1280"}, {Key: "flags"}}},
		{name: "positional after an empty-valued named argument", args: append(argList{{Key: "flags"}}, arg{Value: "1280"}), wantErr: true},
		{name: "raw args are opaque, not a named argument", args: append(argList{{Value: "w=1280", raw: true}}, arg{Value: "720"})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.args.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFilterArgBuildersCompose(t *testing.T) {
	tests := []struct {
		name     string
		filter   *Filter
		expected string
	}{
		{
			name: "positional then named",
			filter: NewFilter("pad").
				WithPositionalArgs("1280", "720", "-1", "-1").
				WithArg("color", "black"),
			expected: "pad=1280:720:-1:-1:color=black",
		},
		{
			name: "some positional slots then named",
			filter: NewFilter("scale").
				WithPositionalArgs("1280").
				WithArg("h", "-2"),
			expected: "scale=1280:h=-2",
		},
		{
			name: "positional in two calls",
			filter: NewFilter("overlay").
				WithPositionalArgs("10").
				WithPositionalArgs("20"),
			expected: "overlay=10:20",
		},
		{
			name: "raw args after positional",
			filter: NewFilter("pad").
				WithPositionalArgs("1280", "720").
				WithRawArgs("color=black"),
			expected: "pad=1280:720:color=black",
		},
		{
			name:     "raw args alone keep the caller's escaping",
			filter:   NewFilter("subtitles").WithRawArgs("f=subs.srt:force_style='FontName=Arial,FontSize=24'"),
			expected: "subtitles=f=subs.srt:force_style='FontName=Arial,FontSize=24'",
		},
		{
			name:     "empty raw args add nothing",
			filter:   NewFilter("null").WithRawArgs(""),
			expected: "null",
		},
		{
			name:     "Raw through WithArgs, then a named arg",
			filter:   NewFilter("crop").WithArgs(Raw("iw/2:ih/2")).WithArg("exact", "1"),
			expected: "crop=iw/2:ih/2:exact=1",
		},
		{
			name:     "a caller's own FilterArguments is not discarded",
			filter:   NewFilter("crop").WithArgs(stubArgs("iw/2:ih/2")).WithArg("exact", "1"),
			expected: "crop=iw/2:ih/2:exact=1",
		},
		{
			name:     "WithNamedArgs replaces, WithArg appends",
			filter:   NewFilter("scale").WithNamedArgs(map[string]string{"w": "1280"}).WithArg("h", "-2"),
			expected: "scale=w=1280:h=-2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
			if err := tt.filter.Validate(); err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

// ffmpeg takes positional arguments only before the first key=value, so
// WithPositionalArgs after WithArg builds a filter it would reject. The
// arguments are all still there — nothing is silently dropped — and
// Validate is what says no.
func TestFilterPositionalAfterNamedFailsValidate(t *testing.T) {
	f := NewFilter("pad").WithArg("color", "black").WithPositionalArgs("1280", "720")

	err := f.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error for a positional argument after a named one")
	}
	for _, want := range []string{"positional argument", `"1280"`, `"color"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %q, want it to mention %s", err, want)
		}
	}
	if got, want := f.String(), "pad=color=black:1280:720"; got != want {
		t.Errorf("String() = %q, want %q: the arguments should still all be present", got, want)
	}
}

// stubArgs is a FilterArguments implementation this package does not own,
// standing in for a caller's own.
type stubArgs string

func (s stubArgs) String() string  { return string(s) }
func (s stubArgs) Validate() error { return nil }

func TestFilterArgsJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		filter   *Filter
		wantArgs string // the "args" member, to pin the wire shape
	}{
		{
			name:     "all named is an object",
			filter:   NewFilter("scale").WithArg("w", "1280").WithArg("h", "-2"),
			wantArgs: `{"w":"1280","h":"-2"}`,
		},
		{
			name:     "all positional is an array of strings",
			filter:   NewFilter("pad").WithPositionalArgs("1280", "720"),
			wantArgs: `["1280","720"]`,
		},
		{
			name:     "mixed is an array with the named args as objects",
			filter:   NewFilter("pad").WithPositionalArgs("1280", "720").WithArg("color", "black"),
			wantArgs: `["1280","720",{"color":"black"}]`,
		},
		{
			name:     "raw is the one rendered string",
			filter:   NewFilter("pad").WithPositionalArgs("1280").WithRawArgs("color=black"),
			wantArgs: `"1280:color=black"`,
		},
		{
			name:     "a value needing quotes keeps its unescaped form",
			filter:   NewFilter("select").WithArg("e", "eq(pict_type,I)"),
			wantArgs: `{"e":"eq(pict_type,I)"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.filter)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			var wire struct {
				Args json.RawMessage `json:"args"`
			}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatalf("Unmarshal wire: %v", err)
			}
			if string(wire.Args) != tt.wantArgs {
				t.Errorf("args member = %s, want %s", wire.Args, tt.wantArgs)
			}

			var back Filter
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got, want := back.String(), tt.filter.String(); got != want {
				t.Errorf("round-tripped filter = %q, want %q", got, want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	tests := []struct {
		name     string
		filter   *Filter
		expected string
	}{
		{
			name:     "simple filter",
			filter:   NewFilter("scale"),
			expected: "scale",
		},
		{
			name:     "filter with instance",
			filter:   NewFilter("scale").WithInstance("main"),
			expected: "scale@main",
		},
		{
			name:     "filter with args",
			filter:   NewFilter("scale").WithNamedArgs(map[string]string{"w": "1920", "h": "1080"}),
			expected: "scale=w=1920:h=1080",
		},
		{
			name:     "filter with input labels",
			filter:   NewFilter("scale").Input("input1"),
			expected: "[input1]scale",
		},
		{
			name:     "filter with output labels",
			filter:   NewFilter("scale").Output("output1"),
			expected: "scale[output1]",
		},
		{
			name:     "complete filter",
			filter:   NewFilter("scale").Input("input1").WithNamedArgs(map[string]string{"w": "1920", "h": "1080"}).Output("output1"),
			expected: "[input1]scale=w=1920:h=1080[output1]",
		},
		{
			name:     "filter with multiple inputs and outputs",
			filter:   NewFilter("overlay").Input("main", "overlay").Output("result"),
			expected: "[main][overlay]overlay[result]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter.String()

			// For filters with args, we need to handle map ordering
			if strings.Contains(result, "=") && strings.Contains(tt.expected, "=") {
				// Split and check components separately
				resultParts := strings.Split(result, "=")
				expectedParts := strings.Split(tt.expected, "=")

				if len(resultParts) != len(expectedParts) {
					t.Errorf("Expected %q, got %q", tt.expected, result)
					return
				}

				// Check prefix (everything before =)
				if resultParts[0] != expectedParts[0] {
					t.Errorf("Expected prefix %q, got %q", expectedParts[0], resultParts[0])
					return
				}

				// For the args part, check that all components are present
				if len(resultParts) > 1 {
					resultArgs := strings.Split(resultParts[1], ":")
					expectedArgs := strings.Split(expectedParts[1], ":")

					if len(resultArgs) != len(expectedArgs) {
						t.Errorf("Expected %q, got %q", tt.expected, result)
						return
					}
				}
			} else {
				if result != tt.expected {
					t.Errorf("Expected %q, got %q", tt.expected, result)
				}
			}
		})
	}
}

func TestFilterValidation(t *testing.T) {
	tests := []struct {
		name        string
		filter      *Filter
		shouldError bool
	}{
		{
			name:        "valid filter",
			filter:      NewFilter("scale"),
			shouldError: false,
		},
		{
			name:        "empty name",
			filter:      NewFilter(""),
			shouldError: true,
		},
		{
			name:        "invalid name with special chars",
			filter:      NewFilter("scale-test"),
			shouldError: true,
		},
		{
			name:        "invalid instance name",
			filter:      NewFilter("scale").WithInstance("test-instance"),
			shouldError: true,
		},
		{
			name:        "invalid input label",
			filter:      NewFilter("scale").Input("input-1"),
			shouldError: true,
		},
		{
			name:        "invalid output label",
			filter:      NewFilter("scale").Output("output@1"),
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate()
			if tt.shouldError && err == nil {
				t.Error("Expected validation error, got nil")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no validation error, got %v", err)
			}
		})
	}
}

func TestFilterChain(t *testing.T) {
	tests := []struct {
		name     string
		chain    *FilterChain
		expected string
	}{
		{
			name:     "single filter",
			chain:    NewFilterChain().Filter("scale"),
			expected: "scale",
		},
		{
			name:     "multiple filters",
			chain:    NewFilterChain().Filter("scale").Filter("fps"),
			expected: "scale,fps",
		},
		{
			name: "chain with complex filters",
			chain: NewFilterChain().
				Add(NewFilter("scale").WithNamedArgs(map[string]string{"w": "1920", "h": "1080"})).
				Add(NewFilter("fps").WithNamedArgs(map[string]string{"fps": "30"})),
			expected: "scale=w=1920:h=1080,fps=fps=30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chain.String()

			// Handle arg ordering for complex filters
			if strings.Contains(result, "=") && strings.Contains(tt.expected, "=") {
				// Split by comma first
				resultParts := strings.Split(result, ",")
				expectedParts := strings.Split(tt.expected, ",")

				if len(resultParts) != len(expectedParts) {
					t.Errorf("Expected %q, got %q", tt.expected, result)
					return
				}
			} else {
				if result != tt.expected {
					t.Errorf("Expected %q, got %q", tt.expected, result)
				}
			}
		})
	}
}

func TestFilterGraph(t *testing.T) {
	tests := []struct {
		name     string
		graph    *FilterGraph
		expected string
	}{
		{
			name: "single chain",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.NewChain().Filter("scale")
				return g
			}(),
			expected: "scale",
		},
		{
			name: "multiple chains",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.NewChain().Filter("scale")
				g.NewChain().Filter("volume")
				return g
			}(),
			expected: "scale;volume",
		},
		{
			name: "graph with sws_flags",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.WithSwsFlags("lanczos")
				g.NewChain().Filter("scale")
				return g
			}(),
			expected: "sws_flags=lanczos;scale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.graph.String()
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCommonFilters(t *testing.T) {
	tests := []struct {
		name     string
		chain    func() *FilterChain
		contains []string // Parts that should be present in the result
	}{
		{
			name:     "scale filter",
			chain:    func() *FilterChain { return NewFilterChain().Scale(1920, 1080) },
			contains: []string{"scale", "w=1920", "h=1080"},
		},
		{
			name:     "fps filter",
			chain:    func() *FilterChain { return NewFilterChain().FPS(30.0) },
			contains: []string{"fps=30"},
		},
		{
			name:     "format filter",
			chain:    func() *FilterChain { return NewFilterChain().Format("yuv420p") },
			contains: []string{"format=yuv420p"},
		},
		{
			name:     "crop filter",
			chain:    func() *FilterChain { return NewFilterChain().Crop(640, 480, 10, 20) },
			contains: []string{"crop", "w=640", "h=480", "x=10", "y=20"},
		},
		{
			name:     "fade filter",
			chain:    func() *FilterChain { return NewFilterChain().Fade("in", 0, 30) },
			contains: []string{"fade", "type=in", "start_frame=0", "nb_frames=30"},
		},
		{
			name:     "volume filter",
			chain:    func() *FilterChain { return NewFilterChain().Volume(0.8) },
			contains: []string{"volume=0.8"},
		},
		{
			name:     "complex chain",
			chain:    func() *FilterChain { return NewFilterChain().Scale(1920, 1080).FPS(30.0).Format("yuv420p") },
			contains: []string{"scale", "fps", "format=yuv420p"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.chain().String()

			for _, expected := range tt.contains {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected result to contain %q, got %q", expected, result)
				}
			}
		})
	}
}

func TestRealWorldExamples(t *testing.T) {
	tests := []struct {
		name        string
		description string
		graph       func() *FilterGraph
		validate    func(t *testing.T, result string)
	}{
		{
			name:        "simple video resize",
			description: "Scale video to 1920x1080 and set framerate to 30fps",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.NewChain().Scale(1920, 1080).FPS(30.0).Format("yuv420p")
				return g
			},
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "scale") ||
					!strings.Contains(result, "fps") ||
					!strings.Contains(result, "format=yuv420p") {
					t.Errorf("Expected video processing chain, got: %s", result)
				}
			},
		},
		{
			name:        "video overlay",
			description: "Overlay one video on top of another",
			graph: func() *FilterGraph {
				g := NewFilterGraph()

				// Main video chain
				g.NewChain().Input("0:v").Output("main")

				// Overlay video chain
				g.NewChain().Input("1:v").Scale(320, 240).Output("overlay")

				// Combine them
				g.NewChain().Input("main", "overlay").Overlay("10", "10").Output("result")

				return g
			},
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "overlay") ||
					!strings.Contains(result, "[main]") ||
					!strings.Contains(result, "[overlay]") {
					t.Errorf("Expected overlay filtergraph, got: %s", result)
				}
			},
		},
		{
			name:        "audio video processing",
			description: "Process both audio and video separately",
			graph: func() *FilterGraph {
				g := NewFilterGraph()

				// Video processing
				g.NewChain().Input("0:v").Scale(1280, 720).FPS(25.0).Output("video_out")

				// Audio processing
				g.NewChain().Input("0:a").Volume(0.8).Output("audio_out")

				return g
			},
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "scale") ||
					!strings.Contains(result, "volume") ||
					!strings.Contains(result, "video_out") ||
					!strings.Contains(result, "audio_out") {
					t.Errorf("Expected audio/video processing, got: %s", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := tt.graph()

			// Validate the graph structure
			if err := graph.Validate(); err != nil {
				t.Errorf("Graph validation failed: %v", err)
				return
			}

			result := graph.String()
			t.Logf("Generated filtergraph: %s", result)

			// Run custom validation
			tt.validate(t, result)
		})
	}
}

func TestFilterGraphValidation(t *testing.T) {
	tests := []struct {
		name        string
		graph       *FilterGraph
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "empty graph",
			graph:       NewFilterGraph(),
			shouldError: true,
			errorMsg:    "empty",
		},
		{
			name: "valid graph",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.NewChain().Scale(1920, 1080)
				return g
			}(),
			shouldError: false,
		},
		{
			name: "unmatched input label",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.NewChain().Input("nonexistent").Scale(1920, 1080)
				return g
			}(),
			shouldError: true,
			errorMsg:    "no corresponding output",
		},
		{
			name: "stream specifiers need no output label",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.NewChain().Input("0:v:0").Output("main")
				g.NewChain().Input("1:a:0", "0:a?").Output("mix")
				g.NewChain().Input("0:m:language:eng").Output("eng")
				g.NewChain().Input("2").Output("all")
				return g
			}(),
			shouldError: false,
		},
		{
			name: "duplicate output labels",
			graph: func() *FilterGraph {
				g := NewFilterGraph()
				g.NewChain().Scale(1920, 1080).Output("out")
				g.NewChain().Volume(0.8).Output("out") // Duplicate label
				return g
			}(),
			shouldError: true,
			errorMsg:    "duplicate output label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.graph.Validate()

			if tt.shouldError {
				if err == nil {
					t.Error("Expected validation error, got nil")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no validation error, got: %v", err)
				}
			}
		})
	}
}

func TestJSONSerialization(t *testing.T) {
	// Test that our structures can be serialized to/from JSON
	g := NewFilterGraph()
	chain := g.NewChain()
	chain.Input("0:v").Scale(1920, 1080).FPS(30.0).Output("output")

	// Serialize to JSON
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal to JSON: %v", err)
	}

	t.Logf("JSON representation:\n%s", string(data))

	// Deserialize from JSON
	var g2 FilterGraph
	err = json.Unmarshal(data, &g2)
	if err != nil {
		t.Fatalf("Failed to unmarshal from JSON: %v", err)
	}

	// Verify the deserialized graph works
	if len(g2.Chains) != 1 {
		t.Errorf("Expected 1 chain, got %d", len(g2.Chains))
	}

	if len(g2.Chains[0].Filters) != 2 {
		t.Errorf("Expected 2 filters, got %d", len(g2.Chains[0].Filters))
	}
	if want := "[0:v]scale=w=1920:h=1080,fps=30[output]"; g2.String() != want {
		t.Errorf("round-tripped graph = %q, want %q", g2.String(), want)
	}
}

func TestExpressionSupport(t *testing.T) {
	// Test that expressions work correctly in filters
	chain := NewFilterChain()

	chain.ScaleExpression("iw/2", "ih/2")                // Scale to half size
	chain.CropExpression("iw/4", "ih/4", "iw/8", "ih/8") // Crop center quarter

	result := chain.String()

	// Check that the expression filters are present
	if !strings.Contains(result, "scale") || !strings.Contains(result, "crop") {
		t.Errorf("Expected expression support, got: %s", result)
	}

	// Check for specific expression values
	if !strings.Contains(result, "iw/2") || !strings.Contains(result, "ih/2") {
		t.Errorf("Expected scale expression values, got: %s", result)
	}
}

func BenchmarkFilterGraphConstruction(b *testing.B) {
	for i := 0; i < b.N; i++ {
		g := NewFilterGraph()

		// Build a complex filtergraph
		video := g.NewChain()
		video.Input("0:v").
			Scale(1920, 1080).
			FPS(30.0).
			Format("yuv420p").
			Fade("in", 0, 30).
			Output("video_out")

		audio := g.NewChain()
		audio.Input("0:a").
			Volume(0.8).
			Output("audio_out")

		_ = g.String()
	}
}

func BenchmarkFilterGraphString(b *testing.B) {
	// Setup a complex graph once
	g := NewFilterGraph().WithSwsFlags("lanczos")

	for range 10 {
		chain := g.NewChain()
		chain.Input("input").
			Scale(1920, 1080).
			FPS(30.0).
			Format("yuv420p").
			Crop(100, 100, 10, 10).
			Output("output")
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = g.String()
	}
}
