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

// arg is one filter argument. With a Key it renders as key=value, and with
// an empty Value as key= , which sets the option to the empty string.
// Without a Key it is positional: the value alone, in place. A raw arg
// renders its Value exactly as given, escaping and all.
//
// ffmpeg has no bare-flag form here — its parser reads every bare token as
// the value of the filter's next unfilled option — so only a positional
// argument renders bare.
type arg struct {
	Key, Value string
	raw        bool
}

// argList is a filter's arguments in the order they were added, so the
// rendering reads the way people write it: scale=w=1280:h=-2 rather than
// h=-2:w=1280, and pad=1280:720:-1:-1:color=black rather than one half of
// it. Positional, named and raw arguments share the one list, which is why
// the builders that add them compose in call order instead of replacing
// each other's work.
//
// ffmpeg constrains that order the way Python constrains keyword arguments:
// positional arguments fill the filter's options in declaration order, and
// the first key=value gives up the rest of them, so a bare argument after a
// named one has nothing left to fill and is a parse error rather than a
// mis-assignment. libavfilter's argument parser reads
//
//	if (parsed_key) {
//		key = parsed_key;
//		priv_class = NULL; /* reject all remaining shorthand */
//
// and ffmpeg reports "No option name near '...'". Stopping short is fine —
// scale=1280:h=-2 fills only w positionally — so the one rule is that every
// positional argument precedes every named one. Validate enforces it.
type argList []arg

var _ FilterArguments = argList(nil)

// argsFromMap sorts the keys so the result is deterministic.
func argsFromMap(m map[string]string) argList {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(argList, 0, len(keys))
	for _, key := range keys {
		out = append(out, arg{Key: key, Value: m[key]})
	}
	return out
}

// positionalArgs makes bare values into positional arguments.
func positionalArgs(values []string) argList {
	out := make(argList, 0, len(values))
	for _, v := range values {
		out = append(out, arg{Value: v})
	}
	return out
}

// String renders one argument, escaping key and value separately so that a
// value containing "=" or "," is quoted while the "key=" introducing it is
// not.
func (a arg) String() string {
	switch {
	case a.raw:
		return a.Value
	case a.Key == "":
		return escapeFilterArg(a.Value)
	default:
		return escapeFilterArg(a.Key) + "=" + escapeFilterArg(a.Value)
	}
}

// String renders the arguments joined by ":". An empty raw argument
// contributes nothing rather than an empty slot.
func (a argList) String() string {
	parts := make([]string, 0, len(a))
	for _, one := range a {
		if one.raw && one.Value == "" {
			continue
		}
		parts = append(parts, one.String())
	}
	return strings.Join(parts, ":")
}

// invalidArgChars are the characters that would end the filter and put what
// follows into the surrounding graph.
var invalidArgChars = regexp.MustCompile(`[;\[\]]`)

// Validate implements [FilterArguments]. It rejects characters that would
// break out of the filter, and a positional argument after a named one,
// which ffmpeg cannot parse. Arguments holding a raw string are the
// caller's to get right: they pass unchecked, and an argument after one is
// checked as though the raw string were not there, since what it ends with
// is unknowable here.
func (a argList) Validate() error {
	var named string
	for _, one := range a {
		if one.raw {
			continue
		}
		if invalidArgChars.MatchString(one.Key) {
			return fmt.Errorf("invalid characters in filter argument key: %s", one.Key)
		}
		if invalidArgChars.MatchString(one.Value) {
			if one.Key == "" {
				return fmt.Errorf("invalid characters in positional argument: %s", one.Value)
			}
			return fmt.Errorf("invalid characters in filter argument value: %s", one.Value)
		}
		if one.Key != "" {
			named = one.Key
			continue
		}
		if named != "" {
			return fmt.Errorf("positional argument %q after named argument %q: ffmpeg takes a filter's positional arguments only before the first key=value", one.Value, named)
		}
	}
	return nil
}

// MarshalJSON writes an object while every argument is named, an array of
// strings while every argument is positional, and otherwise an array in
// argument order with each named argument as a one-key object. Arguments
// that hold a raw string are written as the one rendered string, which
// round-trips the rendering exactly but comes back as a single raw
// argument rather than as the parts it was built from.
func (a argList) MarshalJSON() ([]byte, error) {
	positional := 0
	for _, one := range a {
		if one.raw {
			return json.Marshal(a.String())
		}
		if one.Key == "" {
			positional++
		}
	}
	if positional == 0 {
		return a.marshalObject(), nil
	}
	if positional == len(a) {
		values := make([]string, len(a))
		for i, one := range a {
			values[i] = one.Value
		}
		return json.Marshal(values)
	}
	var b bytes.Buffer
	b.WriteByte('[')
	for i, one := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		if one.Key == "" {
			v, _ := json.Marshal(one.Value)
			b.Write(v)
			continue
		}
		b.Write(argList{one}.marshalObject())
	}
	b.WriteByte(']')
	return b.Bytes(), nil
}

// marshalObject writes the arguments as a JSON object, keys in argument
// order. Marshalling a string cannot fail.
func (a argList) marshalObject() []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, one := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(one.Key)
		v, _ := json.Marshal(one.Value)
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// UnmarshalJSON reads any shape MarshalJSON writes, keeping the arguments
// in document order: an object of named arguments, an array whose strings
// are positional and whose objects are named, or one string taken as a raw
// argument.
func (a *argList) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	var out argList
	switch tok {
	case json.Delim('{'):
		if err := out.decodeObject(dec); err != nil {
			return err
		}
	case json.Delim('['):
		if err := out.decodeArray(dec); err != nil {
			return err
		}
	default:
		s, ok := tok.(string)
		if !ok {
			return fmt.Errorf("filtergraph: filter arguments must be a JSON object, array or string")
		}
		*a = argList{{Value: s, raw: true}}
		return nil
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*a = out
	return nil
}

// decodeObject reads the members of an object already opened on dec.
func (a *argList) decodeObject(dec *json.Decoder) error {
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
		*a = append(*a, arg{Key: key, Value: value})
	}
	return nil
}

// decodeArray reads the elements of an array already opened on dec: a
// string is positional, an object contributes its named arguments in place.
func (a *argList) decodeArray(dec *json.Decoder) error {
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '{' {
			var named argList
			if err := json.Unmarshal(trimmed, &named); err != nil {
				return err
			}
			*a = append(*a, named...)
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*a = append(*a, arg{Value: value})
	}
	return nil
}

// formatNumber prints a float the way people type it: 30, 23.976, 0.8.
func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// escapeFilterArg quotes a key or value that would otherwise be read as
// filter syntax.
func escapeFilterArg(s string) string {
	if needsQuoting(s) {
		return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
	}
	return s
}

var specialChars = regexp.MustCompile(`[\[\]=;,\s\\']`)

func needsQuoting(s string) bool { return specialChars.MatchString(s) }
