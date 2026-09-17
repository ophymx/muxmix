package ffprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// VersionInfo describes the ffprobe binary.
type VersionInfo struct {
	Program   ProgramVersion
	Libraries []*LibraryVersion
}

// Version runs ffprobe -show_program_version -show_library_versions. The
// result is cached for the life of the Prober, so gating options on it
// costs one process per Prober; only a successful run is cached.
func (p *Prober) Version(ctx context.Context) (*VersionInfo, error) {
	p.versionMu.Lock()
	defer p.versionMu.Unlock()
	if p.version != nil {
		return p.version, nil
	}
	raw, err := p.runBare(ctx, "-show_program_version", "-show_library_versions")
	if err != nil {
		return nil, err
	}
	var res Result
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, fmt.Errorf("ffprobe: decode version output: %w", err)
	}
	if res.ProgramVersion.Version == "" {
		return nil, fmt.Errorf("ffprobe: no program_version in output")
	}
	p.version = &VersionInfo{Program: res.ProgramVersion, Libraries: res.LibraryVersions}
	return p.version, nil
}

// AtLeast reports whether the binary is release major.minor or newer. It
// understands every spelling distributions use ("7.1.5", "n8.0",
// "6.1.1-3ubuntu5", "4.4.2-0ubuntu0.22.04.1") and treats git snapshots
// ("N-118000-g8f5d6f3", "2024-07-01-git-...") as newer than any release.
func (v *VersionInfo) AtLeast(major, minor int) bool {
	if v == nil {
		return false
	}
	maj, min, _, snapshot := parseVersion(v.Program.Version)
	if snapshot {
		return true
	}
	return maj > major || (maj == major && min >= minor)
}

// Snapshot reports whether the binary is a git build rather than a
// release, in which case AtLeast is always true.
func (v *VersionInfo) Snapshot() bool {
	if v == nil {
		return false
	}
	_, _, _, snapshot := parseVersion(v.Program.Version)
	return snapshot
}

// Release returns the major, minor and patch numbers of a release build
// (patch is 0 when the version has none). ok is false for git snapshots
// and anything unparseable.
func (v *VersionInfo) Release() (major, minor, patch int, ok bool) {
	if v == nil {
		return 0, 0, 0, false
	}
	major, minor, patch, snapshot := parseVersion(v.Program.Version)
	return major, minor, patch, !snapshot && major > 0
}

// parseVersion reads the leading major.minor[.patch] of an FFmpeg version
// string, tolerating an "n" tag prefix and any distribution suffix. A
// string that starts with "N-" (FFmpeg's own git description) or contains
// "git" is a snapshot.
func parseVersion(s string) (major, minor, patch int, snapshot bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "N-") || strings.Contains(s, "git") {
		return 0, 0, 0, true
	}
	s = strings.TrimPrefix(s, "n")
	var nums [3]int
	for i := range nums {
		j := 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == 0 {
			break
		}
		nums[i], _ = strconv.Atoi(s[:j])
		s = s[j:]
		if !strings.HasPrefix(s, ".") {
			break
		}
		s = s[1:]
	}
	return nums[0], nums[1], nums[2], false
}
