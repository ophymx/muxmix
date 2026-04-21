// Package ffprobe provides a typed interface to FFprobe for analyzing multimedia files.
//
// This package generates Go types from the official FFprobe XSD schema and provides
// a convenient API for probing media files with structured, type-safe results.
//
// Basic usage:
//
//	result, err := ffprobe.Probe("/path/to/video.mp4")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	if result.Format != nil {
//		fmt.Printf("Duration: %f seconds\n", result.Format.Duration)
//	}
//
// Advanced usage with options:
//
//	result, err := ffprobe.ProbeWithOptions("/path/to/video.mp4",
//		ffprobe.WithShowFormat(),
//		ffprobe.WithShowStreams(),
//		ffprobe.WithShowChapters(),
//	)
package ffprobe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var (
	// ErrUnsupportedFile indicates that the file format is not supported by ffprobe
	ErrUnsupportedFile = errors.New("unsupported file")

	// ErrFFProbeNotFound indicates that ffprobe is not installed or not in PATH
	ErrFFProbeNotFound = errors.New("ffprobe not found")
)

// IsUnsupported returns true if the error indicates an unsupported file format
func IsUnsupported(err error) bool {
	return errors.Is(err, ErrUnsupportedFile)
}

// Prober interface for ffprobe functionality
type Prober interface {
	Probe(path string) (*FfprobeType, error)
	ProbeWithOptions(path string, options ...ProbeOption) (*FfprobeType, error)
	ValidateInstall() error
}

// ProbeOption represents an option for the ffprobe command
type ProbeOption func(*probeConfig)

type probeConfig struct {
	showFormat          bool
	showStreams         bool
	showPackets         bool
	showFrames          bool
	showChapters        bool
	showPrograms        bool
	showLibraryVersions bool
	showPixelFormats    bool
	showStreamGroups    bool
	additionalArgs      []string
}

// WithShowFormat enables format information in the probe output
func WithShowFormat() ProbeOption {
	return func(c *probeConfig) {
		c.showFormat = true
	}
}

// WithShowStreams enables stream information in the probe output
func WithShowStreams() ProbeOption {
	return func(c *probeConfig) {
		c.showStreams = true
	}
}

// WithShowPackets enables packet information in the probe output
func WithShowPackets() ProbeOption {
	return func(c *probeConfig) {
		c.showPackets = true
	}
}

// WithShowFrames enables frame information in the probe output
func WithShowFrames() ProbeOption {
	return func(c *probeConfig) {
		c.showFrames = true
	}
}

// WithShowChapters enables chapter information in the probe output
func WithShowChapters() ProbeOption {
	return func(c *probeConfig) {
		c.showChapters = true
	}
}

// WithShowPrograms enables program information in the probe output
func WithShowPrograms() ProbeOption {
	return func(c *probeConfig) {
		c.showPrograms = true
	}
}

// WithShowLibraryVersions enables library version information in the probe output
func WithShowLibraryVersions() ProbeOption {
	return func(c *probeConfig) {
		c.showLibraryVersions = true
	}
}

// WithShowPixelFormats enables pixel format information in the probe output
func WithShowPixelFormats() ProbeOption {
	return func(c *probeConfig) {
		c.showPixelFormats = true
	}
}

// WithShowStreamGroups enables stream group information in the probe output
func WithShowStreamGroups() ProbeOption {
	return func(c *probeConfig) {
		c.showStreamGroups = true
	}
}

// WithAdditionalArgs adds additional command line arguments to ffprobe
func WithAdditionalArgs(args ...string) ProbeOption {
	return func(c *probeConfig) {
		c.additionalArgs = append(c.additionalArgs, args...)
	}
}

type ffprober struct{}

// DefaultProber is the default ffprobe implementation
var DefaultProber Prober = ffprober{}

// Probe analyzes the media file at the given path and returns ffprobe information.
// This is a convenience function that uses the default prober with format and streams enabled.
func Probe(path string) (*FfprobeType, error) {
	return DefaultProber.Probe(path)
}

// ProbeWithOptions analyzes the media file with custom options.
// This is a convenience function that uses the default prober.
func ProbeWithOptions(path string, options ...ProbeOption) (*FfprobeType, error) {
	return DefaultProber.ProbeWithOptions(path, options...)
}

// ValidateInstall checks if ffprobe is available in the system PATH
func ValidateInstall() error {
	return DefaultProber.ValidateInstall()
}

// Probe analyzes the media file at the given path and returns ffprobe information.
// Uses default options: show format and streams.
func (f ffprober) Probe(path string) (*FfprobeType, error) {
	return f.ProbeWithOptions(path, WithShowFormat(), WithShowStreams())
}

// ProbeWithOptions analyzes the media file with custom ffprobe options
func (f ffprober) ProbeWithOptions(path string, options ...ProbeOption) (*FfprobeType, error) {
	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("file access error: %w", err)
	}

	// Configure probe options
	config := &probeConfig{}
	for _, opt := range options {
		opt(config)
	}

	// Build ffprobe command
	args := []string{
		"-hide_banner",
		"-v", "quiet",
		"-print_format", "json=compact=1",
	}

	// Add show options
	if config.showFormat {
		args = append(args, "-show_format")
	}
	if config.showStreams {
		args = append(args, "-show_streams")
	}
	if config.showPackets {
		args = append(args, "-show_packets")
	}
	if config.showFrames {
		args = append(args, "-show_frames")
	}
	if config.showChapters {
		args = append(args, "-show_chapters")
	}
	if config.showPrograms {
		args = append(args, "-show_programs")
	}
	if config.showLibraryVersions {
		args = append(args, "-show_library_versions")
	}
	if config.showPixelFormats {
		args = append(args, "-show_pixel_formats")
	}
	if config.showStreamGroups {
		args = append(args, "-show_stream_groups")
	}

	// Add additional arguments
	args = append(args, config.additionalArgs...)

	// Add the file path
	args = append(args, path)

	// Execute ffprobe command
	cmd := exec.Command("ffprobe", args...)
	var outbuf, errbuf bytes.Buffer
	cmd.Stdout = &outbuf
	cmd.Stderr = &errbuf

	if err := cmd.Run(); err != nil {
		// Check for empty JSON output (indicates unsupported file)
		if strings.TrimSpace(outbuf.String()) == "{}" && cmd.ProcessState.ExitCode() == 1 {
			return nil, ErrUnsupportedFile
		}
		return nil, fmt.Errorf("ffprobe failed (exit code %d): stdout: %s, stderr: %s, error: %w",
			cmd.ProcessState.ExitCode(), outbuf.String(), errbuf.String(), err)
	}

	// Parse JSON output
	var result FfprobeType
	if err := json.Unmarshal(outbuf.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe JSON output: %w", err)
	}

	return &result, nil
}

// ValidateInstall checks if ffprobe is available in the system PATH
func (f ffprober) ValidateInstall() error {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		return ErrFFProbeNotFound
	}
	return nil
}
