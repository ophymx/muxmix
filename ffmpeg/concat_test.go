package ffmpeg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConcatFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.ffconcat")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write concat fixture: %v", err)
	}
	return path
}

func TestConcatQuoteUnquote(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "simple", input: "video.mp4"},
		{name: "backslashes", input: `path\\with\\slashes.avi`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			quoted := concatQuote(tc.input)
			if !strings.HasPrefix(quoted, "'") || !strings.HasSuffix(quoted, "'") {
				t.Fatalf("concatQuote(%q) = %q, expected single-quoted output", tc.input, quoted)
			}
			if got := concatUnquote(quoted); got != tc.input {
				t.Fatalf("concatUnquote(concatQuote(%q)) = %q, want %q", tc.input, got, tc.input)
			}
		})
	}
}

func TestConcatUnquoteEscapedSingleQuote(t *testing.T) {
	input := `'quote\'s here.mkv'`
	if got := concatUnquote(input); got != "quote's here.mkv" {
		t.Fatalf("concatUnquote(%q) = %q, want %q", input, got, "quote's here.mkv")
	}
}

func TestConcatUnquoteDoesNotUnescapeUnquoted(t *testing.T) {
	input := `path\\with\\slashes.mp4`
	if got := concatUnquote(input); got != input {
		t.Fatalf("concatUnquote(%q) = %q, want unchanged %q", input, got, input)
	}
}

func TestParseConcatValid(t *testing.T) {
	path := writeConcatFixture(t, strings.Join([]string{
		"ffconcat version 1.0",
		"file 'first file.mp4'",
		"duration 00:00:10.50",
		"inpoint 00:00:01.00",
		"outpoint 00:00:09.00",
		"file_packet_meta language eng",
		"option safe 0",
		"stream",
		"exact_stream_id 0:0",
		"stream_codec h264",
		"stream_meta title Main",
		"stream_extradata deadbeef",
		"chapter intro 00:00:00.00 00:00:03.00",
		"file second.mp4",
	}, "\n"))

	got, err := ParseConcat(path)
	if err != nil {
		t.Fatalf("ParseConcat returned error: %v", err)
	}

	if got.Version != "1.0" {
		t.Fatalf("Version = %q, want 1.0", got.Version)
	}
	if len(got.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(got.Files))
	}

	first := got.Files[0]
	if first.Path != "first file.mp4" {
		t.Fatalf("first.Path = %q, want %q", first.Path, "first file.mp4")
	}
	if first.Duration == nil || time.Duration(*first.Duration) != 10*time.Second+500*time.Millisecond {
		t.Fatalf("first.Duration = %v, want 10.5s", first.Duration)
	}
	if first.InPoint == nil || time.Duration(*first.InPoint) != 1*time.Second {
		t.Fatalf("first.InPoint = %v, want 1s", first.InPoint)
	}
	if first.OutPoint == nil || time.Duration(*first.OutPoint) != 9*time.Second {
		t.Fatalf("first.OutPoint = %v, want 9s", first.OutPoint)
	}
	if first.PacketMeta["language"] != "eng" {
		t.Fatalf("first.PacketMeta[language] = %q, want eng", first.PacketMeta["language"])
	}
	if first.Options["safe"] != "0" {
		t.Fatalf("first.Options[safe] = %q, want 0", first.Options["safe"])
	}
	if len(first.Streams) != 1 {
		t.Fatalf("len(first.Streams) = %d, want 1", len(first.Streams))
	}

	stream := first.Streams[0]
	if stream.ID != "0:0" {
		t.Fatalf("stream.ID = %q, want 0:0", stream.ID)
	}
	if stream.Codec != "h264" {
		t.Fatalf("stream.Codec = %q, want h264", stream.Codec)
	}
	if stream.Meta["title"] != "Main" {
		t.Fatalf("stream.Meta[title] = %q, want Main", stream.Meta["title"])
	}
	if stream.ExtraData != "deadbeef" {
		t.Fatalf("stream.ExtraData = %q, want deadbeef", stream.ExtraData)
	}

	second := got.Files[1]
	if second.Path != "second.mp4" {
		t.Fatalf("second.Path = %q, want second.mp4", second.Path)
	}

	if len(got.Chapters) != 1 {
		t.Fatalf("len(Chapters) = %d, want 1", len(got.Chapters))
	}
	chapter := got.Chapters[0]
	if chapter.ID != "intro" {
		t.Fatalf("chapter.ID = %q, want intro", chapter.ID)
	}
	if time.Duration(chapter.Start) != 0 || time.Duration(chapter.End) != 3*time.Second {
		t.Fatalf("chapter range = [%v, %v], want [0s, 3s]", time.Duration(chapter.Start), time.Duration(chapter.End))
	}
}

func TestParseConcatErrors(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantCause error
		wantPart  string
	}{
		{
			name:      "requires file before duration",
			content:   "duration 00:00:01.00\n",
			wantPart:  "directive 'duration' set before 'file'",
			wantCause: nil,
		},
		{
			name:      "requires stream before stream_meta",
			content:   "file x.mp4\nstream_meta k v\n",
			wantPart:  "directive 'stream_meta' set before 'stream'",
			wantCause: nil,
		},
		{
			name:      "missing arg",
			content:   "file x.mp4\nduration\n",
			wantPart:  "directive 'duration' missing argument",
			wantCause: nil,
		},
		{
			name:      "invalid chapter",
			content:   "file x.mp4\nchapter one two\n",
			wantPart:  "invalid chapter",
			wantCause: errInvalidChapter,
		},
		{
			name:      "invalid assignment",
			content:   "file x.mp4\nfile_packet_metadata noequals\n",
			wantPart:  "invalid assignment",
			wantCause: errInvalidAssignment,
		},
		{
			name:      "unsupported directive",
			content:   "file x.mp4\nunknown stuff\n",
			wantPart:  "unsupported directive 'unknown'",
			wantCause: nil,
		},
		{
			name:      "ffconcat must be first",
			content:   "file x.mp4\nffconcat version 1.0\n",
			wantPart:  "ffconcat header must be first directive",
			wantCause: errHeaderNotFirst,
		},
		{
			name:      "duplicate ffconcat header",
			content:   "ffconcat version 1.0\nffconcat version 1.0\nfile x.mp4\n",
			wantPart:  "duplicate ffconcat header",
			wantCause: errDuplicateHeader,
		},
		{
			name:      "invalid inpoint outpoint range",
			content:   "file x.mp4\ninpoint 00:00:03.00\noutpoint 00:00:01.00\n",
			wantPart:  "inpoint must be less than or equal to outpoint",
			wantCause: errInvalidInOutRange,
		},
		{
			name:      "invalid chapter range",
			content:   "file x.mp4\nchapter c1 00:00:05.00 00:00:01.00\n",
			wantPart:  "chapter start must be less than or equal to chapter end",
			wantCause: errInvalidChapterRange,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConcatFixture(t, tc.content)
			_, err := ParseConcat(path)
			if err == nil {
				t.Fatal("ParseConcat() error = nil, want non-nil")
			}
			var parseErr *ConcatParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("error type = %T, want *ConcatParseError", err)
			}
			if tc.name == "missing arg" && parseErr.LineNum != 2 {
				t.Fatalf("parseErr.LineNum = %d, want 2", parseErr.LineNum)
			}
			if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Fatalf("errors.Is(..., %v) = false, got err=%v", tc.wantCause, err)
			}
			if !strings.Contains(err.Error(), tc.wantPart) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantPart)
			}
		})
	}
}

func TestParseConcatTabSeparatedAssignments(t *testing.T) {
	path := writeConcatFixture(t, strings.Join([]string{
		"ffconcat version 1.0",
		"file x.mp4",
		"option\tsafe\t0",
		"file_packet_meta\tlanguage\teng",
		"stream",
		"stream_meta\ttitle\t'Main Audio'",
	}, "\n"))

	got, err := ParseConcat(path)
	if err != nil {
		t.Fatalf("ParseConcat returned error: %v", err)
	}

	if got.Files[0].Options["safe"] != "0" {
		t.Fatalf("option safe = %q, want 0", got.Files[0].Options["safe"])
	}
	if got.Files[0].PacketMeta["language"] != "eng" {
		t.Fatalf("packet meta language = %q, want eng", got.Files[0].PacketMeta["language"])
	}
	if got.Files[0].Streams[0].Meta["title"] != "Main Audio" {
		t.Fatalf("stream meta title = %q, want Main Audio", got.Files[0].Streams[0].Meta["title"])
	}
}

func TestConcatString(t *testing.T) {
	dur := TimeSpec(2 * time.Second)
	in := TimeSpec(500 * time.Millisecond)
	out := TimeSpec(1500 * time.Millisecond)

	concat := &Concat{
		Version: "1.0",
		Files: []*ConcatFile{
			{
				Path:       "a'b\\c.mp4",
				Duration:   &dur,
				InPoint:    &in,
				OutPoint:   &out,
				PacketMeta: map[string]string{"lang": "eng"},
				Options:    map[string]string{"safe": "0"},
				Streams: []*ConcatStream{
					{
						ID:        "0:0",
						Codec:     "h264",
						Meta:      map[string]string{"title": "Main Title"},
						ExtraData: "cafebabe",
					},
				},
			},
		},
		Chapters: []*ConcatChapter{
			{ID: "intro", Start: 0, End: TimeSpec(1 * time.Second)},
		},
	}

	outStr := concat.String()
	expectedFileLine := "file " + concatQuote("a'b\\c.mp4") + "\n"
	checks := []string{
		"ffconcat version 1.0\n",
		expectedFileLine,
		"duration 00:00:02.00\n",
		"inpoint 00:00:00.50\n",
		"outpoint 00:00:01.50\n",
		"file_packet_meta lang eng\n",
		"option safe 0\n",
		"stream\n",
		"exact_stream_id 0:0\n",
		"stream_codec h264\n",
		"stream_meta title 'Main Title'\n",
		"stream_extradata cafebabe\n",
		"chapter intro 00:00:00.00 00:00:01.00\n",
	}

	for _, want := range checks {
		if !strings.Contains(outStr, want) {
			t.Fatalf("concat.String() missing %q\nfull output:\n%s", want, outStr)
		}
	}
}

func TestConcatStringDeterministicOrderAndRoundTrip(t *testing.T) {
	concat := &Concat{
		Version: "1.0",
		Files: []*ConcatFile{
			{
				Path:       "input.mp4",
				PacketMeta: map[string]string{"z": "last", "a": "first value"},
				Options:    map[string]string{"safe": "0", "movflags": "+faststart"},
				Streams: []*ConcatStream{
					{
						ID:    "0:1",
						Codec: "aac",
						Meta:  map[string]string{"title": "Main Audio", "lang": "eng"},
					},
				},
			},
		},
	}

	out1 := concat.String()
	out2 := concat.String()
	if out1 != out2 {
		t.Fatalf("concat.String() output is non-deterministic\nout1:\n%s\nout2:\n%s", out1, out2)
	}

	path := writeConcatFixture(t, out1)
	parsed, err := ParseConcat(path)
	if err != nil {
		t.Fatalf("ParseConcat(concat.String()) returned error: %v", err)
	}

	if len(parsed.Files) != 1 || len(parsed.Files[0].Streams) != 1 {
		t.Fatalf("unexpected parsed shape: files=%d streams=%d", len(parsed.Files), len(parsed.Files[0].Streams))
	}

	stream := parsed.Files[0].Streams[0]
	if stream.ID != "0:1" {
		t.Fatalf("stream.ID = %q, want 0:1", stream.ID)
	}
	if stream.Meta["title"] != "Main Audio" {
		t.Fatalf("stream.Meta[title] = %q, want %q", stream.Meta["title"], "Main Audio")
	}
	if parsed.Files[0].PacketMeta["a"] != "first value" {
		t.Fatalf("parsed packet meta a = %q, want %q", parsed.Files[0].PacketMeta["a"], "first value")
	}
}
