package hwaccel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
)

// Detect queries the build's capabilities with caps.Detect, then probes
// every registered backend (or those in options.Kinds) by running a tiny
// pipeline on each device candidate, and on each device that works runs
// one frame through every encoder the backend maps. It takes a few
// seconds on a typical machine; use DetectCached to keep the result
// between runs.
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
			result := Probe(ctx, runner, kind, device)
			if result.Available && !options.NoEncoderProbe {
				result.Encoders = probeEncoders(ctx, runner, set, kind, device, options.Codecs)
			}
			results = append(results, result)
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

// maxEncoderProbes is how many encoder probes run at once. They are
// short, but they share one piece of silicon, so a handful in flight is
// as fast as this gets.
const maxEncoderProbes = 4

// probeEncoders runs every codec in codecs -- or every one the backend
// maps -- through ProbeEncoder on device, concurrently. Codecs the
// backend does not map, or whose encoder the build lacks, are left out:
// Select reports those from the build capabilities without running
// anything.
func probeEncoders(ctx context.Context, runner baseffmpeg.Runner, set *caps.Set, kind Kind, device string, codecs []string) []EncoderProbe {
	codecs = encoderProbeCodecs(set, kind, codecs)
	if len(codecs) == 0 {
		return nil
	}
	probes := make([]EncoderProbe, len(codecs))
	sem := make(chan struct{}, maxEncoderProbes)
	var wg sync.WaitGroup
	for i, codec := range codecs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			probes[i] = ProbeEncoder(ctx, runner, kind, device, codec)
		}()
	}
	wg.Wait()
	return probes
}

// encoderProbeCodecs is the codec list to probe for a backend: the
// requested ones or the backend's own, keeping those it maps to an
// encoder the build has.
func encoderProbeCodecs(set *caps.Set, kind Kind, requested []string) []string {
	candidates := requested
	if len(candidates) == 0 {
		candidates = BackendCodecs(kind)
	}
	var codecs []string
	seen := make(map[string]bool, len(candidates))
	for _, codec := range candidates {
		codec = normalizeCodec(codec)
		if codec == "" || seen[codec] {
			continue
		}
		encoder, err := VideoEncoder(kind, codec)
		if err != nil || (set != nil && !set.HasEncoder(encoder)) {
			continue
		}
		seen[codec] = true
		codecs = append(codecs, codec)
	}
	return codecs
}

// ProbeEncoder encodes one frame with the backend's encoder for codec on
// device, in each of the upload formats, 8-bit first. It answers the two
// questions the build capabilities cannot: whether this machine's
// hardware implements that codec, and whether it keeps a 10-bit source at
// 10 bits. A codec that fails in 8-bit is not tried deeper.
func ProbeEncoder(ctx context.Context, runner baseffmpeg.Runner, kind Kind, device, codec string) EncoderProbe {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}
	kind = NormalizeKind(string(kind))
	probe := EncoderProbe{Codec: normalizeCodec(codec)}
	encoder, err := VideoEncoder(kind, codec)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	probe.Encoder = encoder
	var help *caps.Help
	for _, format := range probeFormats {
		args, err := encoderProbeArgs(kind, device, encoder, format)
		if err != nil {
			probe.Error = err.Error()
			return probe
		}
		result, runErr := runner.RunArgs(ctx, args)
		if runErr != nil {
			if format == NV12 {
				probe.Error = formatProbeError(runErr, result)
				return probe
			}
			continue
		}
		if format != NV12 {
			if help == nil {
				help, _ = caps.EncoderHelp(ctx, runner, encoder)
			}
			if convertsFormat(help, format) {
				continue
			}
		}
		probe.Works = true
		probe.Formats = append(probe.Formats, format)
	}
	return probe
}

// convertsFormat reports whether ffmpeg would have to convert format
// before the encoder sees it, so a probe that succeeded in that format
// proves nothing about depth. ffmpeg negotiates an encoder that takes
// software frames down to one of its listed pixel formats without an
// error: h264_videotoolbox lists nv12 and yuv420p, and a p010 frame fed
// to it comes out 8-bit. An encoder that takes only hardware frames lists
// no nv12 and is left alone; its upload fails outright when the device
// cannot keep the depth. A nil help (the query failed) is not evidence
// either way.
func convertsFormat(help *caps.Help, format string) bool {
	if help == nil || !help.SupportsPixelFormat(NV12) {
		return false
	}
	switch format {
	case P010:
		return !help.SupportsPixelFormat("p010le") && !help.SupportsPixelFormat("p010be")
	}
	return !help.SupportsPixelFormat(format)
}

func formatProbeError(err error, result *baseffmpeg.Result) string {
	if result != nil {
		if stderr := trimDriverChatter(string(result.Stderr)); stderr != "" {
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

// trimDriverChatter drops the lines libva prints on every successful
// driver load. They are the first thing on stderr, so without this the
// one-line form of a probe failure reads "libva info: VA-API version
// 1.22.0" instead of what actually went wrong.
func trimDriverChatter(stderr string) string {
	lines := strings.Split(stderr, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "libva info:") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
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

// BackendCodecs lists the codecs a backend maps to an encoder, for
// Detect to probe. Backends that do not implement VideoCodecLister are
// probed for CommonCodecs.
func BackendCodecs(kind Kind) []string {
	b, ok := Lookup(kind)
	if !ok {
		return nil
	}
	if lister, ok := b.(VideoCodecLister); ok {
		return lister.VideoCodecs()
	}
	return append([]string(nil), CommonCodecs...)
}

// DefaultDevices returns the backend's device candidates on this host.
func DefaultDevices(kind Kind) []string {
	b, ok := Lookup(kind)
	if !ok {
		return nil
	}
	return b.DefaultDevices()
}
