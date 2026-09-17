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

// RunOptions configures a single ffmpeg invocation.
type RunOptions struct {
	Args []string

	Dir string
	Env []string

	Stdout io.Writer
	Stderr io.Writer

	OnProgress OnProgressFunc

	DisableDefaultArgs bool
	KeepReportFile     bool
}

// Result describes one ffmpeg command execution.
type Result struct {
	Binary   string
	Args     []string
	ExitCode int

	StartAt  time.Time
	EndAt    time.Time
	Duration time.Duration

	ReportPath string
	Report     []byte

	Stdout   []byte
	Stderr   []byte
	LastStat ProgressStat
}

// Error wraps ffmpeg execution failures with context and report output.
type Error struct {
	Cause   error
	Result  *Result
	Message string
}

func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Message != "" {
		return err.Message
	}
	if err.Result != nil && err.Result.ReportPath != "" {
		return fmt.Sprintf("ffmpeg failed: %v (report: %s)", err.Cause, err.Result.ReportPath)
	}
	return fmt.Sprintf("ffmpeg failed: %v", err.Cause)
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// Runner provides a rich interface for executing ffmpeg commands.
type Runner interface {
	ValidateInstall() error
	Run(ctx context.Context, args ...string) (*Result, error)
	RunWithOptions(ctx context.Context, options RunOptions) (*Result, error)
	RunWithRawArgs(fn OnProgressFunc, args ...string) error
}

// Option configures an ffmpeg runner instance.
type Option func(*runner)

type runner struct {
	binary      string
	defaultArgs []string
	stdout      io.Writer
	stderr      io.Writer
	env         []string
}

// DefaultRunner is the shared default ffmpeg runner.
var DefaultRunner Runner = New()

// New creates a new ffmpeg runner with sensible defaults.
func New(options ...Option) Runner {
	r := &runner{
		binary:      "ffmpeg",
		defaultArgs: []string{"-hide_banner", "-v", "quiet", "-y", "-stats"},
		stdout:      os.Stdout,
		stderr:      io.Discard,
		env:         os.Environ(),
	}
	for _, opt := range options {
		opt(r)
	}
	return r
}

// WithBinary sets the ffmpeg executable path.
func WithBinary(binary string) Option {
	return func(r *runner) {
		if binary != "" {
			r.binary = binary
		}
	}
}

// WithDefaultArgs replaces the default arguments prepended to each run.
func WithDefaultArgs(args ...string) Option {
	return func(r *runner) {
		r.defaultArgs = append([]string(nil), args...)
	}
}

// WithStdout sets default stdout forwarding.
func WithStdout(w io.Writer) Option {
	return func(r *runner) {
		if w != nil {
			r.stdout = w
		}
	}
}

// WithStderr sets default stderr forwarding of raw ffmpeg output.
func WithStderr(w io.Writer) Option {
	return func(r *runner) {
		if w != nil {
			r.stderr = w
		}
	}
}

// WithEnv sets default command environment.
func WithEnv(env ...string) Option {
	return func(r *runner) {
		r.env = append([]string(nil), env...)
	}
}

func ValidateInstall() error {
	return DefaultRunner.ValidateInstall()
}

func Run(ctx context.Context, args ...string) (*Result, error) {
	return DefaultRunner.Run(ctx, args...)
}

func RunWithOptions(ctx context.Context, options RunOptions) (*Result, error) {
	return DefaultRunner.RunWithOptions(ctx, options)
}

func RunWithRawArgs(fn OnProgressFunc, args ...string) error {
	return DefaultRunner.RunWithRawArgs(fn, args...)
}

func (r *runner) ValidateInstall() error {
	if _, err := exec.LookPath(r.binary); err != nil {
		return ErrFFmpegNotFound
	}
	return nil
}

func (r *runner) Run(ctx context.Context, args ...string) (*Result, error) {
	return r.RunWithOptions(ctx, RunOptions{Args: args})
}

func (r *runner) RunWithRawArgs(fn OnProgressFunc, args ...string) error {
	_, err := r.RunWithOptions(context.Background(), RunOptions{
		Args:       args,
		OnProgress: fn,
	})
	return err
}

func (r *runner) RunWithOptions(ctx context.Context, options RunOptions) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	result := &Result{
		Binary:  r.binary,
		StartAt: time.Now(),
	}

	args := make([]string, 0, len(r.defaultArgs)+len(options.Args))
	if !options.DisableDefaultArgs {
		args = append(args, r.defaultArgs...)
	}
	args = append(args, options.Args...)
	result.Args = append([]string(nil), args...)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	if options.Dir != "" {
		cmd.Dir = options.Dir
	}

	stdoutBuf := &bytes.Buffer{}
	cmd.Stdout = io.MultiWriter(selectWriter(options.Stdout, r.stdout), stdoutBuf)

	pipe, err := cmd.StderrPipe()
	if err != nil {
		result.EndAt = time.Now()
		result.Duration = result.EndAt.Sub(result.StartAt)
		return result, &Error{Cause: err, Result: result}
	}

	reportPath, err := tempReportFile()
	if err != nil {
		result.EndAt = time.Now()
		result.Duration = result.EndAt.Sub(result.StartAt)
		return result, &Error{Cause: err, Result: result}
	}
	result.ReportPath = reportPath
	if !options.KeepReportFile {
		defer os.Remove(reportPath)
	}

	env := append([]string(nil), r.env...)
	env = append(env, options.Env...)
	env = append(env, fmt.Sprintf("FFREPORT=file=%s:level=32", reportPath))
	cmd.Env = env

	stderrBuf := &bytes.Buffer{}
	rawStderr := io.MultiWriter(stderrBuf, selectWriter(options.Stderr, r.stderr))
	parser := NewStatParser(io.TeeReader(pipe, rawStderr))

	if err := cmd.Start(); err != nil {
		result.EndAt = time.Now()
		result.Duration = result.EndAt.Sub(result.StartAt)
		return result, &Error{Cause: fmt.Errorf("failed to start ffmpeg: %w", err), Result: result}
	}

	if options.OnProgress != nil {
		options.OnProgress(ProgressStat{})
	}

	for {
		stat, readErr := parser.Read()
		if readErr == nil {
			result.LastStat = stat
			if options.OnProgress != nil {
				options.OnProgress(stat)
			}
			continue
		}
		if errors.Is(readErr, io.EOF) {
			break
		}

		_ = cmd.Wait()
		result.EndAt = time.Now()
		result.Duration = result.EndAt.Sub(result.StartAt)
		result.Stdout = stdoutBuf.Bytes()
		result.Stderr = stderrBuf.Bytes()
		result.Report = readReport(reportPath, result.Stderr)
		return result, &Error{Cause: readErr, Result: result}
	}

	waitErr := cmd.Wait()
	result.EndAt = time.Now()
	result.Duration = result.EndAt.Sub(result.StartAt)
	result.Stdout = stdoutBuf.Bytes()
	result.Stderr = stderrBuf.Bytes()
	result.Report = readReport(reportPath, result.Stderr)

	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if waitErr != nil {
		return result, &Error{Cause: waitErr, Result: result}
	}
	return result, nil
}

func selectWriter(preferred io.Writer, fallback io.Writer) io.Writer {
	if preferred != nil {
		return preferred
	}
	if fallback != nil {
		return fallback
	}
	return io.Discard
}

func tempReportFile() (string, error) {
	f, err := os.CreateTemp("", "ffmpeg-report-")
	if err != nil {
		return "", err
	}
	return f.Name(), f.Close()
}

func readReport(reportPath string, fallback []byte) []byte {
	b, err := os.ReadFile(reportPath)
	if err == nil && len(b) > 0 {
		return b
	}
	if len(fallback) == 0 {
		return nil
	}
	return []byte(strings.TrimSpace(string(fallback)))
}
