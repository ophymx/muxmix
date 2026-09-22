package filtergraph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/filtergraph"
)

// ─── live tests against a real ffmpeg ──────────────────────────────────────

// TestLiveFilterArgumentOrder holds Validate to what ffmpeg's own parser
// does, because the rule it encodes is not one this package can decide.
// libavfilter fills a filter's options positionally in declaration order and
// drops the rest of them at the first key=value ("reject all remaining
// shorthand"), so a bare argument after a named one has nothing left to fill
// and the parse fails with "No option name near '...'". Every ffmpeg from
// 4.4 to 8.x behaves this way; matrix/test.sh runs this against each.
func TestLiveFilterArgumentOrder(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}

	for _, tc := range []struct {
		name   string
		filter *filtergraph.Filter
		want   bool // whether ffmpeg should parse it
	}{
		{"all positional", filtergraph.NewFilter("pad").WithPositionalArgs("1280", "720", "-1", "-1", "black"), true},
		{"all named", filtergraph.NewFilter("scale").WithArg("w", "1280").WithArg("h", "-2"), true},
		{"positional then named", filtergraph.NewFilter("pad").WithPositionalArgs("1280", "720", "-1", "-1").WithArg("color", "black"), true},
		// Filling only some of the positional slots before switching to
		// names is allowed: here w, then h by name.
		{"some positional then named", filtergraph.NewFilter("scale").WithPositionalArgs("1280").WithArg("h", "-2"), true},
		{"named then positional", filtergraph.NewFilter("pad").WithArg("color", "black").WithPositionalArgs("1280", "720"), false},
		{"positional after a named", filtergraph.NewFilter("pad").WithPositionalArgs("1280", "720").WithArg("color", "black").WithPositionalArgs("-1"), false},
		// An empty value keeps its key, so it stays a named argument and
		// the ordering rule is untouched by it. Rendered bare it would be
		// a positional argument, and this would not parse.
		{"empty value is a named argument", filtergraph.NewFilter("scale").WithArg("w", "1280").WithArg("h", "-2").WithArg("flags", ""), true},
		{"positional after an empty-valued named argument", filtergraph.NewFilter("pad").WithArg("color", "").WithPositionalArgs("1280", "720"), false},
		{"raw args carry their own order", filtergraph.NewFilter("pad").WithRawArgs("1280:720:-1:-1:color=black"), true},
		{"a value needing quotes survives the round trip", filtergraph.NewFilter("drawbox").WithArg("enable", "between(t,0,5)").WithArg("color", "red"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", tc.filter), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))

			_, runErr := ffmpeg.Run(context.Background(), cmd)
			validateErr := tc.filter.Validate()

			if (runErr == nil) != tc.want {
				t.Errorf("ffmpeg parsed %q: %v, want ok = %v", tc.filter, runErr, tc.want)
			}
			if (validateErr == nil) != (runErr == nil) {
				t.Errorf("Validate() = %v but ffmpeg = %v, for %q", validateErr, runErr, tc.filter)
			}
		})
	}
}

// TestLiveFilterArgumentEscaping checks that a value arrives at the filter
// byte for byte. The format filter is the readback: every value below is an
// invalid pixel format, so ffmpeg names the one it received, and finding the
// value in its log means both parsers handed it over intact. A value split
// on ":", stripped of a backslash or trimmed of its whitespace fails the
// search instead.
func TestLiveFilterArgumentEscaping(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}

	for _, value := range []string{
		"plainvalue",
		"a,b",        // a graph separator: quoting carries it
		"a:b",        // the argument separator: needs escaping inside the quotes
		`a\b`,        // a backslash the argument parser would otherwise eat
		"a'b",        // cannot sit inside the quotes at all
		"a b",        // whitespace
		" zz",        // leading whitespace, which the argument parser trims unescaped
		"zz ",        // and trailing, the same
		"a\tb",       // a tab is whitespace to ffmpeg too
		"a=b",        // the key/value separator
		"a;b",        // a chain separator
		"a[b]c",      // label brackets
		`it's a:b\c`, // all of them at once
	} {
		t.Run(value, func(t *testing.T) {
			f := filtergraph.NewFilter("format").WithArg("pix_fmts", value)
			if err := f.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", f), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))

			res, err := ffmpeg.Run(context.Background(), cmd)
			if err == nil {
				t.Fatalf("%q was accepted as a pixel format", value)
			}
			if res == nil {
				t.Fatalf("ffmpeg did not start: %v", err)
			}
			if !strings.Contains(string(res.Stderr), value) {
				t.Errorf("%s rendered as %s, and ffmpeg reported:\n%s\nwant the value %q to reach the filter whole",
					f.Name, f, strings.TrimSpace(string(res.Stderr)), value)
			}
		})
	}
}

// TestLiveFilterPositionalEscaping is the "=" half of the escaping, which
// the named readback above cannot see. ffmpeg's argument parser reads a
// bare argument holding an unescaped "=" as an option name — movie=/tmp/a=b
// looks like the option "/tmp/a" — so a positional value has to escape it
// to stay positional. The format filter is the readback again: its one
// option takes the bare argument, and an invalid pixel format is named back
// in the log.
func TestLiveFilterPositionalEscaping(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}

	for _, value := range []string{
		"/tmp/a=b.srt", // the case this escaping exists for
		"a=b",
		"a=b:c=d",           // both separators at once
		"FontName=Arial",    // the shape a force_style value takes
		`a=b,c;d[e]f\g'h i`, // and every special character together
	} {
		t.Run(value, func(t *testing.T) {
			f := filtergraph.NewFilter("format").WithPositionalArgs(value)
			if err := f.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", f), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))

			res, err := ffmpeg.Run(context.Background(), cmd)
			if err == nil {
				t.Fatalf("%q was accepted as a pixel format", value)
			}
			if res == nil {
				t.Fatalf("ffmpeg did not start: %v", err)
			}
			// Unescaped, ffmpeg never reaches the pixel format: it stops
			// at the option name it thinks the part before "=" is.
			if !reachedFilter(res.Stderr, f.String(), value) {
				t.Errorf("%s rendered as %s, and ffmpeg reported:\n%s\nwant %q to arrive as one positional argument",
					f.Name, f, strings.TrimSpace(string(res.Stderr)), value)
			}
		})
	}
}

// TestLiveFilterArgumentKeys holds Validate's key rule to ffmpeg. A value
// is escapable and a key is not: ffmpeg reads an option name with no
// escaping of its own, so a key needing quotes cannot be delivered however
// it is written, and Validate rejecting it has to match a real refusal
// rather than a guess. Each key below is paired with a value holding the
// same character, which must still be accepted.
func TestLiveFilterArgumentKeys(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}

	for _, char := range []string{"=", ":", " ", ",", ";", "[", `\`, "'"} {
		t.Run("key holding "+char, func(t *testing.T) {
			key := "pix" + char + "fmts"
			f := filtergraph.NewFilter("format").WithArg(key, "yuv420p")

			if err := f.Validate(); err == nil {
				t.Errorf("Validate() = nil for the key %q", key)
			}

			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", f), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))
			if _, err := ffmpeg.Run(context.Background(), cmd); err == nil {
				t.Errorf("ffmpeg accepted %q, so the key %q may be deliverable after all", f, key)
			}
		})

		t.Run("value holding "+char, func(t *testing.T) {
			value := "yuv" + char + "420p"
			f := filtergraph.NewFilter("format").WithArg("pix_fmts", value)

			if err := f.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil for a value", err)
			}

			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", f), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))
			res, err := ffmpeg.Run(context.Background(), cmd)
			if err == nil {
				t.Fatalf("%q was accepted as a pixel format", value)
			}
			if res == nil {
				t.Fatalf("ffmpeg did not start: %v", err)
			}
			if !reachedFilter(res.Stderr, f.String(), value) {
				t.Errorf("%s rendered as %s, and ffmpeg reported:\n%s\nwant the value %q to reach the filter whole",
					f.Name, f, strings.TrimSpace(string(res.Stderr)), value)
			}
		})
	}
}

// reachedFilter reports whether ffmpeg named the value back, which it does
// only once both parsers have handed it over whole. Searching the log for
// the value alone would be fooled by the releases that echo the filter
// description in a parse error, because a value that was not escaped
// appears there verbatim; the rendering is removed first, which is a no-op
// when the value was escaped and erases the false positive when it was not.
// Matching on ffmpeg's own wording is avoided: it has changed between the
// releases matrix/test.sh covers, and the readback has not.
func reachedFilter(stderr []byte, rendered, value string) bool {
	return strings.Contains(strings.ReplaceAll(string(stderr), rendered, ""), value)
}
