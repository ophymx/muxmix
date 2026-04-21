package ffmpeg

// See libavformat/concatdec.c

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

type ConcatStream struct {
	ID        string
	Codec     string
	Meta      map[string]string
	ExtraData string
}

func newConcatStream() *ConcatStream {
	return &ConcatStream{
		Meta: make(map[string]string),
	}
}

type ConcatChapter struct {
	ID    string
	Start TimeSpec
	End   TimeSpec
}

type ConcatFile struct {
	Path       string
	Duration   *TimeSpec
	InPoint    *TimeSpec
	OutPoint   *TimeSpec
	PacketMeta map[string]string
	Options    map[string]string
	Streams    []*ConcatStream
}

func newConcatFile(filename string) *ConcatFile {
	return &ConcatFile{
		Path:       filename,
		PacketMeta: make(map[string]string),
		Options:    make(map[string]string),
		Streams:    make([]*ConcatStream, 0),
	}
}

type Concat struct {
	Version  string
	Files    []*ConcatFile
	Chapters []*ConcatChapter
}

func concatUnquote(filename string) string {
	if strings.HasPrefix(filename, "'") && strings.HasSuffix(filename, "'") {
		filename = strings.TrimPrefix(strings.TrimSuffix(filename, "'"), "'")
		filename = strings.ReplaceAll(strings.ReplaceAll(filename, "\\'", "'"), "\\\\", "\\")
	}
	return filename
}

func concatQuote(filename string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(strings.ReplaceAll(filename, "'", "\\'"), "\\", "\\\\"))
}

func concatMaybeQuote(value string) string {
	if strings.ContainsAny(value, " \t'\\") {
		return concatQuote(value)
	}
	return value
}

func concatSortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func splitDirective(line string) (directive, args string, hasArgSep bool) {
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return line, "", false
	}
	return line[:idx], strings.TrimSpace(line[idx+1:]), true
}

var requiresFile map[string]bool = map[string]bool{
	"duration":             true,
	"inpoint":              true,
	"outpoint":             true,
	"file_packet_metadata": true,
	"file_packet_meta":     true,
	"option":               true,
	"stream":               true,
}

var requiresArg map[string]bool = map[string]bool{
	"duration":             true,
	"inpoint":              true,
	"outpoint":             true,
	"file_packet_metadata": true,
	"file_packet_meta":     true,
	"option":               true,
	"exact_stream_id":      true,
	"stream_meta":          true,
	"stream_codec":         true,
	"stream_extradata":     true,
}

var requiresStream map[string]bool = map[string]bool{
	"exact_stream_id":  true,
	"stream_meta":      true,
	"stream_codec":     true,
	"stream_extradata": true,
}

type ConcatParseError struct {
	Filename string
	LineNum  int
	Line     string
	Cause    error
}

func (err *ConcatParseError) Error() string {
	return fmt.Sprintf("invalid concat file '%s':%d %s: %s", err.Filename, err.LineNum, err.Line, err.Cause.Error())
}

func (err *ConcatParseError) Unwrap() error {
	return err.Cause
}

func concatParseErrorf(filename string, lineNum int, line string, format string, args ...any) *ConcatParseError {
	return &ConcatParseError{
		Filename: filename,
		LineNum:  lineNum + 1,
		Line:     line,
		Cause:    fmt.Errorf(format, args...),
	}
}

func concatParseErr(filename string, lineNum int, line string, cause error) *ConcatParseError {
	return &ConcatParseError{
		Filename: filename,
		LineNum:  lineNum + 1,
		Line:     line,
		Cause:    cause,
	}
}

var errInvalidAssignment = errors.New("invalid assignment")
var errInvalidChapter = errors.New("invalid chapter")
var errDuplicateHeader = errors.New("duplicate ffconcat header")
var errHeaderNotFirst = errors.New("ffconcat header must be first directive")
var errInvalidInOutRange = errors.New("inpoint must be less than or equal to outpoint")
var errInvalidChapterRange = errors.New("chapter start must be less than or equal to chapter end")

func ParseConcat(filename string) (concat *Concat, err error) {
	var b []byte
	if b, err = os.ReadFile(filename); err != nil {
		return nil, fmt.Errorf("failed to open concat file '%s': %w", filename, err)
	}
	concat = new(Concat)
	var file *ConcatFile
	var stream *ConcatStream
	seenDirective := false
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		directive, args, hasSpace := splitDirective(line)
		if requiresArg[directive] && (!hasSpace || args == "") {
			return nil, concatParseErrorf(filename, i, line, "directive '%s' missing argument", directive)
		}
		if requiresFile[directive] && file == nil {
			return nil, concatParseErrorf(filename, i, line, "directive '%s' set before 'file'", directive)
		}
		if requiresStream[directive] && stream == nil {
			return nil, concatParseErrorf(filename, i, line, "directive '%s' set before 'stream'", directive)
		}
		switch directive {
		case "file":
			if file != nil {
				concat.Files = append(concat.Files, file)
				if stream != nil {
					file.Streams = append(file.Streams, stream)
					stream = nil
				}
			}
			file = newConcatFile(concatUnquote(args))
		case "duration":
			var ts TimeSpec
			if ts, err = ParseTimeSpec(args); err != nil {
				return nil, concatParseErr(filename, i, line, err)
			}
			file.Duration = &ts
		case "inpoint":
			var ts TimeSpec
			if ts, err = ParseTimeSpec(args); err != nil {
				return nil, concatParseErr(filename, i, line, err)
			}
			file.InPoint = &ts
			if file.OutPoint != nil && *file.InPoint > *file.OutPoint {
				return nil, concatParseErr(filename, i, line, errInvalidInOutRange)
			}
		case "outpoint":
			var ts TimeSpec
			if ts, err = ParseTimeSpec(args); err != nil {
				return nil, concatParseErr(filename, i, line, err)
			}
			file.OutPoint = &ts
			if file.InPoint != nil && *file.InPoint > *file.OutPoint {
				return nil, concatParseErr(filename, i, line, errInvalidInOutRange)
			}
		case "ffconcat":
			if concat.Version != "" {
				return nil, concatParseErr(filename, i, line, errDuplicateHeader)
			}
			if seenDirective {
				return nil, concatParseErr(filename, i, line, errHeaderNotFirst)
			}
			kw, version, hasVersion := splitDirective(args)
			if kw != "version" || !hasVersion || version == "" {
				return nil, concatParseErr(filename, i, line, errInvalidAssignment)
			}
			concat.Version = version
		case "file_packet_metadata":
			key, val, hasEqual := strings.Cut(args, "=")
			if !hasEqual {
				return nil, concatParseErr(filename, i, line, errInvalidAssignment)
			}
			file.PacketMeta[key] = concatUnquote(val)
		case "file_packet_meta":
			key, val, hasSpace := splitDirective(args)
			if !hasSpace {
				return nil, concatParseErr(filename, i, line, errInvalidAssignment)
			}
			file.PacketMeta[key] = concatUnquote(val)
		case "option":
			key, val, hasSpace := splitDirective(args)
			if !hasSpace {
				return nil, concatParseErr(filename, i, line, errInvalidAssignment)
			}
			file.Options[key] = concatUnquote(val)
		case "stream":
			if stream != nil {
				file.Streams = append(file.Streams, stream)
			}
			stream = newConcatStream()
		case "exact_stream_id":
			stream.ID = args
		case "stream_meta":
			key, val, hasSpace := splitDirective(args)
			if !hasSpace {
				return nil, concatParseErr(filename, i, line, errInvalidAssignment)
			}
			stream.Meta[key] = concatUnquote(val)
		case "stream_codec":
			stream.Codec = args
		case "stream_extradata":
			stream.ExtraData = args
		case "chapter":
			chapterArgs := strings.Fields(args)
			if len(chapterArgs) != 3 {
				return nil, concatParseErr(filename, i, line, errInvalidChapter)
			}
			chapter := &ConcatChapter{
				ID: chapterArgs[0],
			}
			if chapter.Start, err = ParseTimeSpec(chapterArgs[1]); err != nil {
				return nil, concatParseErr(filename, i, line, fmt.Errorf("invalid chapter start: %w", err))
			}
			if chapter.End, err = ParseTimeSpec(chapterArgs[2]); err != nil {
				return nil, concatParseErr(filename, i, line, fmt.Errorf("invalid chapter end: %w", err))
			}
			if chapter.Start > chapter.End {
				return nil, concatParseErr(filename, i, line, errInvalidChapterRange)
			}
			concat.Chapters = append(concat.Chapters, chapter)
		default:
			return nil, concatParseErr(filename, i, line, fmt.Errorf("unsupported directive '%s'", directive))
		}
		seenDirective = true
	}
	if file != nil {
		concat.Files = append(concat.Files, file)
		if stream != nil {
			file.Streams = append(file.Streams, stream)
		}
	}
	return
}

func (concat *Concat) String() string {
	buf := new(strings.Builder)
	if concat.Version != "" {
		buf.WriteString("ffconcat version ")
		buf.WriteString(concat.Version)
		buf.WriteByte('\n')
	}
	for _, file := range concat.Files {
		buf.WriteString("file ")
		buf.WriteString(concatQuote(file.Path))
		buf.WriteByte('\n')

		if file.Duration != nil {
			buf.WriteString("duration ")
			buf.WriteString(file.Duration.String())
			buf.WriteByte('\n')
		}
		if file.InPoint != nil {
			buf.WriteString("inpoint ")
			buf.WriteString(file.InPoint.String())
			buf.WriteByte('\n')
		}
		if file.OutPoint != nil {
			buf.WriteString("outpoint ")
			buf.WriteString(file.OutPoint.String())
			buf.WriteByte('\n')
		}
		for _, key := range concatSortedKeys(file.PacketMeta) {
			val := file.PacketMeta[key]
			buf.WriteString("file_packet_meta ")
			buf.WriteString(key)
			buf.WriteByte(' ')
			buf.WriteString(concatMaybeQuote(val))
			buf.WriteByte('\n')
		}
		for _, key := range concatSortedKeys(file.Options) {
			val := file.Options[key]
			buf.WriteString("option ")
			buf.WriteString(key)
			buf.WriteByte(' ')
			buf.WriteString(concatMaybeQuote(val))
			buf.WriteByte('\n')
		}
		for _, stream := range file.Streams {
			buf.WriteString("stream\n")
			if stream.ID != "" {
				buf.WriteString("exact_stream_id ")
				buf.WriteString(stream.ID)
				buf.WriteByte('\n')
			}
			if stream.Codec != "" {
				buf.WriteString("stream_codec ")
				buf.WriteString(stream.Codec)
				buf.WriteByte('\n')
			}
			for _, key := range concatSortedKeys(stream.Meta) {
				val := stream.Meta[key]
				buf.WriteString("stream_meta ")
				buf.WriteString(key)
				buf.WriteByte(' ')
				buf.WriteString(concatMaybeQuote(val))
				buf.WriteByte('\n')
			}
			if stream.ExtraData != "" {
				buf.WriteString("stream_extradata ")
				buf.WriteString(stream.ExtraData)
				buf.WriteByte('\n')
			}
		}
	}
	for _, chapter := range concat.Chapters {
		buf.WriteString("chapter ")
		buf.WriteString(chapter.ID)
		buf.WriteByte(' ')
		buf.WriteString(chapter.Start.String())
		buf.WriteByte(' ')
		buf.WriteString(chapter.End.String())
		buf.WriteByte('\n')
	}
	return buf.String()
}
