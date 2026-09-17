package hwaccel

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hw", "cache.json")
	support := SystemSupport{
		Built: Support{Accels: map[Kind]bool{VAAPI: true}, Encoders: map[string]bool{"h264_vaapi": true}},
		Probes: map[Kind][]ProbeResult{
			VAAPI: {{Kind: VAAPI, Device: "/dev/dri/renderD128", Available: true, Args: []string{"-x"}}},
			CUDA:  {{Kind: CUDA, Error: "no device"}},
		},
	}
	if err := SaveCache(path, "7.1.5", support); err != nil {
		t.Fatal(err)
	}
	c, err := LoadCache(path, "7.1.5")
	if err != nil {
		t.Fatal(err)
	}
	if c.FFmpegVersion != "7.1.5" || c.DetectedAt.IsZero() || !c.Support.Available(VAAPI) || c.Support.Available(CUDA) {
		t.Errorf("cache = %+v", c)
	}
	if dev, ok := c.Support.Device(VAAPI); !ok || dev != "/dev/dri/renderD128" {
		t.Errorf("device = %q %v", dev, ok)
	}
	if !c.Support.SupportsCodec(VAAPI, "h264") {
		t.Error("SupportsCodec after reload")
	}
	if _, err := LoadCache(path, "8.0"); !errors.Is(err, ErrCacheStale) {
		t.Errorf("stale = %v", err)
	}
	if _, err := LoadCache(filepath.Join(t.TempDir(), "missing.json"), ""); err == nil {
		t.Error("expected error for missing cache")
	}
}

type fakeBackend struct{}

func (fakeBackend) Kind() Kind                           { return "topaz" }
func (fakeBackend) DefaultDevices() []string             { return []string{"gpu0"} }
func (fakeBackend) ProbeArgs(d string) ([]string, error) { return []string{"-probe", d}, nil }
func (fakeBackend) InputArgs(d string) ([]string, error) { return []string{"-topaz_device", d}, nil }
func (fakeBackend) Filter(extra ...string) (string, error) {
	return joinFilters(extra, "topazupload"), nil
}
func (fakeBackend) VideoCodec(codec string) (string, error) {
	return codecTable{"h264": "h264_topaz"}.encoder(codec)
}

func TestRegisterBackend(t *testing.T) {
	Register(fakeBackend{})
	RegisterAlias("tpz", "topaz")
	if NormalizeKind("TPZ") != "topaz" || NormalizeKind("topaz") != "topaz" || NormalizeKind("unknown") != None {
		t.Error("NormalizeKind")
	}
	if !slices.Contains(Kinds(), Kind("topaz")) {
		t.Errorf("Kinds = %v", Kinds())
	}
	args, err := BuildEncodeArgs("topaz", "h264", "gpu0", "scale=1280:-2")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-topaz_device", "gpu0", "-vf", "scale=1280:-2,topazupload", "-c:v", "h264_topaz"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v", args)
	}
	if DefaultDevices("topaz")[0] != "gpu0" {
		t.Error("DefaultDevices")
	}
	if p, err := DefaultProbeArgs("topaz", "gpu0"); err != nil || p[1] != "gpu0" {
		t.Errorf("probe args = %v %v", p, err)
	}
	defer func() {
		if recover() == nil {
			t.Error("duplicate Register should panic")
		}
	}()
	Register(fakeBackend{})
}

func TestNoneAndAuto(t *testing.T) {
	if args, err := BuildInputArgs(None, ""); err != nil || args != nil {
		t.Errorf("None input = %v %v", args, err)
	}
	if f, err := BuildFilter(None, "scale=1:1", "", "fps=30"); err != nil || f != "scale=1:1,fps=30" {
		t.Errorf("None filter = %q %v", f, err)
	}
	if c, err := BuildVideoCodec(None, "H265"); err != nil || c != "hevc" {
		t.Errorf("None codec = %q %v", c, err)
	}
	if _, err := BuildVideoCodec(Auto, "h264"); err == nil {
		t.Error("Auto should error")
	}
	if _, err := BuildVideoCodec("nope", "h264"); err == nil {
		t.Error("unknown kind should error")
	}
}
