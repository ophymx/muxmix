package ffprobe

import "testing"

// TestProbeOptions tests the probe option functions
func TestProbeOptions(t *testing.T) {
	config := &probeConfig{}

	// Test that options modify the config correctly
	WithShowFormat()(config)
	if !config.showFormat {
		t.Error("WithShowFormat should set showFormat to true")
	}

	WithShowStreams()(config)
	if !config.showStreams {
		t.Error("WithShowStreams should set showStreams to true")
	}

	WithShowPackets()(config)
	if !config.showPackets {
		t.Error("WithShowPackets should set showPackets to true")
	}

	WithShowFrames()(config)
	if !config.showFrames {
		t.Error("WithShowFrames should set showFrames to true")
	}

	WithShowChapters()(config)
	if !config.showChapters {
		t.Error("WithShowChapters should set showChapters to true")
	}

	WithShowPrograms()(config)
	if !config.showPrograms {
		t.Error("WithShowPrograms should set showPrograms to true")
	}

	WithShowLibraryVersions()(config)
	if !config.showLibraryVersions {
		t.Error("WithShowLibraryVersions should set showLibraryVersions to true")
	}

	WithShowPixelFormats()(config)
	if !config.showPixelFormats {
		t.Error("WithShowPixelFormats should set showPixelFormats to true")
	}

	WithShowStreamGroups()(config)
	if !config.showStreamGroups {
		t.Error("WithShowStreamGroups should set showStreamGroups to true")
	}

	args := []string{"-custom", "arg"}
	WithAdditionalArgs(args...)(config)
	if len(config.additionalArgs) != len(args) {
		t.Error("WithAdditionalArgs should add arguments to config")
	}
	for i, arg := range args {
		if config.additionalArgs[i] != arg {
			t.Errorf("Expected arg %s at position %d, got %s", arg, i, config.additionalArgs[i])
		}
	}
}
