// Package ffmpeg runs ffmpeg with structured commands, live progress and
// useful errors.
//
//	cmd := ffmpeg.NewCommand().
//		Input("in.mkv").
//		Output("out.mp4", ffmpeg.VideoCodec("libx264"), ffmpeg.CRF(20), ffmpeg.AudioCodec("aac"))
//
//	res, err := ffmpeg.Run(ctx, cmd, ffmpeg.OnProgress(func(p ffmpeg.Progress) {
//		fmt.Printf("\r%s %.1fx", p.Time, p.Speed)
//	}))
//
// Progress comes from ffmpeg's -progress stream on a dedicated pipe, so it is
// independent of the log level and immune to stderr formatting changes. On
// platforms without inheritable pipes the stats line on stderr is parsed
// instead.
//
// Cancelling the context sends ffmpeg SIGINT first, which lets it finish
// writing the container trailer, and kills it only if it has not exited
// within the grace period.
//
// Two kinds of options appear throughout: a RunnerOption (WithBinary,
// WithStderr, ...) configures a Runner once, and a RunOption (OnProgress,
// Stderr, Env, ...) applies to a single run. Command options are the Opt
// values in command.go.
//
// Numeric fields that ffmpeg did not report hold -1 (Progress.Speed,
// Progress.Fraction, VersionInfo.Patch); a duration it did not report is
// also -1. Values that can legitimately be negative, such as loudness in
// dB, use NaN instead; see package analyze.
package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var (
	// ErrFFmpegNotFound indicates ffmpeg is not installed or not in PATH.
	ErrFFmpegNotFound = errors.New("ffmpeg not found")
)

// Runner executes ffmpeg.
type Runner interface {
	// Run executes a Command with the runner's defaults applied
	// (-hide_banner, -nostdin, -loglevel error and -y unless the command
	// sets them). On failure the error is an *Error wrapping the cause,
	// and the Result is still returned whenever ffmpeg was started, so
	// its log and progress can be inspected; it is nil only when the
	// command failed validation or could not be launched.
	Run(ctx context.Context, cmd *Command, opts ...RunOption) (*Result, error)
	// RunArgs executes ffmpeg with exactly the given arguments.
	RunArgs(ctx context.Context, args []string, opts ...RunOption) (*Result, error)
	// Version reports the binary's version.
	Version(ctx context.Context) (*VersionInfo, error)
	// ValidateInstall reports ErrFFmpegNotFound when the binary is missing.
	ValidateInstall() error
	// Binary returns the executable path.
	Binary() string
}

// Result describes one ffmpeg execution.
type Result struct {
	Binary   string
	Args     []string
	ExitCode int

	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration

	// Stdout holds captured standard output unless Stdout was redirected.
	// It is not capped: redirect it with the Stdout option when the output
	// URL is pipe:1 and the payload is large or endless.
	Stdout []byte
	// Stderr holds ffmpeg's log output, the last CaptureLimit bytes of it.
	Stderr []byte
	// StderrDropped counts log bytes discarded to stay within that limit.
	// It is zero for every run that stayed under it.
	StderrDropped int64
	// Progress is the last progress update received.
	Progress Progress

	// ReportPath and Report are set when a report file was requested.
	ReportPath string
	Report     []byte
}

// Error is returned when ffmpeg fails to start or exits unsuccessfully.
type Error struct {
	Cause  error
	Result *Result
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	msg := "ffmpeg failed"
	if e.Result != nil && e.Result.ExitCode != 0 {
		msg = fmt.Sprintf("ffmpeg exited with code %d", e.Result.ExitCode)
	}
	if e.Result != nil {
		if tail := e.Result.errorTail(); tail != "" {
			return msg + ": " + tail
		}
	}
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Cause }

// LogLines returns stderr split into lines, without progress stats lines
// and without the trailing "Conversion failed!" summary.
func (r *Result) LogLines() []string {
	raw := strings.FieldsFunc(string(r.Stderr), func(c rune) bool { return c == '\n' || c == '\r' })
	var lines []string
	for _, l := range raw {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "frame=") || strings.HasPrefix(l, "size=") || l == "Conversion failed!" {
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

// LastLogLine returns the last meaningful line of stderr, which is normally
// ffmpeg's error message.
func (r *Result) LastLogLine() string {
	lines := r.LogLines()
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// errorTail returns the trailing run of error lines (at most two), since
// newer ffmpeg prints a specific line followed by a generic summary.
func (r *Result) errorTail() string {
	lines := r.LogLines()
	if len(lines) == 0 {
		return ""
	}
	tail := []string{lines[len(lines)-1]}
	if len(lines) > 1 {
		prev := lines[len(lines)-2]
		if strings.HasPrefix(strings.ToLower(prev), "error") || strings.HasPrefix(prev, "[") {
			tail = []string{prev, tail[0]}
		}
	}
	return strings.Join(tail, "; ")
}

// ─── runner construction ───────────────────────────────────────────────────

// RunnerOption configures a Runner at construction; see RunOption for
// per-run settings.
type RunnerOption func(*runner)

type runner struct {
	binary string
	env    []string
	stdout io.Writer
	stderr io.Writer
	grace  time.Duration
}

// DefaultRunner is the shared runner used by the package-level functions.
var DefaultRunner Runner = New()

// New creates a Runner.
func New(options ...RunnerOption) Runner {
	r := &runner{binary: "ffmpeg", grace: 5 * time.Second}
	for _, opt := range options {
		opt(r)
	}
	return r
}

// WithBinary sets the ffmpeg executable path.
func WithBinary(binary string) RunnerOption {
	return func(r *runner) {
		if binary != "" {
			r.binary = binary
		}
	}
}

// WithEnv replaces the process environment for every run: ffmpeg sees
// exactly env, not os.Environ. To add variables on top of the inherited
// environment use the per-run Env option instead.
func WithEnv(env ...string) RunnerOption {
	return func(r *runner) { r.env = append([]string(nil), env...) }
}

// WithStdout forwards ffmpeg's stdout to w on every run, in addition to
// capturing it.
func WithStdout(w io.Writer) RunnerOption {
	return func(r *runner) { r.stdout = w }
}

// WithStderr forwards ffmpeg's log output to w on every run, in addition to
// capturing it.
func WithStderr(w io.Writer) RunnerOption {
	return func(r *runner) { r.stderr = w }
}

// WithGrace sets how long a cancelled ffmpeg gets to exit after SIGINT
// before it is killed (default 5s).
func WithGrace(d time.Duration) RunnerOption {
	return func(r *runner) { r.grace = d }
}

// ─── run options ───────────────────────────────────────────────────────────

// RunOption configures a single run.
type RunOption func(*runConfig)

type runConfig struct {
	onProgress     func(Progress)
	stdin          io.Reader
	stdout         io.Writer
	stderr         io.Writer
	dir            string
	env            []string
	report         bool
	reportPath     string
	interval       time.Duration
	total          time.Duration
	noProgressPipe bool
	noDefaults     bool
	captureLimit   int
	// leading counts the default global args Run put in front of the
	// command, so run can slot the progress options right after them.
	leading int
}

// OnProgress receives progress updates while ffmpeg runs. It is called from
// a separate goroutine; the final update has Done set.
func OnProgress(fn func(Progress)) RunOption {
	return func(c *runConfig) { c.onProgress = fn }
}

// Stdin connects r to ffmpeg's standard input, for inputs read from pipe:0.
func Stdin(r io.Reader) RunOption {
	return func(c *runConfig) { c.stdin = r }
}

// Stdout sends ffmpeg's standard output to w instead of capturing it. Use it
// when an output URL is pipe:1.
func Stdout(w io.Writer) RunOption {
	return func(c *runConfig) { c.stdout = w }
}

// Stderr additionally forwards ffmpeg's log output to w for this run.
func Stderr(w io.Writer) RunOption {
	return func(c *runConfig) { c.stderr = w }
}

// Dir sets the working directory for the run.
func Dir(dir string) RunOption {
	return func(c *runConfig) { c.dir = dir }
}

// Env appends environment variables ("KEY=value") to the runner's
// environment for this run.
func Env(kv ...string) RunOption {
	return func(c *runConfig) { c.env = append(c.env, kv...) }
}

// Report asks ffmpeg for a full log via FFREPORT. With an empty path a
// temporary file is used; its path is returned in Result.ReportPath and its
// contents in Result.Report. The caller removes the file.
func Report(path string) RunOption {
	return func(c *runConfig) { c.report = true; c.reportPath = path }
}

// TotalDuration tells the run how long the output will be, so every
// Progress carries a Fraction and ETA. Use the input's duration (from
// ffprobe) adjusted for any -ss/-t trimming.
func TotalDuration(d time.Duration) RunOption {
	return func(c *runConfig) { c.total = d }
}

// ProgressInterval sets how often progress is reported (-stats_period,
// default 0.5s).
func ProgressInterval(d time.Duration) RunOption {
	return func(c *runConfig) { c.interval = d }
}

// NoProgressPipe forces progress parsing from the stderr stats line instead
// of a dedicated pipe.
func NoProgressPipe() RunOption {
	return func(c *runConfig) { c.noProgressPipe = true }
}

// NoDefaultArgs runs the Command exactly as rendered, dropping the whole
// set of runner defaults: the unconditional -hide_banner and -nostdin, the
// -loglevel error added unless the command sets -loglevel or -v, and the -y
// added unless it sets -y or -n. It is all or nothing — pass it when the
// command already carries its own equivalents, and note that losing
// -loglevel error is the one that changes what ffmpeg writes to the log
// Result.Stderr captures.
func NoDefaultArgs() RunOption {
	return func(c *runConfig) { c.noDefaults = true }
}

// CaptureLimit caps how many bytes of ffmpeg's log Result.Stderr keeps
// (default DefaultCaptureLimit). Past the limit the oldest bytes are
// dropped and Result.StderrDropped counts them; the error message and the
// closing log lines, which sit at the end, always survive. Pass 0 to keep
// everything, as runs did before the limit existed.
//
// The Stderr and WithStderr writers are unaffected: they still see every
// byte as it arrives, which is where an endless run should send its log.
func CaptureLimit(n int) RunOption {
	return func(c *runConfig) { c.captureLimit = n }
}

// ─── package-level helpers ─────────────────────────────────────────────────

// Run executes cmd on the default runner.
func Run(ctx context.Context, cmd *Command, opts ...RunOption) (*Result, error) {
	return DefaultRunner.Run(ctx, cmd, opts...)
}

// RunArgs executes raw arguments on the default runner.
func RunArgs(ctx context.Context, args []string, opts ...RunOption) (*Result, error) {
	return DefaultRunner.RunArgs(ctx, args, opts...)
}

// ValidateInstall checks the default runner's binary.
func ValidateInstall() error { return DefaultRunner.ValidateInstall() }

// ─── execution ─────────────────────────────────────────────────────────────

func (r *runner) Binary() string { return r.binary }

func (r *runner) ValidateInstall() error {
	if _, err := exec.LookPath(r.binary); err != nil {
		return ErrFFmpegNotFound
	}
	return nil
}

// newRunConfig applies opts over the per-run defaults.
func newRunConfig(opts []RunOption) runConfig {
	c := runConfig{captureLimit: DefaultCaptureLimit}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func (r *runner) Run(ctx context.Context, cmd *Command, opts ...RunOption) (*Result, error) {
	if err := cmd.Validate(); err != nil {
		return nil, err
	}
	c := newRunConfig(opts)
	var args []string
	if !c.noDefaults {
		args = append(args, "-hide_banner", "-nostdin")
		if !cmd.Global.Has("loglevel") && !cmd.Global.Has("v") {
			args = append(args, "-loglevel", "error")
		}
		if !cmd.Global.Has("y") && !cmd.Global.Has("n") {
			args = append(args, "-y")
		}
	}
	c.leading = len(args)
	args = append(args, cmd.Args()...)
	return r.run(ctx, args, &c)
}

func (r *runner) RunArgs(ctx context.Context, args []string, opts ...RunOption) (*Result, error) {
	c := newRunConfig(opts)
	return r.run(ctx, append([]string(nil), args...), &c)
}

func (r *runner) run(ctx context.Context, args []string, c *runConfig) (*Result, error) {
	res := &Result{Binary: r.binary, StartedAt: time.Now()}
	finish := func(err error) (*Result, error) {
		res.FinishedAt = time.Now()
		res.Duration = res.FinishedAt.Sub(res.StartedAt)
		if err != nil {
			return res, &Error{Cause: err, Result: res}
		}
		return res, nil
	}

	// Progress transport: a dedicated pipe when supported, else -stats on stderr.
	var progressPipe *os.File // our read end
	var progressChild *os.File
	usePipe := c.onProgress != nil && !c.noProgressPipe && supportsExtraFiles()
	if c.onProgress != nil {
		var extra []string
		if c.interval > 0 {
			extra = append(extra, "-stats_period", formatSeconds(c.interval))
		}
		if usePipe {
			pr, pw, err := os.Pipe()
			if err != nil {
				return finish(err)
			}
			progressPipe, progressChild = pr, pw
			extra = append(extra, "-progress", "pipe:3")
		} else {
			extra = append(extra, "-stats")
		}
		// Global options go with the other globals, ahead of every input,
		// so Result.Args reads naturally.
		args = append(append(append([]string(nil), args[:c.leading]...), extra...), args[c.leading:]...)
	}
	res.Args = args

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Dir = c.dir
	cmd.Cancel = func() error { return interrupt(cmd.Process) }
	cmd.WaitDelay = r.grace
	if c.stdin != nil {
		cmd.Stdin = c.stdin
	}
	env := r.env
	if env == nil {
		env = os.Environ()
	}
	env = append(append([]string(nil), env...), c.env...)
	if c.report {
		path := c.reportPath
		if path == "" {
			f, err := os.CreateTemp("", "ffmpeg-report-*.log")
			if err != nil {
				return finish(err)
			}
			path = f.Name()
			f.Close()
		}
		res.ReportPath = path
		env = append(env, "FFREPORT=file="+escapeReportPath(path)+":level=32")
	}
	cmd.Env = env

	var stdoutBuf bytes.Buffer
	stderrBuf := capture{Limit: c.captureLimit}
	if c.stdout != nil {
		cmd.Stdout = c.stdout
	} else if r.stdout != nil {
		cmd.Stdout = io.MultiWriter(&stdoutBuf, r.stdout)
	} else {
		cmd.Stdout = &stdoutBuf
	}
	var stderrWriters []io.Writer
	stderrWriters = append(stderrWriters, &stderrBuf)
	if r.stderr != nil {
		stderrWriters = append(stderrWriters, r.stderr)
	}
	if c.stderr != nil {
		stderrWriters = append(stderrWriters, c.stderr)
	}
	stderrSink := io.MultiWriter(stderrWriters...)

	// Progress readers run in their own goroutine and signal on done.
	done := make(chan struct{})
	closeStats := func() {}
	deliver := func(p Progress) {
		p.Fraction, p.ETA = -1, -1
		if c.total > 0 {
			p.Fraction = min(float64(p.Time)/float64(c.total), 1)
			if p.Done {
				p.Fraction, p.ETA = 1, 0
			} else if p.Speed > 0 {
				p.ETA = time.Duration(float64(max(c.total-p.Time, 0)) / p.Speed)
			}
		}
		res.Progress = p
		if c.onProgress != nil {
			c.onProgress(p)
		}
	}
	switch {
	case usePipe:
		cmd.Stderr = stderrSink
		cmd.ExtraFiles = []*os.File{progressChild}
		go func() {
			defer close(done)
			pr := NewProgressReader(progressPipe)
			for {
				p, err := pr.Read()
				if err != nil {
					return
				}
				deliver(p)
			}
		}()
	case c.onProgress != nil:
		// An io.Pipe rather than cmd.StderrPipe: Wait closes a StderrPipe as
		// soon as the child exits, losing whatever the reader had not read
		// yet. With a plain writer Wait drains stderr first, honouring
		// WaitDelay if the child leaked the fd.
		pipe, statsW := io.Pipe()
		cmd.Stderr = statsW
		closeStats = func() { statsW.Close() }
		go func() {
			defer close(done)
			// Keep draining once parsing stops (a scanner error, say):
			// an unread io.Pipe would block the copy Wait is waiting on.
			defer io.Copy(stderrSink, pipe) //nolint:errcheck
			sr := NewStatsReader(io.TeeReader(pipe, stderrSink))
			for {
				p, err := sr.Read()
				if err != nil {
					return
				}
				deliver(p)
			}
		}()
	default:
		cmd.Stderr = stderrSink
		close(done)
	}

	if err := cmd.Start(); err != nil {
		if progressChild != nil {
			progressChild.Close()
			progressPipe.Close()
		}
		closeStats()
		<-done
		if errors.Is(err, exec.ErrNotFound) {
			return finish(ErrFFmpegNotFound)
		}
		return finish(err)
	}
	if progressChild != nil {
		progressChild.Close() // child holds its own copy
	}

	waitErr := cmd.Wait()
	// Wait has copied all of stderr into the pipe; closing it ends the read.
	closeStats()
	if progressPipe != nil {
		// Wait already reaped the child, so the pipe has hit EOF; make sure
		// the reader goroutine sees it even if ffmpeg leaked the fd.
		<-done
		progressPipe.Close()
	} else {
		<-done
	}

	res.Stdout = stdoutBuf.Bytes()
	res.Stderr = stderrBuf.Bytes()
	res.StderrDropped = stderrBuf.Dropped()
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	if res.ReportPath != "" {
		res.Report, _ = os.ReadFile(res.ReportPath)
	}
	if waitErr != nil {
		if ctx.Err() != nil {
			return finish(ctx.Err())
		}
		return finish(waitErr)
	}
	return finish(nil)
}

func formatSeconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}

// escapeReportPath escapes the characters FFREPORT treats specially.
func escapeReportPath(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `:`, `\:`)
	return r.Replace(p)
}
