// Package ffprobe runs ffprobe and decodes its JSON output into Go types
// generated from FFmpeg's own schema.
//
// The generated types accept the output of every FFmpeg release from 4.4
// onward: fields a given version does not print are simply absent, which the
// value types report through their Valid methods.
//
//	res, err := ffprobe.Probe(ctx, "movie.mkv")
//	if err != nil {
//		return err
//	}
//	v := res.VideoStream()
//	fmt.Println(v.Width.Int(), v.Height.Int(), v.FrameRate().Float64())
//
// Sections beyond format and streams are requested with options:
//
//	res, err := ffprobe.Probe(ctx, "movie.mkv",
//		ffprobe.ShowChapters(), ffprobe.ShowPrograms(), ffprobe.CountFrames())
//
// Sections that only newer releases print, such as stream groups (7.0+),
// can be gated on the binary. Prober.Version is cached, so the check is
// free after the first call, and Prober.Sections lists exactly what the
// running binary can print:
//
//	if v, err := ffprobe.Version(ctx); err == nil && v.AtLeast(7, 0) {
//		res, err = ffprobe.Probe(ctx, "movie.iamf", ffprobe.ShowStreamGroups())
//	}
//
// Packet and frame listings can be very large; Frames, Packets and
// PacketsAndFrames stream them one at a time instead of building a Result.
package ffprobe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Prober runs an ffprobe binary. It is safe for concurrent use. Make one
// per binary and keep it: it caches the binary's version (and, through
// Sections, its feature set), so a Prober built per call re-runs those
// probes every time.
type Prober struct {
	binary  string
	env     []string
	stderr  io.Writer
	timeout time.Duration

	versionMu sync.Mutex
	version   *VersionInfo
}

// ProberOption configures a Prober.
type ProberOption func(*Prober)

// WithBinary sets the ffprobe executable path (default "ffprobe" from PATH).
func WithBinary(path string) ProberOption {
	return func(p *Prober) {
		if path != "" {
			p.binary = path
		}
	}
}

// WithEnv sets the environment for ffprobe processes (default: inherited).
func WithEnv(env ...string) ProberOption {
	return func(p *Prober) { p.env = append([]string(nil), env...) }
}

// WithStderr forwards ffprobe's log output to w in addition to capturing it
// for error messages.
func WithStderr(w io.Writer) ProberOption {
	return func(p *Prober) { p.stderr = w }
}

// WithTimeout bounds every probe this Prober runs; ffprobe is killed when
// the timeout expires and the error wraps context.DeadlineExceeded. The
// per-call Timeout option overrides it. Zero (the default) means no limit
// beyond the caller's context.
func WithTimeout(d time.Duration) ProberOption {
	return func(p *Prober) { p.timeout = d }
}

// New returns a Prober. Build it once and share it; see Prober.
func New(opts ...ProberOption) *Prober {
	p := &Prober{binary: "ffprobe"}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Default is the Prober used by the package-level functions.
var Default = New()

// Binary returns the executable this Prober runs.
func (p *Prober) Binary() string { return p.binary }

// ValidateInstall reports ErrFFProbeNotFound when the binary is not
// available.
func (p *Prober) ValidateInstall() error {
	if _, err := exec.LookPath(p.binary); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrFFProbeNotFound, p.binary, err)
	}
	return nil
}

// startError classifies a failure to launch the binary. A missing
// executable becomes ErrFFProbeNotFound so it is never confused with a
// missing input file; anything else is reported as a start failure with the
// original cause still wrapped.
func (p *Prober) startError(err error) error {
	var pe *fs.PathError
	if errors.Is(err, exec.ErrNotFound) || (errors.As(err, &pe) && errors.Is(pe.Err, fs.ErrNotExist)) {
		return fmt.Errorf("%w: %s: %v", ErrFFProbeNotFound, p.binary, err)
	}
	return fmt.Errorf("ffprobe: start %s: %w", p.binary, err)
}

// ValidateInstall checks the default Prober's binary.
func ValidateInstall() error { return Default.ValidateInstall() }

// Probe runs the default Prober. With no section options it shows format and
// streams.
func Probe(ctx context.Context, input string, opts ...Option) (*Result, error) {
	return Default.Probe(ctx, input, opts...)
}

// ProbeReader probes data read from r using the default Prober.
func ProbeReader(ctx context.Context, r io.Reader, opts ...Option) (*Result, error) {
	return Default.ProbeReader(ctx, r, opts...)
}

// Version returns the default Prober's ffprobe version information.
func Version(ctx context.Context) (*VersionInfo, error) { return Default.Version(ctx) }

// ─── options ───────────────────────────────────────────────────────────────

// Option configures a single probe.
type Option func(*config)

type config struct {
	sections     []string // -show_* flags requested
	modifiers    []string // flags that alter sections (-count_frames, -select_streams ...)
	inputOptions []string // options that precede -i
	extra        []string // appended verbatim before the input
	stdin        io.Reader
	timeout      time.Duration
	hasTimeout   bool
}

func (c *config) apply(opts []Option) {
	for _, o := range opts {
		o(c)
	}
}

func section(flag string) Option {
	return func(c *config) { c.sections = append(c.sections, flag) }
}

// ShowFormat includes the container section (Result.Format).
func ShowFormat() Option { return section("-show_format") }

// ShowStreams includes the stream list (Result.Streams).
func ShowStreams() Option { return section("-show_streams") }

// ShowChapters includes chapters (Result.Chapters).
func ShowChapters() Option { return section("-show_chapters") }

// ShowPrograms includes MPEG-TS style programs (Result.Programs).
func ShowPrograms() Option { return section("-show_programs") }

// ShowStreamGroups includes stream groups such as IAMF audio elements
// (Result.StreamGroups). Requires ffprobe 7.0 or newer; gate it on
// Version(ctx).AtLeast(7, 0) or Sections.
func ShowStreamGroups() Option { return section("-show_stream_groups") }

// ShowPackets includes every packet (Result.Packets). Combined with
// ShowFrames the output moves to Result.PacketsAndFrames.
func ShowPackets() Option { return section("-show_packets") }

// ShowFrames includes every decoded frame (Result.Frames). Decoding the
// whole file is slow; combine with ReadIntervals or SelectStreams, or use
// Prober.Frames to stream.
func ShowFrames() Option { return section("-show_frames") }

// ShowProgramVersion includes Result.ProgramVersion.
func ShowProgramVersion() Option { return section("-show_program_version") }

// ShowLibraryVersions includes Result.LibraryVersions.
func ShowLibraryVersions() Option { return section("-show_library_versions") }

// ShowPixelFormats includes the pixel format table (Result.PixelFormats).
func ShowPixelFormats() Option { return section("-show_pixel_formats") }

// ShowData adds a hex dump of packet payloads (Packet.Data).
func ShowData() Option { return func(c *config) { c.modifiers = append(c.modifiers, "-show_data") } }

// ShowDataHash adds a hash of each packet payload and stream extradata using
// the given algorithm (for example "md5", "sha256", "crc32").
func ShowDataHash(algorithm string) Option {
	return func(c *config) { c.modifiers = append(c.modifiers, "-show_data_hash", algorithm) }
}

// CountFrames decodes the input to fill Stream.NbReadFrames, which
// Stream.FrameCount reads.
func CountFrames() Option {
	return func(c *config) { c.modifiers = append(c.modifiers, "-count_frames") }
}

// CountPackets reads the input to fill Stream.NbReadPackets.
func CountPackets() Option {
	return func(c *config) { c.modifiers = append(c.modifiers, "-count_packets") }
}

// ShowOptionalFields controls whether values ffprobe cannot determine are
// printed as "N/A" (true) or omitted (false, ffprobe's JSON default). The
// generated types treat both the same way. Requires ffprobe 5.0 or newer.
func ShowOptionalFields(always bool) Option {
	v := "never"
	if always {
		v = "always"
	}
	return func(c *config) { c.modifiers = append(c.modifiers, "-show_optional_fields", v) }
}

// SelectStreams restricts stream, packet and frame sections to the streams
// matched by an ffmpeg stream specifier such as "v:0", "a" or "0".
func SelectStreams(spec string) Option {
	return func(c *config) { c.modifiers = append(c.modifiers, "-select_streams", spec) }
}

// ShowEntries limits the printed fields using ffprobe's section=field syntax,
// for example "format=duration:stream=codec_type,width,height". It implies
// the sections it names.
func ShowEntries(spec string) Option {
	return func(c *config) {
		c.sections = append(c.sections, "-show_entries", spec)
	}
}

// ReadIntervals limits packet and frame reading to the given interval
// specification, for example "%+#30" (the first 30 packets) or
// "10%+5" (5 seconds starting at 10 seconds).
func ReadIntervals(spec string) Option {
	return func(c *config) { c.modifiers = append(c.modifiers, "-read_intervals", spec) }
}

// InputFormat forces the demuxer (ffprobe -f), needed for headerless input
// such as raw PCM or when probing from a pipe.
func InputFormat(name string) Option {
	return func(c *config) { c.inputOptions = append(c.inputOptions, "-f", name) }
}

// InputOption passes an arbitrary demuxer or codec option before the input,
// for example InputOption("probesize", "50M") or
// InputOption("framerate", "30").
func InputOption(name, value string) Option {
	return func(c *config) { c.inputOptions = append(c.inputOptions, "-"+name, value) }
}

// Probesize sets how many bytes ffprobe inspects when detecting the format.
func Probesize(bytes int64) Option {
	return InputOption("probesize", strconv.FormatInt(bytes, 10))
}

// AnalyzeDuration sets how much of the input ffprobe examines when detecting
// streams. Long files with late-starting streams need a larger value.
func AnalyzeDuration(d time.Duration) Option {
	return InputOption("analyzeduration", strconv.FormatInt(d.Microseconds(), 10))
}

// Args appends raw ffprobe arguments after the generated ones.
func Args(args ...string) Option {
	return func(c *config) { c.extra = append(c.extra, args...) }
}

// Timeout bounds this one probe, overriding the Prober's WithTimeout;
// ffprobe is killed when it expires and the error wraps
// context.DeadlineExceeded. Zero disables the Prober's default for this
// call.
func Timeout(d time.Duration) Option {
	return func(c *config) { c.timeout, c.hasTimeout = d, true }
}

func withStdin(r io.Reader) Option {
	return func(c *config) { c.stdin = r }
}

// ─── running ───────────────────────────────────────────────────────────────

// Probe runs ffprobe on input, which may be a path or any URL ffmpeg
// understands. With no section options it shows format and streams.
func (p *Prober) Probe(ctx context.Context, input string, opts ...Option) (*Result, error) {
	raw, err := p.Run(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("ffprobe: decode output: %w", err)
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return &res, nil
}

// ProbeReader probes data streamed from r through ffprobe's stdin. Formats
// that need to seek (most mp4 files, for example) cannot be probed this way;
// pass InputFormat when the data has no self-describing header.
func (p *Prober) ProbeReader(ctx context.Context, r io.Reader, opts ...Option) (*Result, error) {
	return p.Probe(ctx, "pipe:0", append(opts, withStdin(r))...)
}

// Run executes ffprobe and returns its raw JSON output without decoding it.
// The output is returned even when ffprobe reports an error, so the caller
// can inspect the "error" section; a structured *ProbeError is returned
// alongside it in that case.
func (p *Prober) Run(ctx context.Context, input string, opts ...Option) ([]byte, error) {
	var c config
	c.apply(opts)
	args := p.buildArgs(&c, input)

	run := p.newRun(ctx, &c, input)
	defer run.cancel()
	cmd := p.command(run.ctx, args, &c)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = p.stderrWriter(&stderr)

	runErr := cmd.Run()
	out := stdout.Bytes()

	if probeErr := errorSection(out); probeErr != nil {
		return out, probeErr
	}
	if runErr != nil {
		if err := run.err(); err != nil {
			return out, err
		}
		if cmd.ProcessState == nil {
			return out, p.startError(runErr)
		}
		return out, &ExitError{
			ExitCode: cmd.ProcessState.ExitCode(),
			Stderr:   strings.TrimSpace(stderr.String()),
			Args:     args,
			Cause:    runErr,
		}
	}
	return out, nil
}

// probeRun is the context bookkeeping for one ffprobe process: the
// caller's context, the timeout in force, and the derived context the
// process runs under.
type probeRun struct {
	parent  context.Context
	ctx     context.Context
	cancel  context.CancelFunc
	input   string
	timeout time.Duration
}

// newRun derives the process context, applying the per-call timeout when
// given and the Prober's default otherwise.
func (p *Prober) newRun(ctx context.Context, c *config, input string) *probeRun {
	r := &probeRun{parent: ctx, input: input, timeout: p.timeout}
	if c.hasTimeout {
		r.timeout = c.timeout
	}
	if r.timeout > 0 {
		r.ctx, r.cancel = context.WithTimeout(ctx, r.timeout)
	} else {
		r.ctx, r.cancel = context.WithCancel(ctx)
	}
	return r
}

// err reports why the process context ended, or nil when it has not. The
// error names ffprobe and the input and wraps the context error, so
// errors.Is(err, context.DeadlineExceeded) holds whether the deadline was
// the caller's or the timeout option's.
func (r *probeRun) err() error {
	cause := r.ctx.Err()
	if cause == nil {
		return nil
	}
	if r.parent.Err() == nil && cause == context.DeadlineExceeded {
		return fmt.Errorf("ffprobe: %s: timed out after %v: %w", r.input, r.timeout, cause)
	}
	return fmt.Errorf("ffprobe: %s: %w", r.input, cause)
}

// PixelFormats returns the pixel formats the binary supports.
func (p *Prober) PixelFormats(ctx context.Context) ([]*PixelFormat, error) {
	raw, err := p.runBare(ctx, "-show_pixel_formats")
	if err != nil {
		return nil, err
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("ffprobe: decode pixel formats: %w", err)
	}
	return res.PixelFormats, nil
}

func (p *Prober) runBare(ctx context.Context, flags ...string) ([]byte, error) {
	// No -show_error here: with it ffprobe insists on an input file.
	args := append([]string{"-hide_banner", "-loglevel", "error", "-print_format", "json"}, flags...)
	cmd := p.command(ctx, args, &config{})
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = p.stderrWriter(&stderr)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("ffprobe: %s: %w", strings.Join(flags, " "), ctx.Err())
		}
		if cmd.ProcessState == nil {
			return nil, p.startError(err)
		}
		return nil, &ExitError{ExitCode: cmd.ProcessState.ExitCode(), Stderr: strings.TrimSpace(stderr.String()), Args: args, Cause: err}
	}
	return stdout.Bytes(), nil
}

func baseArgs() []string {
	return []string{"-hide_banner", "-loglevel", "error", "-print_format", "json", "-show_error"}
}

func (p *Prober) buildArgs(c *config, input string) []string {
	args := baseArgs()
	if len(c.sections) == 0 {
		args = append(args, "-show_format", "-show_streams")
	} else {
		args = append(args, c.sections...)
	}
	args = append(args, c.modifiers...)
	args = append(args, c.extra...)
	args = append(args, c.inputOptions...)
	args = append(args, "-i", input)
	return args
}

func (p *Prober) command(ctx context.Context, args []string, c *config) *exec.Cmd {
	cmd := exec.CommandContext(ctx, p.binary, args...)
	if p.env != nil {
		cmd.Env = p.env
	}
	if c.stdin != nil {
		cmd.Stdin = c.stdin
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

func (p *Prober) stderrWriter(buf *bytes.Buffer) io.Writer {
	if p.stderr != nil {
		return io.MultiWriter(buf, p.stderr)
	}
	return buf
}

// errorSection extracts the "error" object from ffprobe output, if any.
func errorSection(out []byte) *ProbeError {
	if !bytes.Contains(out, []byte(`"error"`)) {
		return nil
	}
	var res struct {
		Error *ProbeError `json:"error"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil
	}
	return res.Error
}
