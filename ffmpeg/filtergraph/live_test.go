package filtergraph_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/filtergraph"
)

// ─── live tests against a real ffmpeg ──────────────────────────────────────

// TestLiveFilterArgumentOrder holds the orderings Validate accepts to a
// real ffmpeg: every one of them has to parse, on every release
// matrix/test.sh covers.
func TestLiveFilterArgumentOrder(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}

	for _, tc := range []struct {
		name   string
		filter *filtergraph.Filter
	}{
		{"all positional", filtergraph.NewFilter("pad").WithPositionalArgs("1280", "720", "-1", "-1", "black")},
		{"all named", filtergraph.NewFilter("scale").WithArg("w", "1280").WithArg("h", "-2")},
		{"positional then named", filtergraph.NewFilter("pad").WithPositionalArgs("1280", "720", "-1", "-1").WithArg("color", "black")},
		// Filling only some of the positional slots before switching to
		// names is allowed: here w, then h by name.
		{"some positional then named", filtergraph.NewFilter("scale").WithPositionalArgs("1280").WithArg("h", "-2")},
		// An empty value keeps its key, so it stays a named argument and
		// the ordering rule is untouched by it. Rendered bare it would be
		// a positional argument, and this would not parse. Which options
		// take an empty value is the filter's business and moves between
		// releases — scale's flags= is refused by 4.4 and accepted from
		// 5.1 — so the readback here is metadata's key=, which every
		// release in the matrix takes.
		{"empty value is a named argument", filtergraph.NewFilter("metadata").WithArg("mode", "print").WithArg("key", "")},
		{"raw args carry their own order", filtergraph.NewFilter("pad").WithRawArgs("1280:720:-1:-1:color=black")},
		{"a value needing quotes survives the round trip", filtergraph.NewFilter("drawbox").WithArg("enable", "between(t,0,5)").WithArg("color", "red")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.filter.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil for %q", err, tc.filter)
			}

			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", tc.filter), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))

			if _, err := ffmpeg.Run(context.Background(), cmd); err != nil {
				t.Errorf("ffmpeg rejected %q: %v", tc.filter, err)
			}
		})
	}
}

// TestLiveFilterMisorderedArguments is the other half of the ordering rule,
// and the reason Validate cannot simply mirror whether ffmpeg runs: a
// positional argument after a named one is refused by some releases and
// silently misread by the rest.
//
// From 7.1 on, libavfilter gives up the remaining shorthand at the first
// key=value and answers "No option name near '...'". Up to 6.1 it did not:
// the walk over the filter's options carried on from where the named
// argument left it, so the bare arguments landed on whatever option came
// next and the description ran. pad=color=black:1280:720 pads a 64x64 input
// to 64x1280 there, having read 1280 as the height and 720 as x.
//
// A misreading is not always quiet, which is the other reason not to test
// for a refusal: 5.1 reads pad=color=black:1280:720 the same way but lands
// 1280 on eval, an enum, and fails there. The description was still not
// read as written.
//
// So the live claim is not "ffmpeg refuses this" — half the matrix runs it.
// It is that ffmpeg never reads it as written: each case carries the frame
// size it names, and ffmpeg has to either not reach a filter at all or
// report some other size.
func TestLiveFilterMisorderedArguments(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}

	for _, tc := range []struct {
		name string
		// filter pads to asWritten if ffmpeg reads it as written.
		filter    *filtergraph.Filter
		asWritten string
	}{
		{"named then positional", filtergraph.NewFilter("pad").WithArg("color", "black").WithPositionalArgs("1280", "720"), "1280x720"},
		{"positional after a named", filtergraph.NewFilter("pad").WithPositionalArgs("1280", "720").WithArg("color", "black").WithPositionalArgs("-1"), "1280x720"},
		{"positional after an empty-valued named argument", filtergraph.NewFilter("pad").WithArg("color", "").WithPositionalArgs("1280", "720"), "1280x720"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.filter.Validate(); err == nil {
				t.Errorf("Validate() = nil for %q, want the ordering rejected", tc.filter)
			}

			size, err := paddedSize(t, tc.filter)
			if err != nil {
				// ffmpeg never got as far as a frame, either refusing the
				// description or misreading it into a value the filter
				// would not take. Nothing to disagree with a size about.
				return
			}
			if size == tc.asWritten {
				t.Errorf("ffmpeg read %q as written and padded to %s, so the ordering is deliverable after all and Validate should not reject it",
					tc.filter, size)
				return
			}
			t.Logf("ffmpeg accepted %q and padded to %s, not the %s it names", tc.filter, size, tc.asWritten)
		})
	}
}

// showinfoSize picks the frame size out of a showinfo line.
var showinfoSize = regexp.MustCompile(`\bs:(\d+x\d+)`)

// paddedSize runs the filter with a showinfo behind it and reads back the
// size of the frame that reached it, which is how a description ffmpeg
// accepts is held to meaning what it says rather than merely parsing. The
// error it returns is ffmpeg's, from anywhere in the graph; a run that
// succeeds without naming a size is the test's bug, not the filter's.
func paddedSize(t *testing.T, f *filtergraph.Filter) (string, error) {
	t.Helper()
	// showinfo logs at info, which the runner's default -loglevel error
	// would swallow.
	chain := filtergraph.NewFilterChain().Add(f).Add(filtergraph.NewFilter("showinfo"))
	cmd := ffmpeg.NewCommand().
		GlobalOptions(ffmpeg.LogLevel("info")).
		Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
		Output("-", ffmpeg.Filter("v", chain), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))

	res, err := ffmpeg.Run(context.Background(), cmd)
	if err != nil {
		return "", err
	}
	match := showinfoSize.FindSubmatch(res.Stderr)
	if match == nil {
		t.Fatalf("no showinfo frame size in ffmpeg's log for %q:\n%s", chain, strings.TrimSpace(string(res.Stderr)))
	}
	return string(match[1]), nil
}

// TestLiveFilterArgumentEscaping checks that a value arrives at the filter
// byte for byte. The setpts filter is the readback: every value below is an
// invalid expression, so ffmpeg names the one it received ("Error while
// parsing expression '...'"), and finding the value in its log means both
// parsers handed it over intact. A value split on ":", stripped of a
// backslash or trimmed of its whitespace fails the search instead.
//
// setpts rather than something more obvious because its one option holds a
// string ffmpeg does not take apart. format's pix_fmts looks like the
// better readback and is not: 4.4 reads ":" in it as a list separator and
// reports the halves, so a value carrying one is never named back whole
// however faithfully it was delivered.
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
			f := filtergraph.NewFilter("setpts").WithArg("expr", value)
			if err := f.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", f), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))

			res, err := ffmpeg.Run(context.Background(), cmd)
			if err == nil {
				t.Fatalf("%q was accepted as an expression", value)
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
// to stay positional. The setpts filter is the readback again: its one
// option takes the bare argument, and an invalid expression is named back
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
			f := filtergraph.NewFilter("setpts").WithPositionalArgs(value)
			if err := f.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", f), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))

			res, err := ffmpeg.Run(context.Background(), cmd)
			if err == nil {
				t.Fatalf("%q was accepted as an expression", value)
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
			key := "ex" + char + "pr"
			f := filtergraph.NewFilter("setpts").WithArg(key, "PTS")

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
			value := "zz" + char + "zz"
			f := filtergraph.NewFilter("setpts").WithArg("expr", value)

			if err := f.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil for a value", err)
			}

			cmd := ffmpeg.NewCommand().
				Input("color=c=red:s=64x64:d=0.1", ffmpeg.Lavfi()).
				Output("-", ffmpeg.Filter("v", f), ffmpeg.Frames("v", 1), ffmpeg.Format("null"))
			res, err := ffmpeg.Run(context.Background(), cmd)
			if err == nil {
				t.Fatalf("%q was accepted as an expression", value)
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
