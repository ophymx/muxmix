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

func (f *Filter) WithArgs(args FilterArguments) *Filter {
	f.Args = args
	return f
}

// WithNamedArgs sets key-value arguments for the filter
func (f *Filter) WithNamedArgs(args map[string]string) *Filter {
	f.Args = namedArgs(args)
	return f
}

// WithPositionalArgs sets positional arguments for the filter
func (f *Filter) WithPositionalArgs(args ...string) *Filter {
	f.Args = positionalArgs(args)
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

// Validate checks if the filter is valid
func (f *Filter) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("filter name cannot be empty")
	}

	namePattern := regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	if !namePattern.MatchString(f.Name) {
		return fmt.Errorf("invalid filter name: %s", f.Name)
	}

	if f.Instance != "" && !namePattern.MatchString(f.Instance) {
		return fmt.Errorf("invalid instance name: %s", f.Instance)
	}

	labelPattern := regexp.MustCompile(`^[a-zA-Z0-9_:]+$`)
	for _, label := range f.InputLabels {
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
		sb.WriteString(fmt.Sprintf("[%s]", label))
	}

	sb.WriteString(f.Name)
	if f.Instance != "" {
		sb.WriteString(fmt.Sprintf("@%s", f.Instance))
	}

	if f.Args != nil {
		argsStr := f.Args.String()
		if argsStr != "" {
			sb.WriteString("=")
			sb.WriteString(argsStr)
		}
	}

	for _, label := range f.OutputLabels {
		sb.WriteString(fmt.Sprintf("[%s]", label))
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
		if kv, ok := f.Args.(namedArgs); ok {
			aux.Args = map[string]string(kv)
		} else if pos, ok := f.Args.(positionalArgs); ok {
			aux.Args = []string(pos)
		} else {
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

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if len(aux.Args) > 0 {
		var m map[string]string
		if err := json.Unmarshal(aux.Args, &m); err == nil {
			f.Args = namedArgs(m)
		} else {
			var s []string
			if err := json.Unmarshal(aux.Args, &s); err == nil {
				f.Args = positionalArgs(s)
			} else {
				var str string
				if err := json.Unmarshal(aux.Args, &str); err != nil {
					return fmt.Errorf("failed to unmarshal args: %v", err)
				}
				f.Args = namedArgs{"value": str}
			}
		}
	}

	return nil
}
