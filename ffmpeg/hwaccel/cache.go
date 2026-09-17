package hwaccel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

// Cache is the on-disk form of a DetectSystem result. It is keyed by the
// ffmpeg version string so a binary upgrade invalidates it.
type Cache struct {
	FFmpegVersion string        `json:"ffmpeg_version"`
	DetectedAt    time.Time     `json:"detected_at"`
	Support       SystemSupport `json:"support"`
}

// ErrCacheStale is returned by LoadCache when the cached ffmpeg version
// does not match the one requested.
var ErrCacheStale = errors.New("hwaccel: cache is for a different ffmpeg version")

// SaveCache writes the detection result for the given ffmpeg version.
func SaveCache(path string, version string, support SystemSupport) error {
	c := Cache{FFmpegVersion: version, DetectedAt: time.Now().UTC(), Support: support}
	b, err := json.MarshalIndent(c, "", "  ")
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

// LoadCache reads a cache file. When version is not empty and differs from
// the cached one, the cache is returned along with ErrCacheStale.
func LoadCache(path string, version string) (*Cache, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Cache
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("hwaccel: parse cache %s: %w", path, err)
	}
	if version != "" && c.FFmpegVersion != version {
		return &c, ErrCacheStale
	}
	return &c, nil
}

// DetectSystemCached returns the cached detection for the runner's ffmpeg
// version when path holds one, and otherwise runs DetectSystem and saves
// the result. fromCache reports which happened. A cache that cannot be
// written is not an error: the fresh detection is still returned.
func DetectSystemCached(ctx context.Context, runner baseffmpeg.Runner, path string, options ProbeOptions) (support SystemSupport, fromCache bool, err error) {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}
	v, err := runner.Version(ctx)
	if err != nil {
		return SystemSupport{}, false, err
	}
	if c, err := LoadCache(path, v.Version); err == nil {
		return c.Support, true, nil
	}
	support, err = DetectSystem(ctx, runner, options)
	if err != nil {
		return SystemSupport{}, false, err
	}
	_ = SaveCache(path, v.Version, support)
	return support, false, nil
}
