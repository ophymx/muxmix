package hwaccel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

// ErrCacheStale is returned by DetectCached's loader when the saved
// System is for another ffmpeg version, older than maxAge, or probed a
// different set of devices than the host has now.
var ErrCacheStale = errors.New("hwaccel: cached detection is stale")

// Save writes the System as JSON, atomically.
func (s *System) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load reads a System saved with Save. It does not check staleness; see
// DetectCached.
func Load(path string) (*System, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s System
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("hwaccel: parse cache %s: %w", path, err)
	}
	return &s, nil
}

// DetectCached returns the System saved at path when it is still valid for
// this ffmpeg binary and host, and otherwise runs Detect and saves the
// result. fromCache reports which happened. A cache is stale when the
// ffmpeg version differs, when it is older than maxAge (0 disables the age
// check), or when the device candidates a backend would probe now differ
// from the ones it probed then, so a GPU appearing or disappearing is
// noticed. A cache that cannot be written is not an error: the fresh
// detection is still returned.
func DetectCached(ctx context.Context, runner baseffmpeg.Runner, path string, maxAge time.Duration, options ProbeOptions) (sys *System, fromCache bool, err error) {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}
	v, err := runner.Version(ctx)
	if err != nil {
		return nil, false, err
	}
	if cached, err := Load(path); err == nil && cached.stale(v.Version, maxAge, options) == nil {
		return cached, true, nil
	}
	sys, err = Detect(ctx, runner, options)
	if err != nil {
		return nil, false, err
	}
	_ = sys.Save(path)
	return sys, false, nil
}

// stale returns ErrCacheStale (wrapped with the reason) when the System
// no longer describes this binary and host.
func (s *System) stale(version string, maxAge time.Duration, options ProbeOptions) error {
	if s.Caps == nil || s.Caps.Version == nil || s.Caps.Version.Version != version {
		return fmt.Errorf("%w: ffmpeg version changed", ErrCacheStale)
	}
	if maxAge > 0 && time.Since(s.DetectedAt) > maxAge {
		return fmt.Errorf("%w: older than %s", ErrCacheStale, maxAge)
	}
	for _, kind := range probeKinds(options.Kinds) {
		if !s.Built(kind) {
			continue
		}
		var probed []string
		for _, p := range s.Probes[kind] {
			probed = append(probed, p.Device)
		}
		now := probeDevices(kind, options.Devices)
		slices.Sort(probed)
		slices.Sort(now)
		if !slices.Equal(probed, now) {
			return fmt.Errorf("%w: %s devices changed", ErrCacheStale, kind)
		}
	}
	return nil
}
