package filtergraph

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FilterArguments represents arguments passed to a filter
type FilterArguments interface {
	String() string
	Validate() error
}

// namedArg is one key=value filter argument.
type namedArg struct {
	Key, Value string
}

// namedArgs is an ordered list of key=value arguments, rendered in the
// order they were added so the output reads the way people write it
// (scale=w=1280:h=-2, not h=-2:w=1280).
type namedArgs []namedArg

var _ FilterArguments = namedArgs(nil)

// namedArgsFromMap sorts the keys so the result is deterministic.
func namedArgsFromMap(m map[string]string) namedArgs {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(namedArgs, 0, len(keys))
	for _, key := range keys {
		out = append(out, namedArg{key, m[key]})
	}
	return out
}

func (args namedArgs) String() string {
	if len(args) == 0 {
		return ""
	}
	argStrs := make([]string, 0, len(args))
	for _, a := range args {
		if a.Value == "" {
			argStrs = append(argStrs, escapeFilterArg(a.Key))
		} else {
			argStrs = append(argStrs, fmt.Sprintf("%s=%s", escapeFilterArg(a.Key), escapeFilterArg(a.Value)))
		}
	}
	return strings.Join(argStrs, ":")
}

func (args namedArgs) Validate() error {
	invalidChars := regexp.MustCompile(`[;\[\]]`)

	for _, a := range args {
		if invalidChars.MatchString(a.Key) {
			return fmt.Errorf("invalid characters in filter argument key: %s", a.Key)
		}
		if invalidChars.MatchString(a.Value) {
			return fmt.Errorf("invalid characters in filter argument value: %s", a.Value)
		}
	}
	return nil
}

// MarshalJSON writes a JSON object with the keys in argument order.
func (args namedArgs) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, a := range args {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(a.Key)
		v, _ := json.Marshal(a.Value)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}

// UnmarshalJSON reads a JSON object, keeping the keys in document order.
func (args *namedArgs) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return fmt.Errorf("filtergraph: named args must be a JSON object")
	}
	var out namedArgs
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := keyTok.(string)
		var value string
		if err := dec.Decode(&value); err != nil {
			return err
		}
		out = append(out, namedArg{key, value})
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*args = out
	return nil
}

// formatNumber prints a float the way people type it: 30, 23.976, 0.8.
func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// positionalArgs represents positional arguments for filters
type positionalArgs []string

var _ FilterArguments = make(positionalArgs, 0)

func (args positionalArgs) String() string {
	if len(args) == 0 {
		return ""
	}

	escaped := make([]string, len(args))
	for i, arg := range args {
		escaped[i] = escapeFilterArg(arg)
	}
	return strings.Join(escaped, ":")
}

func (args positionalArgs) Validate() error {
	invalidChars := regexp.MustCompile(`[;\[\]]`)

	for _, arg := range args {
		if invalidChars.MatchString(arg) {
			return fmt.Errorf("invalid characters in positional argument: %s", arg)
		}
	}
	return nil
}

func escapeFilterArg(arg string) string {
	if needsQuoting(arg) {
		return "'" + strings.ReplaceAll(arg, "'", "\\'") + "'"
	}
	return arg
}

func needsQuoting(arg string) bool {
	specialChars := regexp.MustCompile(`[\[\]=;,\s\\']`)
	return specialChars.MatchString(arg)
}

// mixedArgs represents a combination of named and positional arguments
type mixedArgs struct {
	namedArgs
	positionalArgs
}

// String implements [FilterArguments].
func (m mixedArgs) String() string {
	posStr := m.positionalArgs.String()
	namedStr := m.namedArgs.String()

	if namedStr == "" {
		return posStr
	}
	if posStr == "" {
		return namedStr
	}
	return posStr + ":" + namedStr
}

// Validate implements [FilterArguments].
func (m mixedArgs) Validate() error {
	if err := m.positionalArgs.Validate(); err != nil {
		return err
	}
	return m.namedArgs.Validate()
}

var _ FilterArguments = mixedArgs{}
