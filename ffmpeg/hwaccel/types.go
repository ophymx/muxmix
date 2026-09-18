package hwaccel

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ophymx/muxmix/ffmpeg/caps"
)

// Kind names a hardware acceleration backend, as ffmpeg -hwaccels prints
// it. None means software.
type Kind string

const (
	None         Kind = ""
	VAAPI        Kind = "vaapi"
	CUDA         Kind = "cuda"
	QSV          Kind = "qsv"
	VideoToolbox Kind = "videotoolbox"
)

// String prints "software" for None.
func (k Kind) String() string {
	if k == None {
		return "software"
	}
	return string(k)
}

// ProbeOptions narrows what Detect probes.
type ProbeOptions struct {
	// Kinds limits probing to these backends; nil probes every registered
	// one.
	Kinds []Kind
	// Devices overrides the device candidates per backend; a kind not
	// listed uses the backend's own defaults.
	Devices map[Kind][]string
	// Codecs limits the encoder probes to these codecs; nil probes every
	// codec the backend maps and the build has an encoder for.
	Codecs []string
	// NoEncoderProbe stops Detect from probing encoders at all, leaving
	// only the device probes. Selection then trusts the build's encoder
	// list, which can offer a codec the device cannot encode.
	NoEncoderProbe bool
}

// ProbeResult is the outcome of trying one backend on one device: whether
// the device initialised and, when it did, which of the backend's encoders
// actually encoded a frame on it.
type ProbeResult struct {
	Kind      Kind     `json:"kind"`
	Device    string   `json:"device,omitempty"`
	Args      []string `json:"args,omitempty"`
	Available bool     `json:"available"`
	Error     string   `json:"error,omitempty"`
	// Encoders is one entry per codec probed on this device, in codec
	// order. It is empty when the device did not initialise, when
	// ProbeOptions.NoEncoderProbe was set, or when the System was built
	// by hand; Select then trusts the build's encoder list.
	Encoders []EncoderProbe `json:"encoders,omitempty"`
}

// EncoderProbe is the outcome of encoding one frame with one encoder on
// one device. A build can carry an encoder the silicon does not implement
// -- av1_vaapi on a chip without AV1 encode, av1_nvenc before Ada -- and
// only running it tells the two apart.
type EncoderProbe struct {
	Codec   string `json:"codec"`
	Encoder string `json:"encoder"`
	Works   bool   `json:"works"`
	// Formats are the upload formats that encoded, 8-bit first ("nv12",
	// "p010"). A device whose list has p010 keeps a 10-bit source at 10
	// bits; see System.PreserveDepth.
	Formats []string `json:"formats,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// encoder returns the probe for codec, if it was probed.
func (p ProbeResult) encoder(codec string) (EncoderProbe, bool) {
	codec = normalizeCodec(codec)
	for _, e := range p.Encoders {
		if e.Codec == codec {
			return e, true
		}
	}
	return EncoderProbe{}, false
}

// System is what one ffmpeg binary can do on this machine: the build's
// capabilities and, for each hardware backend, whether it actually
// initialises here and on which device. Detect it once at startup, or
// load it with DetectCached, and share it between jobs.
type System struct {
	Caps       *caps.Set              `json:"caps"`
	Probes     map[Kind][]ProbeResult `json:"probes"`
	DetectedAt time.Time              `json:"detected_at"`
}

// NormalizeKind maps a name from ffmpeg output or user input to a
// registered Kind: case-insensitive, with aliases (for example "nvenc" for
// cuda). Unknown names yield None.
func NormalizeKind(raw string) Kind {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return None
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	if kind, ok := registry.aliases[name]; ok {
		return kind
	}
	if _, ok := registry.backends[Kind(name)]; ok {
		return Kind(name)
	}
	return None
}

// Built reports whether the ffmpeg build includes the backend at all.
func (s *System) Built(kind Kind) bool {
	kind = NormalizeKind(string(kind))
	return s != nil && s.Caps != nil && kind != None && s.Caps.HasHWAccel(string(kind))
}

// Available reports whether the backend initialised on at least one device.
func (s *System) Available(kind Kind) bool {
	_, ok := s.Device(kind)
	return ok
}

// Device returns the first device the backend initialised on. The device
// is "" for backends that take none.
func (s *System) Device(kind Kind) (string, bool) {
	probe, ok := s.availableProbe(kind)
	return probe.Device, ok
}

// availableProbe returns the first probe on which the backend initialised.
func (s *System) availableProbe(kind Kind) (ProbeResult, bool) {
	if s == nil {
		return ProbeResult{}, false
	}
	for _, probe := range s.Probes[NormalizeKind(string(kind))] {
		if probe.Available {
			return probe, true
		}
	}
	return ProbeResult{}, false
}

// PreserveDepth returns sel with the upload format to use for a source in
// srcPixFmt. A deep source keeps its depth when the device encoded that
// format during detection, and is flattened to 8-bit explicitly when it
// did not -- CUDA uploads a source's own format by default, so without
// the explicit nv12 a 10-bit source reaches an 8-bit-only encoder and the
// job fails. An 8-bit source, a software selection, or a System with no
// probe for that encoder comes back unchanged.
func (s *System) PreserveDepth(sel Selection, srcPixFmt string) Selection {
	if !sel.Hardware() {
		return sel
	}
	format := deepUploadFormat(srcPixFmt)
	if format == "" {
		return sel
	}
	probe, ok := s.availableProbe(sel.Kind)
	if !ok {
		return sel
	}
	e, probed := probe.encoder(sel.Codec)
	if !probed {
		return sel
	}
	if slices.Contains(e.Formats, format) {
		sel.Format = format
	} else {
		sel.Format = NV12
	}
	return sel
}

// AvailableKinds lists the backends that initialised, in registration order.
func (s *System) AvailableKinds() []Kind {
	var kinds []Kind
	for _, kind := range Kinds() {
		if s.Available(kind) {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

// HasEncoder reports whether the build has the named encoder. With no
// caps it reports true, so a System built by hand without a Set does not
// refuse everything.
func (s *System) HasEncoder(name string) bool {
	if s == nil || s.Caps == nil {
		return true
	}
	return s.Caps.HasEncoder(name)
}

// SupportsCodec reports whether the backend is usable here and its
// encoder for codec is in the build and, when Detect probed it, worked on
// the device.
func (s *System) SupportsCodec(kind Kind, codec string) bool {
	_, err := s.check(kind, codec)
	return err == nil
}

// check explains why a backend cannot encode codec, or returns the encoder.
func (s *System) check(kind Kind, codec string) (string, error) {
	encoder, err := VideoEncoder(kind, codec)
	if err != nil {
		return "", err
	}
	if !s.HasEncoder(encoder) {
		return "", fmt.Errorf("build lacks %s", encoder)
	}
	if !s.Available(kind) {
		if s.Caps != nil && !s.Built(kind) {
			return "", fmt.Errorf("not in this ffmpeg build")
		}
		if s != nil {
			for _, p := range s.Probes[kind] {
				if p.Error != "" {
					return "", fmt.Errorf("probe failed: %s", firstLine(p.Error))
				}
			}
		}
		return "", fmt.Errorf("not probed")
	}
	if probe, ok := s.availableProbe(kind); ok {
		if e, probed := probe.encoder(codec); probed && !e.Works {
			return "", fmt.Errorf("%s did not encode on %s: %s", encoder, probe.deviceLabel(), firstLine(e.Error))
		}
	}
	return encoder, nil
}

// Select picks the first backend, in preference order (registration order
// when none is given), that is usable here and can encode codec. The
// error lists why each candidate was rejected.
func (s *System) Select(codec string, preferred ...Kind) (Selection, error) {
	se := &SelectError{Codec: normalizeCodec(codec)}
	for _, kind := range selectOrder(preferred) {
		encoder, err := s.check(kind, codec)
		if err == nil {
			device, _ := s.Device(kind)
			return Selection{Kind: kind, Device: device, Encoder: encoder, Codec: se.Codec}, nil
		}
		se.Tried = append(se.Tried, kind)
		se.Reasons = append(se.Reasons, err.Error())
	}
	return Selection{Codec: se.Codec}, se
}

// SelectError reports why no backend was selected for a codec.
type SelectError struct {
	Codec   string
	Tried   []Kind
	Reasons []string // parallel to Tried
}

func (e *SelectError) Error() string {
	return "hwaccel: no usable backend for " + e.Codec + ": " + e.Detail()
}

// Detail is the per-backend reasons as "vaapi: probe failed: ...; cuda: ...".
func (e *SelectError) Detail() string {
	if len(e.Tried) == 0 {
		return "no backends registered"
	}
	parts := make([]string, len(e.Tried))
	for i, k := range e.Tried {
		parts[i] = string(k) + ": " + e.Reasons[i]
	}
	return strings.Join(parts, "; ")
}

func selectOrder(preferred []Kind) []Kind {
	if len(preferred) == 0 {
		return Kinds()
	}
	seen := make(map[Kind]bool, len(preferred))
	order := make([]Kind, 0, len(preferred))
	for _, kind := range preferred {
		kind = NormalizeKind(string(kind))
		if kind == None || seen[kind] {
			continue
		}
		seen[kind] = true
		order = append(order, kind)
	}
	return order
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// sortProbes puts successful probes first, then by device name.
func sortProbes(probes []ProbeResult) {
	slices.SortStableFunc(probes, func(a, b ProbeResult) int {
		if a.Available != b.Available {
			if a.Available {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Device, b.Device)
	})
}

// deviceLabel names the device for an error message, for backends that
// take none as well.
func (p ProbeResult) deviceLabel() string {
	if p.Device == "" {
		return string(p.Kind)
	}
	return p.Device
}
