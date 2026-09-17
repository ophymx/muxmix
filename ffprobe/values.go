package ffprobe

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// The scalar types in this file exist because ffprobe's JSON writer is only
// loosely typed: integers printed through print_int are JSON numbers, but
// byte sizes, file positions, bit rates, timestamps in seconds and rationals
// are JSON strings, and any value ffprobe cannot determine is either omitted
// or printed as the string "N/A" (with -show_optional_fields always).
//
// Each type accepts every spelling ffprobe has used, records whether a real
// value was present, and marshals back in ffprobe's own style so a Result
// round-trips.

const notAvailable = "N/A"

// Int is an integer field. It decodes from a JSON number or a numeric string
// and reports whether the value was present and not "N/A".
type Int struct {
	v  int64
	ok bool
}

// NewInt returns a valid Int.
func NewInt(v int64) Int { return Int{v: v, ok: true} }

// Valid reports whether the field was present with a numeric value.
func (i Int) Valid() bool { return i.ok }

// Int64 returns the value, or 0 when not Valid.
func (i Int) Int64() int64 { return i.v }

// Int returns the value as an int, or 0 when not Valid.
func (i Int) Int() int { return int(i.v) }

// Or returns the value, or def when not Valid.
func (i Int) Or(def int64) int64 {
	if i.ok {
		return i.v
	}
	return def
}

// OrInt returns the value as an int, or def when not Valid.
func (i Int) OrInt(def int) int {
	if i.ok {
		return int(i.v)
	}
	return def
}

// IsZero reports whether the field is absent; it lets encoding/json's
// omitzero drop it.
func (i Int) IsZero() bool { return !i.ok }

// String prints the value in decimal, or "N/A" when not Valid, matching
// ffprobe's own spelling.
func (i Int) String() string {
	if !i.ok {
		return notAvailable
	}
	return strconv.FormatInt(i.v, 10)
}

// MarshalJSON emits a JSON number, or null when not Valid.
func (i Int) MarshalJSON() ([]byte, error) {
	if !i.ok {
		return []byte("null"), nil
	}
	return strconv.AppendInt(nil, i.v, 10), nil
}

// UnmarshalJSON accepts a number, a numeric string, "N/A" or null.
func (i *Int) UnmarshalJSON(data []byte) error {
	s, isStr, err := scalarString(data)
	if err != nil {
		return err
	}
	if s == "" || s == notAvailable {
		*i = Int{}
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		// ffprobe never prints fractional integers, but be forgiving about
		// "12.0" style output from other tools that mimic it.
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil || f != float64(int64(f)) {
			if isStr {
				return fmt.Errorf("ffprobe: cannot parse %q as integer", s)
			}
			return fmt.Errorf("ffprobe: cannot parse %s as integer", s)
		}
		v = int64(f)
	}
	*i = Int{v: v, ok: true}
	return nil
}

// Seconds is a duration or timestamp that ffprobe prints as a decimal number
// of seconds (for example "0.040000"). It decodes from a JSON number or a
// numeric string.
type Seconds struct {
	v  float64
	ok bool
}

// NewSeconds returns a valid Seconds.
func NewSeconds(v float64) Seconds { return Seconds{v: v, ok: true} }

// SecondsOf converts a time.Duration into a valid Seconds.
func SecondsOf(d time.Duration) Seconds { return Seconds{v: d.Seconds(), ok: true} }

// Valid reports whether the field was present with a numeric value.
func (s Seconds) Valid() bool { return s.ok }

// Float64 returns the value in seconds, or 0 when not Valid.
func (s Seconds) Float64() float64 { return s.v }

// Duration returns the value as a time.Duration, or 0 when not Valid.
func (s Seconds) Duration() time.Duration { return time.Duration(s.v * float64(time.Second)) }

// Or returns the value, or def when not Valid.
func (s Seconds) Or(def time.Duration) time.Duration {
	if s.ok {
		return s.Duration()
	}
	return def
}

// IsZero reports whether the field is absent.
func (s Seconds) IsZero() bool { return !s.ok }

// String prints the value with six decimals ("0.040000") the way ffprobe
// does, or "N/A" when not Valid.
func (s Seconds) String() string {
	if !s.ok {
		return notAvailable
	}
	return strconv.FormatFloat(s.v, 'f', 6, 64)
}

// MarshalJSON emits the value as a string with six decimals, ffprobe style,
// or null when not Valid.
func (s Seconds) MarshalJSON() ([]byte, error) {
	if !s.ok {
		return []byte("null"), nil
	}
	return strconv.AppendQuote(nil, s.String()), nil
}

// UnmarshalJSON accepts a number, a numeric string, "N/A" or null.
func (s *Seconds) UnmarshalJSON(data []byte) error {
	str, _, err := scalarString(data)
	if err != nil {
		return err
	}
	if str == "" || str == notAvailable {
		*s = Seconds{}
		return nil
	}
	v, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return fmt.Errorf("ffprobe: cannot parse %q as seconds", str)
	}
	*s = Seconds{v: v, ok: true}
	return nil
}

// Bool is a flag that ffprobe prints as the integers 0 and 1. It also accepts
// JSON booleans and the strings "0", "1", "true" and "false".
type Bool struct {
	v  bool
	ok bool
}

// NewBool returns a valid Bool.
func NewBool(v bool) Bool { return Bool{v: v, ok: true} }

// Valid reports whether the field was present.
func (b Bool) Valid() bool { return b.ok }

// Bool returns the value, or false when not Valid.
func (b Bool) Bool() bool { return b.v }

// IsZero reports whether the field is absent.
func (b Bool) IsZero() bool { return !b.ok }

// String prints "1" or "0" the way ffprobe does, or "N/A" when not Valid.
func (b Bool) String() string {
	if !b.ok {
		return notAvailable
	}
	if b.v {
		return "1"
	}
	return "0"
}

// MarshalJSON emits 0 or 1, ffprobe style, or null when not Valid.
func (b Bool) MarshalJSON() ([]byte, error) {
	if !b.ok {
		return []byte("null"), nil
	}
	return []byte(b.String()), nil
}

// UnmarshalJSON accepts 0/1, true/false, their string forms, "N/A" or null.
func (b *Bool) UnmarshalJSON(data []byte) error {
	s, _, err := scalarString(data)
	if err != nil {
		return err
	}
	switch strings.ToLower(s) {
	case "", notAvailable:
		*b = Bool{}
	case "0", "false":
		*b = Bool{v: false, ok: true}
	case "1", "true":
		*b = Bool{v: true, ok: true}
	default:
		return fmt.Errorf("ffprobe: cannot parse %q as boolean", s)
	}
	return nil
}

// Rat is a rational number as ffprobe prints it: "30000/1001" for frame
// rates and time bases, "16:9" for aspect ratios, and "0/0" when unknown.
// The zero value and an unknown "0/0" are both reported as not Valid.
type Rat struct {
	num, den int64
	sep      byte // '/' or ':' as printed; '/' when constructed
	ok       bool
}

// NewRat returns a Rat with the given numerator and denominator. A zero
// denominator yields a Rat that is not Valid.
func NewRat(num, den int64) Rat {
	return Rat{num: num, den: den, sep: '/', ok: den != 0}
}

// ParseRat parses "num/den" or "num:den". A bare integer is accepted as
// num/1. It returns an error for anything else; "N/A" yields an invalid Rat
// with a nil error.
func ParseRat(s string) (Rat, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == notAvailable {
		return Rat{}, nil
	}
	sep := byte('/')
	idx := strings.IndexAny(s, "/:")
	var numStr, denStr string
	if idx < 0 {
		numStr, denStr = s, "1"
	} else {
		sep = s[idx]
		numStr, denStr = s[:idx], s[idx+1:]
	}
	num, err := strconv.ParseInt(strings.TrimSpace(numStr), 10, 64)
	if err != nil {
		return Rat{}, fmt.Errorf("ffprobe: cannot parse %q as rational", s)
	}
	den, err := strconv.ParseInt(strings.TrimSpace(denStr), 10, 64)
	if err != nil {
		return Rat{}, fmt.Errorf("ffprobe: cannot parse %q as rational", s)
	}
	return Rat{num: num, den: den, sep: sep, ok: den != 0}, nil
}

// Valid reports whether the field was present with a non-zero denominator.
func (r Rat) Valid() bool { return r.ok }

// Num returns the numerator (0 when not Valid).
func (r Rat) Num() int64 { return r.num }

// Den returns the denominator (0 when not Valid).
func (r Rat) Den() int64 { return r.den }

// Float64 returns num/den, or 0 when not Valid.
func (r Rat) Float64() float64 {
	if !r.ok {
		return 0
	}
	return float64(r.num) / float64(r.den)
}

// Rat returns an exact *big.Rat, or nil when not Valid. Use it for
// timestamp arithmetic where float rounding would be wrong.
func (r Rat) Rat() *big.Rat {
	if !r.ok {
		return nil
	}
	return big.NewRat(r.num, r.den)
}

// Inverse returns den/num, or an invalid Rat when the numerator is zero.
func (r Rat) Inverse() Rat {
	if !r.ok || r.num == 0 {
		return Rat{}
	}
	return Rat{num: r.den, den: r.num, sep: r.sep, ok: true}
}

// Duration interprets an integer count of ticks in this time base and returns
// the corresponding time.Duration. It is the usual way to turn a pts or a
// duration_ts into wall time: stream.TimeBase.Duration(stream.DurationTS).
func (r Rat) Duration(ticks Int) time.Duration {
	if !r.ok || !ticks.ok {
		return 0
	}
	ns := new(big.Rat).SetInt64(ticks.v)
	ns.Mul(ns, big.NewRat(r.num, r.den))
	ns.Mul(ns, big.NewRat(int64(time.Second), 1))
	q := new(big.Int).Quo(ns.Num(), ns.Denom())
	return time.Duration(q.Int64())
}

// IsZero reports whether the field is absent or unknown.
func (r Rat) IsZero() bool { return !r.ok && r.sep == 0 }

// String prints the rational with the separator it was parsed with, or "0/0"
// for an unknown value, matching ffprobe's own output.
func (r Rat) String() string {
	sep := r.sep
	if sep == 0 {
		sep = '/'
	}
	if !r.ok {
		return "0" + string(sep) + "0"
	}
	return strconv.FormatInt(r.num, 10) + string(sep) + strconv.FormatInt(r.den, 10)
}

// MarshalJSON emits the rational as a string, ffprobe style. An absent value
// marshals as null; an unknown but present value marshals as "0/0".
func (r Rat) MarshalJSON() ([]byte, error) {
	if r.IsZero() {
		return []byte("null"), nil
	}
	return strconv.AppendQuote(nil, r.String()), nil
}

// UnmarshalJSON accepts "num/den", "num:den", a bare integer, "N/A" or null.
func (r *Rat) UnmarshalJSON(data []byte) error {
	s, _, err := scalarString(data)
	if err != nil {
		return err
	}
	parsed, err := ParseRat(s)
	if err != nil {
		return err
	}
	if !parsed.ok && s != "" && s != notAvailable && parsed.sep == 0 {
		parsed.sep = '/'
	}
	*r = parsed
	return nil
}

// Tags is the key/value metadata dictionary ffprobe attaches to formats,
// streams, chapters, programs, packets and frames.
type Tags map[string]string

// Get returns the value for key, matching case-insensitively the way ffmpeg's
// own metadata lookup does (Matroska writes "LANGUAGE", MP4 writes "language").
func (t Tags) Get(key string) (string, bool) {
	if v, ok := t[key]; ok {
		return v, true
	}
	for k, v := range t {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return "", false
}

// Value returns the value for key or "" when absent.
func (t Tags) Value(key string) string {
	v, _ := t.Get(key)
	return v
}

// scalarString decodes a JSON scalar (string, number, bool or null) into its
// textual form. isStr reports whether the input was a JSON string.
func scalarString(data []byte) (s string, isStr bool, err error) {
	data = []byte(strings.TrimSpace(string(data)))
	if len(data) == 0 || string(data) == "null" {
		return "", false, nil
	}
	if data[0] == '"' {
		if err := json.Unmarshal(data, &s); err != nil {
			return "", true, err
		}
		return strings.TrimSpace(s), true, nil
	}
	if data[0] == '{' || data[0] == '[' {
		return "", false, fmt.Errorf("ffprobe: expected scalar, got %s", data)
	}
	return string(data), false, nil
}
