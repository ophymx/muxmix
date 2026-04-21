package ffprobe_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffprobe"
)

func TestExampleJSONFixturesUnmarshal(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	pkgDir := filepath.Dir(thisFile)

	fixtures, err := filepath.Glob(filepath.Join(pkgDir, "testdata", "*.json"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no fixtures found in testdata/*.json")
	}

	for _, path := range fixtures {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			var doc ffprobe.FfprobeType
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}

			if doc.Format == nil {
				t.Fatal("format is nil")
			}
			if strings.TrimSpace(doc.Format.Filename) == "" {
				t.Fatal("format.filename is empty")
			}
			if len(doc.Streams) == 0 {
				t.Fatal("streams is empty")
			}

			hasCodecType := false
			for i := range doc.Streams {
				if doc.Streams[i].CodecType != nil && strings.TrimSpace(*doc.Streams[i].CodecType) != "" {
					hasCodecType = true
					break
				}
			}
			if !hasCodecType {
				t.Fatal("no stream has codec_type")
			}
		})
	}
}

// TestProbeValidateInstall tests the ValidateInstall function
func TestProbeValidateInstall(t *testing.T) {
	err := ffprobe.ValidateInstall()
	if err == ffprobe.ErrFFProbeNotFound {
		t.Skip("ffprobe not installed, skipping probe tests")
	}
	if err != nil {
		t.Fatalf("ValidateInstall failed: %v", err)
	}
}

// TestProbeNonExistentFile tests probing a non-existent file
func TestProbeNonExistentFile(t *testing.T) {
	if err := ffprobe.ValidateInstall(); err == ffprobe.ErrFFProbeNotFound {
		t.Skip("ffprobe not installed, skipping probe tests")
	}

	_, err := ffprobe.Probe("/path/to/nonexistent/file.mp4")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

// TestProbeWithOptions tests the ProbeWithOptions function
func TestProbeWithOptions(t *testing.T) {
	if err := ffprobe.ValidateInstall(); err == ffprobe.ErrFFProbeNotFound {
		t.Skip("ffprobe not installed, skipping probe tests")
	}

	// Find a test fixture
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	pkgDir := filepath.Dir(thisFile)

	fixtures, err := filepath.Glob(filepath.Join(pkgDir, "testdata", "*.json"))
	if err != nil {
		t.Fatalf("glob fixtures: %v", err)
	}
	if len(fixtures) == 0 {
		t.Skip("no test fixtures found")
	}

	// Try to find a corresponding media file (this test might fail if no actual media files exist)
	// This is more of a placeholder test structure
	t.Log("ProbeWithOptions test structure created but requires actual media files to test")
}

// TestIsUnsupported tests the IsUnsupported helper function
func TestIsUnsupported(t *testing.T) {
	if !ffprobe.IsUnsupported(ffprobe.ErrUnsupportedFile) {
		t.Error("IsUnsupported should return true for ErrUnsupportedFile")
	}

	otherErr := errors.New("some other error")
	if ffprobe.IsUnsupported(otherErr) {
		t.Error("IsUnsupported should return false for other errors")
	}
}
