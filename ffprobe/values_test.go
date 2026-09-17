package ffprobe

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIntUnmarshal(t *testing.T) {
	cases := []struct {
		in    string
		want  int64
		valid bool
		err   bool
	}{
		{`42`, 42, true, false},
		{`"42"`, 42, true, false},
		{`"-7"`, -7, true, false},
		{`"N/A"`, 0, false, false},
		{`null`, 0, false, false},
		{`""`, 0, false, false},
		{`"12.0"`, 12, true, false},
		{`"abc"`, 0, false, true},
		{`"1.5"`, 0, false, true},
		{`{}`, 0, false, true},
	}
	for _, c := range cases {
		var v Int
		err := json.Unmarshal([]byte(c.in), &v)
		if (err != nil) != c.err {
			t.Errorf("%s: err=%v want err=%v", c.in, err, c.err)
			continue
		}
		if v.Valid() != c.valid || v.Int64() != c.want {
			t.Errorf("%s: got (%d,%v) want (%d,%v)", c.in, v.Int64(), v.Valid(), c.want, c.valid)
		}
	}
	if NewInt(3).Or(9) != 3 || (Int{}).Or(9) != 9 {
		t.Error("Or")
	}
}

func TestSecondsUnmarshal(t *testing.T) {
	var s Seconds
	if err := json.Unmarshal([]byte(`"0.040000"`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Duration() != 40*time.Millisecond {
		t.Errorf("got %v", s.Duration())
	}
	if err := json.Unmarshal([]byte(`1.5`), &s); err != nil || s.Float64() != 1.5 {
		t.Errorf("number: %v %v", s, err)
	}
	if err := json.Unmarshal([]byte(`"N/A"`), &s); err != nil || s.Valid() {
		t.Errorf("N/A: %v %v", s, err)
	}
	b, _ := json.Marshal(NewSeconds(2))
	if string(b) != `"2.000000"` {
		t.Errorf("marshal: %s", b)
	}
}

func TestBoolUnmarshal(t *testing.T) {
	for in, want := range map[string]bool{`0`: false, `1`: true, `true`: true, `"0"`: false, `"1"`: true, `"false"`: false} {
		var b Bool
		if err := json.Unmarshal([]byte(in), &b); err != nil || !b.Valid() || b.Bool() != want {
			t.Errorf("%s: %v %v", in, b, err)
		}
	}
	var b Bool
	if err := json.Unmarshal([]byte(`"maybe"`), &b); err == nil {
		t.Error("expected error")
	}
	out, _ := json.Marshal(NewBool(true))
	if string(out) != "1" {
		t.Errorf("marshal: %s", out)
	}
}

func TestRat(t *testing.T) {
	cases := []struct {
		in    string
		num   int64
		den   int64
		valid bool
		str   string
	}{
		{`"30000/1001"`, 30000, 1001, true, "30000/1001"},
		{`"16:9"`, 16, 9, true, "16:9"},
		{`"0/0"`, 0, 0, false, "0/0"},
		{`"0:1"`, 0, 1, true, "0:1"},
		{`"25"`, 25, 1, true, "25/1"},
		{`25`, 25, 1, true, "25/1"},
		{`"N/A"`, 0, 0, false, "0/0"},
		{`null`, 0, 0, false, "0/0"},
	}
	for _, c := range cases {
		var r Rat
		if err := json.Unmarshal([]byte(c.in), &r); err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if r.Valid() != c.valid || r.Num() != c.num || r.Den() != c.den || r.String() != c.str {
			t.Errorf("%s: got %d/%d valid=%v str=%q", c.in, r.Num(), r.Den(), r.Valid(), r.String())
		}
	}
	var r Rat
	if err := json.Unmarshal([]byte(`"a/b"`), &r); err == nil {
		t.Error("expected error")
	}

	tb := NewRat(1, 90000)
	if got := tb.Duration(NewInt(90000 * 3)); got != 3*time.Second {
		t.Errorf("Duration = %v", got)
	}
	if tb.Duration(Int{}) != 0 || (Rat{}).Duration(NewInt(1)) != 0 {
		t.Error("invalid inputs should give 0")
	}
	fr := NewRat(30000, 1001)
	if fr.Rat().String() != "30000/1001" || fr.Inverse().String() != "1001/30000" {
		t.Errorf("Rat/Inverse: %v %v", fr.Rat(), fr.Inverse())
	}
	if f := fr.Float64(); f < 29.97 || f > 29.971 {
		t.Errorf("Float64 = %v", f)
	}

	// Round trip preserves the separator ffprobe used.
	for _, s := range []string{`"16:9"`, `"1/1000"`, `"0/0"`} {
		var r Rat
		_ = json.Unmarshal([]byte(s), &r)
		out, _ := json.Marshal(r)
		if string(out) != s {
			t.Errorf("round trip %s -> %s", s, out)
		}
	}
}

func TestOmitZero(t *testing.T) {
	type doc struct {
		A Int     `json:"a,omitzero"`
		B Seconds `json:"b,omitzero"`
		C Rat     `json:"c,omitzero"`
		D Bool    `json:"d,omitzero"`
	}
	out, err := json.Marshal(doc{})
	if err != nil || string(out) != "{}" {
		t.Errorf("got %s %v", out, err)
	}
	out, _ = json.Marshal(doc{A: NewInt(1), B: NewSeconds(1), C: NewRat(1, 2), D: NewBool(false)})
	if string(out) != `{"a":1,"b":"1.000000","c":"1/2","d":0}` {
		t.Errorf("got %s", out)
	}
}

func TestTagsGet(t *testing.T) {
	tags := Tags{"LANGUAGE": "eng", "title": "x"}
	if v, ok := tags.Get("language"); !ok || v != "eng" {
		t.Errorf("Get language = %q %v", v, ok)
	}
	if tags.Value("missing") != "" {
		t.Error("missing")
	}
}
