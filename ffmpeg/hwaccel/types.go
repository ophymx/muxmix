package hwaccel

import (
	"fmt"
	"slices"
	"strings"
)

type Kind string

const (
	None         Kind = ""
	Auto         Kind = "auto"
	VAAPI        Kind = "vaapi"
	CUDA         Kind = "cuda"
	QSV          Kind = "qsv"
	VideoToolbox Kind = "videotoolbox"
)

type Support struct {
	Accels   map[Kind]bool
	Encoders map[string]bool
}

type ProbeOptions struct {
	Kinds   []Kind
	Devices map[Kind][]string
}

type ProbeResult struct {
	Kind      Kind
	Device    string
	Args      []string
	Available bool
	Error     string
}

type SystemSupport struct {
	Built  Support
	Probes map[Kind][]ProbeResult
}

func NormalizeKind(raw string) Kind {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "hardware acceleration methods:", "hardware acceleration:", "encoders:":
		return None
	case "auto":
		return Auto
	case "vaapi":
		return VAAPI
	case "cuda", "nvenc":
		return CUDA
	case "qsv":
		return QSV
	case "videotoolbox":
		return VideoToolbox
	default:
		return None
	}
}

func (s Support) Has(kind Kind) bool {
	kind = NormalizeKind(string(kind))
	if kind == None {
		return false
	}
	return s.Accels[kind]
}

func (s Support) SupportsCodec(kind Kind, codec string) bool {
	encoder, err := BuildVideoCodec(kind, codec)
	if err != nil {
		return false
	}
	return s.Encoders[strings.ToLower(encoder)]
}

func (s Support) Kinds() []Kind {
	kinds := make([]Kind, 0, len(s.Accels))
	for kind, enabled := range s.Accels {
		if enabled {
			kinds = append(kinds, kind)
		}
	}
	slices.Sort(kinds)
	return kinds
}

func (s SystemSupport) Available(kind Kind) bool {
	for _, probe := range s.Probes[NormalizeKind(string(kind))] {
		if probe.Available {
			return true
		}
	}
	return false
}

func (s SystemSupport) Device(kind Kind) (string, bool) {
	for _, probe := range s.Probes[NormalizeKind(string(kind))] {
		if probe.Available {
			return probe.Device, true
		}
	}
	return "", false
}

func (s SystemSupport) SupportsCodec(kind Kind, codec string) bool {
	return s.Built.SupportsCodec(kind, codec) && s.Available(kind)
}

func (s SystemSupport) Select(codec string, preferred ...Kind) (Kind, string, error) {
	for _, kind := range selectOrder(preferred) {
		if s.SupportsCodec(kind, codec) {
			device, _ := s.Device(kind)
			return kind, device, nil
		}
	}
	return None, "", fmt.Errorf("no runtime hwaccel support available for codec %q", normalizeCodec(codec))
}
