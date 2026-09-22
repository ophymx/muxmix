package filtergraph

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Filter represents a single filter in the filtergraph
type Filter struct {
	InputLabels  []string        `json:"input_labels"`
	Name         string          `json:"name"`
	Instance     string          `json:"instance,omitempty"` // Optional instance name (@instance)
	Args         FilterArguments `json:"args,omitempty"`
	OutputLabels []string        `json:"output_labels"`
}

// NewFilter creates a new filter with the given name
func NewFilter(name string) *Filter {
	return &Filter{
		Name:         name,
		InputLabels:  make([]string, 0),
		OutputLabels: make([]string, 0),
	}
}

// WithInstance sets an instance name for the filter
func (f *Filter) WithInstance(instance string) *Filter {
	f.Instance = instance
	return f
}

// WithArgs replaces the filter's arguments with a FilterArguments
// implementation of your own, or with [Raw]. Arguments added afterwards by
// WithArg, WithPositionalArgs or WithRawArgs are appended to its rendering.
func (f *Filter) WithArgs(args FilterArguments) *Filter {
	f.Args = args
	return f
}

// WithNamedArgs replaces the filter's arguments with key=value arguments
// from a map; they render in key order. Use WithArg to control the order.
func (f *Filter) WithNamedArgs(args map[string]string) *Filter {
	f.Args = argsFromMap(args)
	return f
}

// WithArg appends one key=value argument. The value is escaped as ffmpeg
// needs and may hold any character; the key may not, because ffmpeg reads
// an option name with no escaping of its own, so a key needing quotes fails
// Validate. See [argList.Validate].
//
// An empty value renders as key= and sets the option to the empty string,
// which options taking a string accept and options taking a number or an
// expression reject by name. An empty key makes the argument positional,
// the same as WithPositionalArgs.
func (f *Filter) WithArg(key, value string) *Filter {
	return f.appendArgs(argList{{Key: key, Value: value}})
}

// WithPositionalArgs appends positional arguments, each escaped as ffmpeg
// needs. Positional and named arguments share one ordered list, so calling
// this and WithArg in the order the filter wants renders that order:
//
//	filtergraph.NewFilter("pad").
//		WithPositionalArgs("1280", "720", "-1", "-1").
//		WithArg("color", "black") // pad=1280:720:-1:-1:color=black
//
// Not every positional slot has to be filled before switching to names —
// scale=1280:h=-2 is valid — but no positional argument may follow a named
// one, so calling this after WithArg builds a filter that fails Validate
// and that no ffmpeg reads as written. See [argList.Validate] for why.
func (f *Filter) WithPositionalArgs(args ...string) *Filter {
	return f.appendArgs(positionalArgs(args))
}

// WithRawArgs appends a pre-formed argument string, rendered exactly as
// given. See [Raw] for what the caller takes on in return.
func (f *Filter) WithRawArgs(args string) *Filter {
	return f.appendArgs(argList{{Value: args, raw: true}})
}

// appendArgs adds to the filter's arguments, keeping a foreign
// FilterArguments implementation whole rather than its rendering, so its
// Validate survives. See [joinedArgs].
func (f *Filter) appendArgs(add argList) *Filter {
	switch existing := f.Args.(type) {
	case nil:
		f.Args = add
	case argList:
		f.Args = append(existing, add...)
	case joinedArgs:
		existing.rest = append(existing.rest, add...)
		f.Args = existing
	default:
		f.Args = joinedArgs{first: existing, rest: add}
	}
	return f
}

// Input adds an input label
func (f *Filter) Input(labels ...string) *Filter {
	f.InputLabels = append(f.InputLabels, labels...)
	return f
}

// Output adds an output label
func (f *Filter) Output(labels ...string) *Filter {
	f.OutputLabels = append(f.OutputLabels, labels...)
	return f
}

// A filter name and an @instance are both [a-zA-Z][a-zA-Z0-9_]*. An output
// label is a name ffmpeg matches by string; an input label is either that
// or a stream specifier, which may carry "?" (optional), "#"/"0x" (stream
// id) and metadata matches such as "0:m:language:eng".
var (
	namePattern      = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	labelPattern     = regexp.MustCompile(`^[a-zA-Z0-9_:]+$`)
	specifierPattern = regexp.MustCompile(`^[0-9][a-zA-Z0-9_:?#.\-]*$`)
)

// Validate checks if the filter is valid
func (f *Filter) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("filter name cannot be empty")
	}

	if !namePattern.MatchString(f.Name) {
		return fmt.Errorf("invalid filter name: %s", f.Name)
	}

	if f.Instance != "" && !namePattern.MatchString(f.Instance) {
		return fmt.Errorf("invalid instance name: %s", f.Instance)
	}

	for _, label := range f.InputLabels {
		if isStreamSpecifier(label) {
			if !specifierPattern.MatchString(label) {
				return fmt.Errorf("invalid input stream specifier: %s", label)
			}
			continue
		}
		if !labelPattern.MatchString(label) {
			return fmt.Errorf("invalid input label: %s", label)
		}
	}
	for _, label := range f.OutputLabels {
		if !labelPattern.MatchString(label) {
			return fmt.Errorf("invalid output label: %s", label)
		}
	}

	if f.Args != nil {
		return f.Args.Validate()
	}

	return nil
}

// String returns the string representation of the filter
func (f *Filter) String() string {
	var sb strings.Builder

	for _, label := range f.InputLabels {
		sb.WriteString("[" + label + "]")
	}

	sb.WriteString(f.Name)
	if f.Instance != "" {
		sb.WriteString("@" + f.Instance)
	}

	if f.Args != nil {
		argsStr := f.Args.String()
		if argsStr != "" {
			sb.WriteString("=")
			sb.WriteString(argsStr)
		}
	}

	for _, label := range f.OutputLabels {
		sb.WriteString("[" + label + "]")
	}

	return sb.String()
}

func (f *Filter) MarshalJSON() ([]byte, error) {
	type Alias Filter
	aux := &struct {
		Args any `json:"args,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(f),
	}

	if f.Args != nil {
		if list, ok := f.Args.(argList); ok {
			aux.Args = list
		} else {
			// A FilterArguments implementation of the caller's own: keep
			// its rendering, which is all that is knowable about it.
			aux.Args = f.Args.String()
		}
	}

	return json.Marshal(aux)
}

func (f *Filter) UnmarshalJSON(data []byte) error {
	type Alias Filter
	aux := &struct {
		Args json.RawMessage `json:"args,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(f),
	}

	// aux, not &aux: encoding/json sets a pointer it is given the address
	// of to nil for a JSON null, and every read below would then be a nil
	// dereference. Handed the pointer itself, a null is the no-op every
	// other struct gets, leaving the filter as it was.
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if len(aux.Args) > 0 {
		var args argList
		if err := json.Unmarshal(aux.Args, &args); err != nil {
			return fmt.Errorf("failed to unmarshal args: %v", err)
		}
		// A null or empty "args" is a filter without arguments rather
		// than one carrying an empty set of them.
		if args != nil {
			f.Args = args
		}
	}

	return nil
}
