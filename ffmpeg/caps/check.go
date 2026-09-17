package caps

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ophymx/muxmix/ffmpeg"
)

// Problem is one thing a Command asks for that the ffmpeg build cannot do.
type Problem struct {
	Where       string   // "global", "input 0", "output 1"
	Option      string   // the option name, e.g. "c:v"
	Value       string   // the offending value
	Message     string   // what is wrong
	Suggestions []string // alternatives the build does have
}

func (p Problem) String() string {
	s := fmt.Sprintf("%s: -%s %s: %s", p.Where, p.Option, p.Value, p.Message)
	if len(p.Suggestions) > 0 {
		s += " (available: " + strings.Join(p.Suggestions, ", ") + ")"
	}
	return s
}

// CheckError lists every problem found; Check reports all of them at once.
type CheckError struct {
	Problems []Problem
}

func (e *CheckError) Error() string {
	if len(e.Problems) == 1 {
		return "ffmpeg command: " + e.Problems[0].String()
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "ffmpeg command has %d problems:", len(e.Problems))
	for _, p := range e.Problems {
		sb.WriteString("\n  " + p.String())
	}
	return sb.String()
}

// Checker validates Commands against a Set and, when it has a runner or
// preloaded Help tables, against each encoder's option table.
type Checker struct {
	set    *Set
	runner ffmpeg.Runner

	mu    sync.Mutex
	helps map[string]*Help
}

// NewChecker returns a Checker. runner may be nil, in which case encoder
// option values are only checked for encoders whose Help was added with
// AddHelp.
func NewChecker(set *Set, runner ffmpeg.Runner) *Checker {
	return &Checker{set: set, runner: runner, helps: map[string]*Help{}}
}

// AddHelp preloads an option table so it need not be fetched.
func (c *Checker) AddHelp(h *Help) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.helps[h.Kind+":"+h.Name] = h
}

// Check validates a Command against the build and returns a *CheckError
// listing every problem, or nil.
func Check(ctx context.Context, set *Set, runner ffmpeg.Runner, cmd *ffmpeg.Command) error {
	return NewChecker(set, runner).Check(ctx, cmd)
}

// Check validates cmd. It never runs the command.
func (c *Checker) Check(ctx context.Context, cmd *ffmpeg.Command) error {
	var problems []Problem
	add := func(where, option, value, msg string, suggestions ...string) {
		problems = append(problems, Problem{Where: where, Option: option, Value: value, Message: msg, Suggestions: suggestions})
	}

	for _, a := range cmd.Global {
		switch a.Name {
		case "filter_complex", "lavfi":
			c.checkFilters("global", a, add)
		case "init_hw_device":
			typ := a.Value
			if i := strings.IndexAny(typ, "=:"); i >= 0 {
				typ = typ[:i]
			}
			if !c.set.HasHWAccel(typ) {
				add("global", a.Name, a.Value, "hardware device type not available", c.set.HWAccels...)
			}
		}
	}

	for i, in := range cmd.Inputs {
		where := fmt.Sprintf("input %d", i)
		for _, a := range in.Options {
			switch {
			case a.Name == "f":
				if !c.set.HasDemuxer(a.Value) {
					add(where, a.Name, a.Value, "no such demuxer", c.suggestFormats(a.Value, c.set.Demuxers)...)
				}
			case a.Name == "hwaccel":
				if a.Value != "auto" && !c.set.HasHWAccel(a.Value) {
					add(where, a.Name, a.Value, "hardware acceleration not available", c.set.HWAccels...)
				}
			case isCodecOption(a.Name):
				if a.Value != "copy" && !c.set.HasDecoder(a.Value) {
					add(where, a.Name, a.Value, "no such decoder", names(c.set.DecodersFor(a.Value))...)
				}
			}
		}
	}

	for i, out := range cmd.Outputs {
		where := fmt.Sprintf("output %d", i)
		var encoders []*Help
		for _, a := range out.Options {
			switch {
			case a.Name == "f":
				if !c.set.HasMuxer(a.Value) {
					add(where, a.Name, a.Value, "no such muxer", c.suggestFormats(a.Value, c.set.Muxers)...)
				}
			case isCodecOption(a.Name):
				if a.Value == "copy" {
					continue
				}
				if !c.set.HasEncoder(a.Value) {
					add(where, a.Name, a.Value, "no such encoder", c.suggestEncoders(a.Value)...)
					continue
				}
				if h := c.help(ctx, "encoder", a.Value); h != nil {
					encoders = append(encoders, h)
				}
			case a.Name == "pix_fmt":
				if c.set.PixelFormat(a.Value) == nil {
					add(where, a.Name, a.Value, "no such pixel format")
				}
			case a.Name == "vf" || a.Name == "af" || a.Name == "filter" || strings.HasPrefix(a.Name, "filter:"):
				c.checkFilters(where, a, add)
			}
		}
		// Encoder-specific options and pixel format support.
		for _, a := range out.Options {
			if a.Name == "pix_fmt" {
				for _, h := range encoders {
					if len(h.PixelFormats) > 0 && !h.SupportsPixelFormat(a.Value) {
						add(where, a.Name, a.Value, "not supported by "+h.Name, h.PixelFormats...)
					}
				}
				continue
			}
			base := a.Name
			if i := strings.Index(base, ":"); i >= 0 {
				base = base[:i]
			}
			for _, h := range encoders {
				opt := h.Option(base)
				if opt == nil {
					continue
				}
				if msg := checkOptionValue(opt, a.Value); msg != "" {
					add(where, a.Name, a.Value, msg+" for "+h.Name, constantNames(opt)...)
				}
				break
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return &CheckError{Problems: problems}
}

func (c *Checker) help(ctx context.Context, kind, name string) *Help {
	key := kind + ":" + name
	c.mu.Lock()
	h, ok := c.helps[key]
	c.mu.Unlock()
	if ok {
		return h
	}
	if c.runner == nil || ctx == nil {
		return nil
	}
	h, err := HelpFor(ctx, c.runner, kind, name)
	if err != nil {
		h = nil
	}
	c.mu.Lock()
	c.helps[key] = h
	c.mu.Unlock()
	return h
}

func (c *Checker) checkFilters(where string, a ffmpeg.Arg, add func(where, option, value, msg string, suggestions ...string)) {
	for _, name := range FilterNames(a.Value) {
		if !c.set.HasFilter(name) {
			add(where, a.Name, name, "no such filter", c.suggestFilters(name)...)
		}
	}
}

func (c *Checker) suggestEncoders(value string) []string {
	if enc := c.set.EncodersFor(value); len(enc) > 0 {
		return names(enc)
	}
	var out []string
	for _, e := range c.set.Encoders {
		if strings.Contains(e.Name, value) || strings.Contains(value, e.CodecID) && e.CodecID != "" {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return limit(out, 8)
}

func (c *Checker) suggestFormats(value string, list []Format) []string {
	var out []string
	for _, f := range list {
		for _, n := range f.Names() {
			if strings.Contains(n, value) || strings.Contains(value, n) {
				out = append(out, n)
				break
			}
		}
	}
	sort.Strings(out)
	return limit(out, 8)
}

func (c *Checker) suggestFilters(value string) []string {
	var out []string
	for _, f := range c.set.Filters {
		if strings.Contains(f.Name, value) || strings.Contains(value, f.Name) {
			out = append(out, f.Name)
		}
	}
	sort.Strings(out)
	return limit(out, 8)
}

func names(codecs []Codec) []string {
	out := make([]string, len(codecs))
	for i, c := range codecs {
		out[i] = c.Name
	}
	return out
}

func limit(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func isCodecOption(name string) bool {
	switch {
	case name == "c", name == "vcodec", name == "acodec", name == "scodec", name == "dcodec":
		return true
	case strings.HasPrefix(name, "c:"), strings.HasPrefix(name, "codec:"):
		return true
	}
	return false
}

func constantNames(opt *Option) []string {
	var out []string
	for _, c := range opt.Constants {
		out = append(out, c.Name)
	}
	return out
}

// checkOptionValue validates a value against an AVOption; it returns "" when
// the value is acceptable or cannot be judged.
func checkOptionValue(opt *Option, value string) string {
	value = strings.TrimSpace(value)
	if opt.Constant(value) != nil {
		return ""
	}
	switch opt.Type {
	case "int", "int64", "uint64", "float", "double":
		f, ok := parseNumber(value)
		if !ok {
			if len(opt.Constants) > 0 {
				return "not a number or a named value"
			}
			return "not a number"
		}
		if min, ok := parseBound(opt.Min); ok && f < min {
			return fmt.Sprintf("below minimum %s", opt.Min)
		}
		if max, ok := parseBound(opt.Max); ok && f > max {
			return fmt.Sprintf("above maximum %s", opt.Max)
		}
	case "boolean":
		switch strings.ToLower(value) {
		case "true", "false", "0", "1", "auto", "yes", "no", "on", "off", "-1":
		default:
			return "not a boolean"
		}
	case "flags":
		if len(opt.Constants) == 0 {
			return ""
		}
		for _, tok := range strings.FieldsFunc(value, func(r rune) bool { return r == '+' || r == '-' || r == '|' }) {
			if tok == "" {
				continue
			}
			if opt.Constant(tok) == nil {
				if _, ok := parseNumber(tok); !ok {
					return fmt.Sprintf("unknown flag %q", tok)
				}
			}
		}
	}
	return ""
}

// parseNumber parses an AVOption numeric value, accepting SI suffixes as
// ffmpeg does (k, K, M, G) and a trailing i for binary multiples.
func parseNumber(s string) (float64, bool) {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	mult := 1.0
	base := s
	if strings.HasSuffix(base, "i") {
		base = strings.TrimSuffix(base, "i")
	}
	if len(base) > 1 {
		switch base[len(base)-1] {
		case 'k', 'K':
			mult = 1000
		case 'M':
			mult = 1e6
		case 'G':
			mult = 1e9
		default:
			return 0, false
		}
		base = base[:len(base)-1]
	}
	f, err := strconv.ParseFloat(base, 64)
	if err != nil {
		return 0, false
	}
	return f * mult, true
}

// parseBound turns an AVOption range bound into a number; symbolic bounds
// such as INT_MAX and FLT_MAX are treated as unbounded.
func parseBound(s string) (float64, bool) {
	switch s {
	case "", "INT_MIN", "INT_MAX", "INT64_MIN", "INT64_MAX", "UINT32_MAX", "FLT_MIN", "FLT_MAX", "DBL_MIN", "DBL_MAX", "-INF", "INF", "I64_MIN", "I64_MAX":
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// FilterNames extracts the filter names used in a filtergraph string,
// ignoring pad labels, instance names, arguments and sws_flags.
func FilterNames(graph string) []string {
	var out []string
	var seg strings.Builder
	depth, quoted := 0, false
	flush := func() {
		s := strings.TrimSpace(seg.String())
		seg.Reset()
		if s == "" {
			return
		}
		// Strip leading and trailing [labels].
		for strings.HasPrefix(s, "[") {
			end := strings.Index(s, "]")
			if end < 0 {
				return
			}
			s = strings.TrimSpace(s[end+1:])
		}
		if i := strings.IndexAny(s, "=@["); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
		if s == "" || s == "sws_flags" {
			return
		}
		out = append(out, s)
	}
	for _, r := range graph {
		switch {
		case r == '\'':
			quoted = !quoted
			seg.WriteRune(r)
		case quoted:
			seg.WriteRune(r)
		case r == '[':
			depth++
			seg.WriteRune(r)
		case r == ']':
			depth--
			seg.WriteRune(r)
		case (r == ',' || r == ';') && depth == 0:
			flush()
		default:
			seg.WriteRune(r)
		}
	}
	flush()
	return out
}
