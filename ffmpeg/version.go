package ffmpeg

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// VersionInfo is the parsed output of ffmpeg -version.
type VersionInfo struct {
	Version       string   // e.g. "7.1.5-0+deb13u1" or "n8.0"
	Major, Minor  int      // numeric release line, 0/0 when the version is a git hash
	Built         string   // "built with gcc 14 (Debian 14.2.0-19)"
	Configuration []string // each --flag from the configuration line
	Libraries     []LibraryVersion
}

// LibraryVersion is one "libavcodec 61. 19.101 / 61. 19.101" line.
type LibraryVersion struct {
	Name    string
	Version string // compiled version, dots without padding: "61.19.101"
	Runtime string // linked version
}

// AtLeast reports whether the release line is major.minor or newer.
func (v *VersionInfo) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

// Enabled reports whether a configure flag such as "libx264" (--enable-libx264)
// or "gpl" is present.
func (v *VersionInfo) Enabled(feature string) bool {
	want := "--enable-" + feature
	for _, c := range v.Configuration {
		if c == want {
			return true
		}
	}
	return false
}

// Library returns the version of the named library ("libavcodec"), or "".
func (v *VersionInfo) Library(name string) string {
	for _, l := range v.Libraries {
		if l.Name == name {
			return l.Version
		}
	}
	return ""
}

var (
	versionLine = regexp.MustCompile(`^ffmpeg version (\S+)`)
	versionNums = regexp.MustCompile(`^[nN]?(\d+)\.(\d+)`)
	libraryLine = regexp.MustCompile(`^(lib\w+)\s+(\d+)\.\s*(\d+)\.\s*(\d+)\s*/\s*(\d+)\.\s*(\d+)\.\s*(\d+)`)
)

// ParseVersion parses the text of ffmpeg -version.
func ParseVersion(output string) (*VersionInfo, error) {
	v := &VersionInfo{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case v.Version == "" && versionLine.MatchString(line):
			v.Version = versionLine.FindStringSubmatch(line)[1]
			if m := versionNums.FindStringSubmatch(v.Version); m != nil {
				v.Major, _ = strconv.Atoi(m[1])
				v.Minor, _ = strconv.Atoi(m[2])
			}
		case strings.HasPrefix(line, "built with "):
			v.Built = strings.TrimPrefix(line, "built with ")
		case strings.HasPrefix(line, "configuration:"):
			v.Configuration = strings.Fields(strings.TrimPrefix(line, "configuration:"))
		default:
			if m := libraryLine.FindStringSubmatch(line); m != nil {
				v.Libraries = append(v.Libraries, LibraryVersion{
					Name:    m[1],
					Version: m[2] + "." + m[3] + "." + m[4],
					Runtime: m[5] + "." + m[6] + "." + m[7],
				})
			}
		}
	}
	if v.Version == "" {
		return nil, fmt.Errorf("ffmpeg: no version line in output")
	}
	return v, nil
}

// Version runs ffmpeg -version on the runner's binary.
func (r *runner) Version(ctx context.Context) (*VersionInfo, error) {
	res, err := r.RunArgs(ctx, []string{"-version"})
	if err != nil {
		return nil, err
	}
	return ParseVersion(string(res.Stdout))
}

// Version runs ffmpeg -version on the default runner.
func Version(ctx context.Context) (*VersionInfo, error) {
	return DefaultRunner.Version(ctx)
}
