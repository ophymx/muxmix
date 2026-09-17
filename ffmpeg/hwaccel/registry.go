package hwaccel

import (
	"fmt"
	"strings"
	"sync"
)

// Backend describes one hardware acceleration method: how to probe it, how
// to feed it, and which encoders it provides. Register adds a Backend so
// Detect, Select and Selection know about it; the built-in backends
// (VAAPI, CUDA, QSV, VideoToolbox) are registered at init.
//
// The pipeline a Backend describes is software decode, upload, hardware
// encode: DeviceArgs initialise the device as global options and Filter
// ends the filter chain with the upload. Hardware decoding is not part of
// the interface yet.
type Backend interface {
	// Kind is the backend's name, as ffmpeg -hwaccels prints it.
	Kind() Kind
	// DefaultDevices lists device candidates to probe on this host. An
	// empty string means "no device argument".
	DefaultDevices() []string
	// ProbeArgs returns a complete ffmpeg argument list that succeeds only
	// when the backend works with the given device.
	ProbeArgs(device string) ([]string, error)
	// DeviceArgs returns the global options that initialise the device
	// for filters and encoders (-init_hw_device, -filter_hw_device). Nil
	// when the backend needs none.
	DeviceArgs(device string) ([]string, error)
	// Filter returns the filter chain that runs extraFilters and then
	// moves frames to the device.
	Filter(extraFilters ...string) (string, error)
	// VideoCodec maps a codec name ("h264") to the backend's encoder.
	VideoCodec(codec string) (string, error)
}

var registry = struct {
	mu       sync.RWMutex
	backends map[Kind]Backend
	order    []Kind
	aliases  map[string]Kind
}{
	backends: map[Kind]Backend{},
	aliases:  map[string]Kind{},
}

// Register adds a backend. It panics if the kind is already registered.
func Register(b Backend) {
	kind := Kind(strings.ToLower(strings.TrimSpace(string(b.Kind()))))
	if kind == None {
		panic("hwaccel: cannot register backend with empty kind")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if _, dup := registry.backends[kind]; dup {
		panic(fmt.Sprintf("hwaccel: backend %q already registered", kind))
	}
	registry.backends[kind] = b
	registry.order = append(registry.order, kind)
}

// RegisterAlias makes NormalizeKind map alias (e.g. "nvenc") to kind.
func RegisterAlias(alias string, kind Kind) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.aliases[strings.ToLower(strings.TrimSpace(alias))] = kind
}

// Lookup returns the backend registered for kind.
func Lookup(kind Kind) (Backend, bool) {
	kind = NormalizeKind(string(kind))
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	b, ok := registry.backends[kind]
	return b, ok
}

// Kinds returns the registered kinds in registration order, which is also
// the default probe and selection order.
func Kinds() []Kind {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return append([]Kind(nil), registry.order...)
}

// lookupOrErr resolves a kind for the builders: a name that is not
// registered is an error rather than a silent fall back to software.
func lookupOrErr(kind Kind) (Backend, error) {
	b, ok := Lookup(kind)
	if !ok {
		return nil, fmt.Errorf("unsupported hwaccel %q", strings.TrimSpace(string(kind)))
	}
	return b, nil
}
