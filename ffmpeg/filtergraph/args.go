package filtergraph

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// FilterArguments represents arguments passed to a filter
type FilterArguments interface {
	String() string
	Validate() error
}

// namedArgs represents key-value arguments for filters
type namedArgs map[string]string

var _ FilterArguments = make(namedArgs)

func (args namedArgs) String() string {
	if len(args) == 0 {
		return ""
	}

	argStrs := make([]string, 0, len(args))
	for key, value := range args {
		if value == "" {
			argStrs = append(argStrs, escapeFilterArg(key))
		} else {
			argStrs = append(argStrs, fmt.Sprintf("%s=%s", escapeFilterArg(key), escapeFilterArg(value)))
		}
	}
	return strings.Join(argStrs, ":")
}

func (args namedArgs) Validate() error {
	invalidChars := regexp.MustCompile(`[;\[\]]`)

	for key, value := range args {
		if invalidChars.MatchString(key) {
			return fmt.Errorf("invalid characters in filter argument key: %s", key)
		}
		if invalidChars.MatchString(value) {
			return fmt.Errorf("invalid characters in filter argument value: %s", value)
		}
	}
	return nil
}

func (args namedArgs) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string(args))
}

func (args *namedArgs) UnmarshalJSON(data []byte) error {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*args = namedArgs(m)
	return nil
}

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
