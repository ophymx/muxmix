package hwaccel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

func DetectSystem(ctx context.Context, runner baseffmpeg.Runner, options ProbeOptions) (SystemSupport, error) {
	built, err := Detect(ctx, runner)
	if err != nil {
		return SystemSupport{}, err
	}

	system := SystemSupport{
		Built:  built,
		Probes: make(map[Kind][]ProbeResult),
	}

	for _, kind := range probeKinds(options.Kinds) {
		if !built.Has(kind) {
			system.Probes[kind] = []ProbeResult{{
				Kind:  kind,
				Error: fmt.Sprintf("ffmpeg is not built with %s support", kind),
			}}
			continue
		}

		devices := probeDevices(kind, options.Devices)
		results := make([]ProbeResult, 0, len(devices))
		for _, device := range devices {
			probe := Probe(ctx, runner, kind, device)
			results = append(results, probe)
		}
		if len(results) == 0 {
			results = append(results, ProbeResult{
				Kind:  kind,
				Error: fmt.Sprintf("no probe candidates for %s", kind),
			})
		}
		SystemSortProbes(results)
		if len(results) > 0 {
			system.Probes[kind] = results
		}
	}

	return system, nil
}

func Probe(ctx context.Context, runner baseffmpeg.Runner, kind Kind, device string) ProbeResult {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}
	kind = NormalizeKind(string(kind))
	probe := ProbeResult{Kind: kind, Device: device}
	args, err := DefaultProbeArgs(kind, device)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	probe.Args = append([]string(nil), args...)

	result, runErr := runner.RunWithOptions(ctx, baseffmpeg.RunOptions{
		Args:               args,
		DisableDefaultArgs: true,
	})
	if runErr != nil {
		probe.Error = formatProbeError(runErr, result)
		return probe
	}
	probe.Available = true
	return probe
}

func DefaultProbeArgs(kind Kind, device string) ([]string, error) {
	kind = NormalizeKind(string(kind))
	base := []string{"-hide_banner", "-loglevel", "error"}
	switch kind {
	case None, Auto:
		return nil, fmt.Errorf("cannot probe hwaccel %q", kind)
	case VAAPI:
		if device == "" {
			return nil, fmt.Errorf("vaapi probe device is required")
		}
		return append(base,
			"-init_hw_device", fmt.Sprintf("vaapi=probe:%s", device),
			"-f", "lavfi",
			"-i", "color=s=16x16:d=0.1",
			"-vf", "format=nv12,hwupload",
			"-frames:v", "1",
			"-f", "null", "-",
		), nil
	case CUDA:
		deviceSpec := "cuda=probe"
		if device != "" {
			deviceSpec += ":" + device
		}
		return append(base,
			"-init_hw_device", deviceSpec,
			"-f", "lavfi",
			"-i", "color=s=16x16:d=0.1",
			"-vf", "hwupload_cuda",
			"-frames:v", "1",
			"-f", "null", "-",
		), nil
	case QSV:
		deviceSpec := "qsv=probe"
		if device != "" {
			deviceSpec += ":" + device
		}
		return append(base,
			"-init_hw_device", deviceSpec,
			"-f", "lavfi",
			"-i", "color=s=16x16:d=0.1",
			"-frames:v", "1",
			"-f", "null", "-",
		), nil
	case VideoToolbox:
		return append(base,
			"-init_hw_device", "videotoolbox=probe",
			"-f", "lavfi",
			"-i", "color=s=16x16:d=0.1",
			"-frames:v", "1",
			"-f", "null", "-",
		), nil
	default:
		return nil, fmt.Errorf("unsupported hwaccel %q", kind)
	}
}

func DefaultDevices(kind Kind) []string {
	kind = NormalizeKind(string(kind))
	switch kind {
	case VAAPI, QSV:
		if runtime.GOOS != "linux" {
			return nil
		}
		matches, err := filepath.Glob("/dev/dri/renderD*")
		if err != nil {
			return nil
		}
		if len(matches) == 0 {
			defaultDevice := defaultVAAPIDevice()
			if defaultDevice == "" {
				return nil
			}
			if _, err := os.Stat(defaultDevice); err == nil {
				return []string{defaultDevice}
			}
			return nil
		}
		sort.Strings(matches)
		return matches
	case CUDA, VideoToolbox:
		return []string{""}
	default:
		return nil
	}
}

func formatProbeError(err error, result *baseffmpeg.Result) string {
	if result != nil {
		stderr := strings.TrimSpace(string(result.Stderr))
		if stderr != "" {
			return stderr
		}
		report := strings.TrimSpace(string(result.Report))
		if report != "" {
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
		return []Kind{VAAPI, CUDA, QSV, VideoToolbox}
	}
	seen := make(map[Kind]bool, len(kinds))
	resolved := make([]Kind, 0, len(kinds))
	for _, kind := range kinds {
		kind = NormalizeKind(string(kind))
		if kind == None || kind == Auto || seen[kind] {
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
	devices := DefaultDevices(kind)
	if len(devices) == 0 {
		switch kind {
		case CUDA, VideoToolbox:
			return []string{""}
		default:
			return nil
		}
	}
	return devices
}

func SystemSortProbes(probes []ProbeResult) {
	sort.SliceStable(probes, func(i, j int) bool {
		if probes[i].Available != probes[j].Available {
			return probes[i].Available
		}
		return probes[i].Device < probes[j].Device
	})
}

func selectOrder(preferred []Kind) []Kind {
	if len(preferred) == 0 {
		return []Kind{VAAPI, CUDA, QSV, VideoToolbox}
	}
	seen := make(map[Kind]bool, len(preferred))
	order := make([]Kind, 0, len(preferred))
	for _, kind := range preferred {
		kind = NormalizeKind(string(kind))
		if kind == None || kind == Auto || seen[kind] {
			continue
		}
		seen[kind] = true
		order = append(order, kind)
	}
	return order
}

func defaultVAAPIDevice() string {
	if runtime.GOOS == "linux" {
		return "/dev/dri/renderD128"
	}
	return ""
}
