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

// arg is one filter argument: key=value with a Key, the value alone
// without one, and the Value exactly as given when raw. An empty Value
// still renders the "key=", which sets the option to the empty string.
type arg struct {
	Key, Value string
	raw        bool
}

// argList is a filter's arguments in the order they were added, so the
// rendering reads the way people write it: scale=w=1280:h=-2 rather than
// h=-2:w=1280. Positional, named and raw arguments share the one list,
// which is why the builders that add them compose in call order instead of
// replacing each other's work. Validate holds the order to ffmpeg's rule.
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

func positionalArgs(values []string) argList {
	out := make(argList, 0, len(values))
	for _, v := range values {
		out = append(out, arg{Value: v})
	}
	return out
}

// Raw returns arguments that render exactly as given, for an argument
// string you already hold whole: out of a config file, a saved preset, a
// command line you are mirroring. The caller owns the escaping — nothing is
// added, removed or checked, so a stray ";" reaches ffmpeg and breaks the
// graph around it.
//
//	filtergraph.NewFilter("subtitles").WithArgs(filtergraph.Raw(preset))
//
// Prefer WithArg and WithPositionalArgs, which escape correctly, for
// anything they cover.
func Raw(s string) FilterArguments { return argList{{Value: s, raw: true}} }

// joinedArgs is a [FilterArguments] implementation this package does not
// own with arguments appended after it. Keeping the implementation rather
// than its rendered string keeps its Validate.
type joinedArgs struct {
	first FilterArguments
	rest  argList
}

var _ FilterArguments = joinedArgs{}

// String joins the two renderings with ":", an empty one on either side
// contributing no empty slot.
func (j joinedArgs) String() string {
	first, rest := j.first.String(), j.rest.String()
	switch {
	case first == "":
		return rest
	case rest == "":
		return first
	default:
		return first + ":" + rest
	}
}

// Validate reports the first arguments' own error first. The appended ones
// are checked as though the first were not there: what they render is as
// opaque here as a raw string, so whether the last is named is unknowable.
func (j joinedArgs) Validate() error {
	if err := j.first.Validate(); err != nil {
		return err
	}
	return j.rest.Validate()
}

// String escapes key and value separately, so the "=" introducing a value
// is not itself escaped.
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

// String joins the arguments with ":". An empty raw argument contributes
// nothing rather than an empty slot.
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

// Validate implements [FilterArguments], rejecting the two shapes ffmpeg
// cannot be made to read: a positional argument after a named one, which
// 7.1 and later refuse outright and the releases before misread onto
// whatever option came next, and a key holding a character
// escapeFilterArg would have to quote.
//
// A value can hold anything; a key cannot. Quoting does not survive to the
// option parser, which has no escaping of its own — scale='z\:z'=50
// reaches it as z\:z=50 — and escaping the "=" turns the whole argument
// positional. Real option names are [a-zA-Z0-9_], so nothing is lost.
//
// Raw arguments pass unchecked, and an argument after one is checked as
// though the raw string were not there.
func (a argList) Validate() error {
	var named string
	for _, one := range a {
		if one.raw {
			continue
		}
		if one.Key != "" {
			if needsQuoting(one.Key) {
				return fmt.Errorf("argument key %q: ffmpeg reads an option name with no escaping of its own, so a key cannot hold any of %s or whitespace", one.Key, `[]=;,:'\`)
			}
			named = one.Key
			continue
		}
		if named != "" {
			return fmt.Errorf("positional argument %q after named argument %q: ffmpeg takes a filter's positional arguments only before the first key=value", one.Value, named)
		}
	}
	return nil
}

// MarshalJSON writes an object while every argument is named and no key
// repeats, an array of strings while every argument is positional, and
// otherwise an array in argument order with each named argument as a
// one-key object. Raw arguments are written as the one rendered string,
// which round-trips the rendering but comes back as a single raw argument
// rather than as the parts it was built from.
//
// A repeated key takes the array form because most decoders keep only the
// last member of a repeated name, and ffmpeg takes the last of a repeated
// option, so collapsing them would change what the filter reads.
func (a argList) MarshalJSON() ([]byte, error) {
	positional, repeated := 0, false
	seen := make(map[string]bool, len(a))
	for _, one := range a {
		if one.raw {
			return json.Marshal(a.String())
		}
		if one.Key == "" {
			positional++
			continue
		}
		if seen[one.Key] {
			repeated = true
		}
		seen[one.Key] = true
	}
	if positional == 0 && !repeated {
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
// argument. A null leaves the arguments as they were, so a document that
// spells out an absent "args" still decodes.
func (a *argList) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	var out argList
	switch tok {
	case nil:
		// encoding/json calls UnmarshalJSON for null rather than skipping
		// the member, so the no-op has to be written out.
		return nil
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

// decodeObject reads the members of an object already opened on dec. An
// empty name is rejected: with no key the argument would turn positional
// and change what the filter reads.
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
		if key == "" {
			return fmt.Errorf("filtergraph: filter argument name is empty: write a positional argument as a string in an array, not as a \"\" member")
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

// escapeFilterArg renders a key or value so that ffmpeg delivers it to the
// filter unchanged.
//
// A filter description is unescaped twice. The graph parser splits on ","
// ";" "[" "]", consumes backslashes outside quotes, and strips a quoted
// section's quotes while passing its contents through untouched. The
// argument parser then splits what is left on ":" and on the "=" that
// introduces a value, and consumes the backslashes that survived. So the
// quotes carry the graph separators on their own, ":", "=" and "\\" need a
// backslash inside them as well, and a literal "'" cannot sit inside them
// at all: it is written by closing the quotes, escaping it for both
// parsers, and reopening.
//
// The two that surprise: "=" makes the argument parser read a positional
// value as an option name, so only movie='/tmp/a\\=b.srt' opens the file;
// and whitespace outlives the quotes, which are gone before av_get_token
// trims the token, so pix_fmts=' zz' arrives as "zz" and only '\ zz' as
// " zz". Every whitespace character is escaped, not just the ones at the
// edges, which keeps the distinction out of here.
func escapeFilterArg(s string) string {
	if !needsQuoting(s) {
		return s
	}
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString(`'\\\''`)
		case '\\':
			b.WriteString(`\\`)
		case ':', '=', ' ', '\t', '\n', '\f', '\r':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// specialChars are the characters one parser or the other acts on.
var specialChars = regexp.MustCompile(`[\[\]=;,:\s\\']`)

func needsQuoting(s string) bool { return specialChars.MatchString(s) }
