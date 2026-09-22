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
// a bare argument after a named one is not something any release reads the
// way it is written. The recent ones refuse it. libavfilter's argument
// parser reads
//
//	if (parsed_key) {
//		key = parsed_key;
//		priv_class = NULL; /* reject all remaining shorthand */
//
// and ffmpeg reports "No option name near '...'".
//
// Up to 6.1 there was no such refusal, and what happened instead is worse:
// the walk over the filter's options simply carried on from where the
// named argument left it, so the bare arguments landed on whatever came
// next. pad=color=black:1280:720 sets height and x rather than width and
// height, and pads a 64x64 input to 64x1280 without a word. Occasionally
// the walk lands where you meant — scale=w=200:100 does give 200x100 —
// which makes the shape harder to catch, not safer to use.
//
// Stopping short is fine — scale=1280:h=-2 fills only w positionally — so
// the one rule that holds on every release is that every positional
// argument precedes every named one. Validate enforces it.
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

// Raw returns arguments that render exactly as given, for an argument
// string you already hold whole: out of a config file, a saved preset, a
// command line you are mirroring. The caller owns the escaping — nothing is
// added, removed or checked, so a stray ";" or "[" reaches ffmpeg and
// breaks the graph around it.
//
//	filtergraph.NewFilter("subtitles").WithArgs(filtergraph.Raw(preset))
//
// Use it for what the builders do not model; WithArg and WithPositionalArgs
// escape correctly and should be preferred where they fit.
func Raw(s string) FilterArguments { return argList{{Value: s, raw: true}} }

// joinedArgs is a [FilterArguments] implementation this package does not
// own with arguments appended after it. Rendering the first one and
// keeping the string would be enough for the rendering, but it would drop
// the implementation's Validate, and quietly not checking what a caller
// asked to have checked is worse than either alternative.
type joinedArgs struct {
	first FilterArguments
	rest  argList
}

var _ FilterArguments = joinedArgs{}

// String renders the first arguments and then the appended ones, joined by
// ":", with an empty rendering on either side contributing no empty slot.
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

// Validate reports the first arguments' own error before looking at the
// appended ones, which are checked as though the first were not there: what
// they render is as opaque here as a raw argument's string, so whether the
// last of them is named is unknowable.
func (j joinedArgs) Validate() error {
	if err := j.first.Validate(); err != nil {
		return err
	}
	return j.rest.Validate()
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

// Validate implements [FilterArguments]. It rejects the two things ffmpeg
// cannot be made to read: a positional argument after a named one, and a
// key holding a character escapeFilterArg would have to quote.
//
// Values are unrestricted — escapeFilterArg carries every character past
// both parsers — but a key is not a token ffmpeg lets you escape. Quoting
// one does not help: the graph parser eats the quotes, and the option
// parser then reads what is left with no escaping of its own, so
// scale='z\:z'=50 reaches it as z\:z=50 and it answers "No option name
// near". Escaping is worse than useless for "=", which stops the parser
// splitting off a name at all and turns the whole argument positional. A
// key is therefore deliverable only when it needs no quoting, which every
// real option name does, being [a-zA-Z0-9_]. Rejecting the rest here turns
// a silent mis-assignment into an error.
//
// Raw arguments are the caller's to get right: they pass unchecked, and an
// argument after one is checked as though the raw string were not there,
// since what it ends with is unknowable here.
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
// one-key object. Arguments that hold a raw string are written as the one
// rendered string, which round-trips the rendering exactly but comes back
// as a single raw argument rather than as the parts it was built from.
//
// A repeated key takes the array form because an object cannot hold it.
// Repeating a member name is legal JSON and this package's own decoder
// keeps both, but most decoders keep only the last, so writing
// {"k":"1","k":"2"} would let a round trip through anything else drop
// k=1 without a word. ffmpeg itself takes the last of a repeated option,
// so the arguments are worth keeping in order rather than collapsing.
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
		// A JSON null leaves the arguments alone, the no-op every other
		// type gets from encoding/json, which calls UnmarshalJSON for
		// null rather than skipping the member.
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
// empty member name is rejected rather than decoded: an argument with no
// key is a positional one here, so "" would quietly turn a named argument
// into a positional one and change what the filter reads.
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
// A filter description is unescaped twice. The graph parser goes first: it
// splits on "," ";" "[" "]", consumes backslashes outside quotes and strips
// a quoted section's quotes while passing its contents through untouched.
// The argument parser then splits what is left on ":" and on the "=" that
// introduces a value, and consumes the backslashes that survived. So
// quoting carries the graph separators through on its own, while ":", "="
// and "\\" have to be escaped inside the quotes as well, and a literal "'"
// cannot appear inside them at all — it is written by closing the quotes,
// escaping it for both parsers, and reopening.
//
// "=" matters only to a positional argument, which the argument parser
// reads as a name when it finds one: movie='/tmp/a=b.srt' looks like the
// option "/tmp/a" and fails, and only movie='/tmp/a\\=b.srt' reaches the
// filter whole. A named argument's value is already safe, since the parser
// gives up the name at the first "=" and takes the rest verbatim, but
// escaping there too costs nothing and keeps one rule instead of two.
//
// Whitespace needs the backslash too, although the quotes look like
// enough: they are gone by the time the argument parser reads the token,
// and av_get_token then trims the leading and trailing whitespace of
// whatever a backslash or a quote of its own has not claimed. So
// format=pix_fmts=' zz' reaches the filter as "zz" and only
// format=pix_fmts='\ zz' as " zz". Escaping every whitespace character and
// not just the ones at the edges keeps that distinction out of here;
// ffmpeg reads the two the same.
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

// specialChars are the characters that one parser or the other would act on,
// so a key or value holding any of them is quoted and escaped.
var specialChars = regexp.MustCompile(`[\[\]=;,:\s\\']`)

func needsQuoting(s string) bool { return specialChars.MatchString(s) }
