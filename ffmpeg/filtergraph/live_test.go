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
// on ":" or stripped of a backslash fails the search instead.
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
