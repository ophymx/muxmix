package hwaccel

import (
	"context"
	"fmt"
	"strings"
	"time"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
)

// Detect queries the build's capabilities with caps.Detect, then probes
// every registered backend (or those in options.Kinds) by running a tiny
// pipeline on each device candidate. It takes a second or so on a typical
// machine; use DetectCached to keep the result between runs.
func Detect(ctx context.Context, runner baseffmpeg.Runner, options ProbeOptions) (*System, error) {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}
	set, err := caps.Detect(ctx, runner)
	if err != nil {
		return nil, err
	}
	return DetectWithCaps(ctx, runner, set, options), nil
}

// DetectWithCaps probes the backends for a build whose capabilities are
// already known. Backends the build lacks are recorded as unavailable
// without running anything. set may be nil, in which case every backend
// is probed.
func DetectWithCaps(ctx context.Context, runner baseffmpeg.Runner, set *caps.Set, options ProbeOptions) *System {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}
	sys := &System{Caps: set, Probes: make(map[Kind][]ProbeResult), DetectedAt: time.Now().UTC()}
	for _, kind := range probeKinds(options.Kinds) {
		if set != nil && !set.HasHWAccel(string(kind)) {
			sys.Probes[kind] = []ProbeResult{{Kind: kind, Error: fmt.Sprintf("ffmpeg is not built with %s support", kind)}}
			continue
		}
		devices := probeDevices(kind, options.Devices)
		results := make([]ProbeResult, 0, len(devices))
		for _, device := range devices {
			results = append(results, Probe(ctx, runner, kind, device))
		}
		if len(results) == 0 {
			results = append(results, ProbeResult{Kind: kind, Error: fmt.Sprintf("no device candidates for %s", kind)})
		}
		sortProbes(results)
		sys.Probes[kind] = results
	}
	return sys
}

// Probe tries to initialise one backend on one device by running the
// backend's probe command.
func Probe(ctx context.Context, runner baseffmpeg.Runner, kind Kind, device string) ProbeResult {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}
	kind = NormalizeKind(string(kind))
	probe := ProbeResult{Kind: kind, Device: device}
	b, ok := Lookup(kind)
	if !ok {
		probe.Error = fmt.Sprintf("unknown hwaccel %q", kind)
		return probe
	}
	args, err := b.ProbeArgs(device)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	probe.Args = append([]string(nil), args...)

	result, runErr := runner.RunArgs(ctx, args)
	if runErr != nil {
		probe.Error = formatProbeError(runErr, result)
		return probe
	}
	probe.Available = true
	return probe
}

func formatProbeError(err error, result *baseffmpeg.Result) string {
	if result != nil {
		if stderr := strings.TrimSpace(string(result.Stderr)); stderr != "" {
			return stderr
		}
		if report := strings.TrimSpace(string(result.Report)); report != "" {
			return report
		}
	}
	if err != nil {
		return err.Error()
	}
	return "probe failed"
}

func probeKinds(kinds []Kind) []Kind {
	if len(kinds) == 0 {
		return Kinds()
	}
	seen := make(map[Kind]bool, len(kinds))
	resolved := make([]Kind, 0, len(kinds))
	for _, kind := range kinds {
		kind = NormalizeKind(string(kind))
		if kind == None || seen[kind] {
			continue
		}
		seen[kind] = true
		resolved = append(resolved, kind)
	}
	return resolved
}

func probeDevices(kind Kind, configured map[Kind][]string) []string {
	if configured != nil {
		if devices, ok := configured[kind]; ok {
			return append([]string(nil), devices...)
		}
	}
	return DefaultDevices(kind)
}

// DefaultDevices returns the backend's device candidates on this host.
func DefaultDevices(kind Kind) []string {
	b, ok := Lookup(kind)
	if !ok {
		return nil
	}
	return b.DefaultDevices()
}
