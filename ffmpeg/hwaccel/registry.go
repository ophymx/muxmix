package hwaccel

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Backend describes one hardware acceleration method: how to probe it, how
// to feed it, and which encoders it provides. Register adds a Backend so
// Detect, DetectSystem, Select and the Build functions know about it; the
// built-in backends (VAAPI, CUDA, QSV, VideoToolbox) are registered at init.
type Backend interface {
	// Kind is the backend's name, as ffmpeg -hwaccels prints it.
	Kind() Kind
	// DefaultDevices lists device candidates to probe on this host. An
	// empty string means "no device argument".
	DefaultDevices() []string
	// ProbeArgs returns a complete ffmpeg argument list that succeeds only
	// when the backend works with the given device.
	ProbeArgs(device string) ([]string, error)
	// InputArgs returns the options placed before -i to decode or upload
	// with this backend.
	InputArgs(device string) ([]string, error)
	// Filter returns the -vf chain that moves frames to the device,
	// after any extra filters.
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
	if kind == None || kind == Auto {
		panic("hwaccel: cannot register backend with empty or auto kind")
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

// SortedKinds returns the registered kinds sorted by name.
func SortedKinds() []Kind {
	kinds := Kinds()
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

func lookupNoneError(kind Kind) error {
	return fmt.Errorf("cannot probe hwaccel %q", NormalizeKind(string(kind)))
}

// lookupOrErr resolves a kind for the Build functions: None means software
// (nil backend, nil error), Auto is an error because it has not been
// resolved, and a name that is not registered is an error rather than
// silently falling back to software.
func lookupOrErr(kind Kind, what string) (Backend, error) {
	raw := strings.ToLower(strings.TrimSpace(string(kind)))
	switch raw {
	case "":
		return nil, nil
	case "auto":
		return nil, fmt.Errorf("cannot build %s for unresolved hwaccel %q", what, raw)
	}
	b, ok := Lookup(Kind(raw))
	if !ok {
		return nil, fmt.Errorf("unsupported hwaccel %q", raw)
	}
	return b, nil
}
