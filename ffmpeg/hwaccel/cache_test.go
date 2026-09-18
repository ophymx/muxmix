package hwaccel

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hw", "cache.json")
	sys := handSystem()
	if err := sys.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DetectedAt.IsZero() || !got.Available(VAAPI) || got.Available(CUDA) || !got.Caps.HasEncoder("h264_vaapi") || got.Caps.Version.Version != "7.1.5" {
		t.Errorf("reloaded = %+v", got)
	}
	if !got.SupportsCodec(VAAPI, "h264") || got.SupportsCodec(VAAPI, "hevc") {
		t.Error("SupportsCodec after reload")
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected error for missing cache")
	}

	opts := ProbeOptions{Devices: map[Kind][]string{VAAPI: {"/dev/dri/renderD128"}, CUDA: {""}}}
	if err := got.stale("7.1.5", 0, opts); err != nil {
		t.Errorf("fresh cache reported stale: %v", err)
	}
	if err := got.stale("8.0", 0, opts); !errors.Is(err, ErrCacheStale) {
		t.Errorf("version change: %v", err)
	}
	if err := got.stale("7.1.5", time.Nanosecond, opts); !errors.Is(err, ErrCacheStale) {
		t.Errorf("age: %v", err)
	}
	moved := ProbeOptions{Devices: map[Kind][]string{VAAPI: {"/dev/dri/renderD129"}, CUDA: {""}}}
	if err := got.stale("7.1.5", 0, moved); !errors.Is(err, ErrCacheStale) {
		t.Errorf("device change: %v", err)
	}

	// A cache written before the encoders were probed does not know which
	// codecs the device really encodes, so it has to be redone.
	unprobed := *got
	unprobed.Probes = map[Kind][]ProbeResult{
		VAAPI: {{Kind: VAAPI, Device: "/dev/dri/renderD128", Available: true}},
		CUDA:  got.Probes[CUDA],
	}
	if err := unprobed.stale("7.1.5", 0, opts); !errors.Is(err, ErrCacheStale) {
		t.Errorf("cache without encoder probes: %v", err)
	}
	if err := unprobed.stale("7.1.5", 0, ProbeOptions{NoEncoderProbe: true, Devices: opts.Devices}); err != nil {
		t.Errorf("NoEncoderProbe must not care about encoder probes: %v", err)
	}

	// Nor does a cache that probed the codec but not its upload formats
	// still describe the host: PreserveDepth would read it as 8-bit only.
	noFormats := *got
	noFormats.Probes = map[Kind][]ProbeResult{
		VAAPI: {{Kind: VAAPI, Device: "/dev/dri/renderD128", Available: true,
			Encoders: []EncoderProbe{{Codec: "h264", Encoder: "h264_vaapi", Works: true}}}},
		CUDA: got.Probes[CUDA],
	}
	if err := noFormats.stale("7.1.5", 0, opts); !errors.Is(err, ErrCacheStale) {
		t.Errorf("cache without upload formats: %v", err)
	}
}

func TestDetectCached(t *testing.T) {
	fake := fakeFFmpeg(t, "vaapi", vaapiQSVEncoders, `
  *"vaapi=probe:/dev/fake"*) exit 0 ;;
  *"-c:v h264_vaapi"*) exit 0 ;;
`)
	runner := baseffmpeg.New(baseffmpeg.WithBinary(fake))
	path := filepath.Join(t.TempDir(), "cache.json")
	opts := ProbeOptions{Kinds: []Kind{VAAPI}, Devices: map[Kind][]string{VAAPI: {"/dev/fake"}}}
	ctx := context.Background()

	sys, fromCache, err := DetectCached(ctx, runner, path, time.Hour, opts)
	if err != nil || fromCache || !sys.Available(VAAPI) {
		t.Fatalf("first: %v %v %+v", err, fromCache, sys)
	}
	sys, fromCache, err = DetectCached(ctx, runner, path, time.Hour, opts)
	if err != nil || !fromCache || !sys.Available(VAAPI) {
		t.Fatalf("second: %v %v", err, fromCache)
	}
	opts.Devices[VAAPI] = []string{"/dev/other"}
	if _, fromCache, err = DetectCached(ctx, runner, path, time.Hour, opts); err != nil || fromCache {
		t.Fatalf("after device change: %v %v", err, fromCache)
	}
}
