package ffprobe

import (
	"context"
	"sync"
	"testing"
)

func TestVersionAtLeast(t *testing.T) {
	for _, tc := range []struct {
		version              string
		major, minor, patch  int
		snapshot             bool
		atLeast71, atLeast80 bool
	}{
		{"7.1.5", 7, 1, 5, false, true, false},
		{"7.1.5-0+deb13u1", 7, 1, 5, false, true, false},
		{"n8.0", 8, 0, 0, false, true, true},
		{"n8.1.2", 8, 1, 2, false, true, true},
		{"6.1.1-3ubuntu5", 6, 1, 1, false, false, false},
		{"4.4.2-0ubuntu0.22.04.1", 4, 4, 2, false, false, false},
		{"7.0", 7, 0, 0, false, false, false},
		{"7.1", 7, 1, 0, false, true, false},
		{"N-118000-g8f5d6f3", 0, 0, 0, true, true, true},
		{"2024-07-01-git-abc123", 0, 0, 0, true, true, true},
		{"", 0, 0, 0, false, false, false},
		{"garbage", 0, 0, 0, false, false, false},
	} {
		v := &VersionInfo{Program: ProgramVersion{Version: tc.version}}
		maj, min, patch, ok := v.Release()
		if maj != tc.major || min != tc.minor || patch != tc.patch || ok != (!tc.snapshot && tc.major > 0) {
			t.Errorf("%q: Release() = %d.%d.%d %v", tc.version, maj, min, patch, ok)
		}
		if v.Snapshot() != tc.snapshot {
			t.Errorf("%q: Snapshot() = %v", tc.version, v.Snapshot())
		}
		if v.AtLeast(7, 1) != tc.atLeast71 || v.AtLeast(8, 0) != tc.atLeast80 {
			t.Errorf("%q: AtLeast(7,1)=%v AtLeast(8,0)=%v", tc.version, v.AtLeast(7, 1), v.AtLeast(8, 0))
		}
	}
	var nilInfo *VersionInfo
	if nilInfo.AtLeast(0, 0) || nilInfo.Snapshot() {
		t.Error("nil VersionInfo")
	}
}

func TestVersionCachedLive(t *testing.T) {
	requireFFprobe(t)
	ctx := context.Background()
	p := New()
	first, err := p.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AtLeast(4, 4) {
		t.Errorf("installed ffprobe %q not >= 4.4", first.Program.Version)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := p.Version(ctx)
			if err != nil || v != first {
				t.Errorf("cached Version() = %p %v, want %p", v, err, first)
			}
		}()
	}
	wg.Wait()

	// A failed run is not cached.
	bad := New(WithBinary("ffprobe-does-not-exist"))
	if _, err := bad.Version(ctx); err == nil || bad.version != nil {
		t.Errorf("failure cached: %v %v", err, bad.version)
	}
}
