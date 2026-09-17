package hwaccel

import "fmt"

// Mode is how strongly a Policy wants hardware.
type Mode int

const (
	// Software never uses a hardware backend. It is the zero value.
	Software Mode = iota
	// Prefer uses a usable backend when one supports the codec and falls
	// back to software otherwise.
	Prefer
	// Require fails when no usable backend supports the codec.
	Require
)

func (m Mode) String() string {
	switch m {
	case Prefer:
		return "prefer"
	case Require:
		return "require"
	}
	return "software"
}

// Policy says how a job should use hardware encoding. The zero value is
// software only; PreferHardware and RequireHardware build the others.
type Policy struct {
	Mode Mode
	// Kinds are the backends to try, in order. Nil means every registered
	// backend in registration order.
	Kinds []Kind
}

// PreferHardware uses the first usable backend among kinds (or any) and
// falls back to software.
func PreferHardware(kinds ...Kind) Policy { return Policy{Mode: Prefer, Kinds: kinds} }

// RequireHardware uses the first usable backend among kinds (or any) and
// fails otherwise.
func RequireHardware(kinds ...Kind) Policy { return Policy{Mode: Require, Kinds: kinds} }

// Resolve picks how to encode codec under this policy. A software result
// under Prefer comes with the reason hardware was not used, suitable for a
// plan's explanation; Require turns that reason into an error. sys may be
// nil, which counts as "no hardware detected".
func (p Policy) Resolve(sys *System, codec string) (sel Selection, reason string, err error) {
	sel = Selection{Codec: normalizeCodec(codec)}
	if p.Mode == Software {
		return sel, "", nil
	}
	if sys == nil {
		reason = "no hardware detection available"
	} else {
		hw, serr := sys.Select(codec, p.Kinds...)
		if serr == nil {
			return hw, "", nil
		}
		var se *SelectError
		if ok := asSelectError(serr, &se); ok {
			reason = se.Detail()
		} else {
			reason = serr.Error()
		}
	}
	if p.Mode == Require {
		return sel, reason, fmt.Errorf("hwaccel: hardware encoding required for %s but unavailable: %s", sel.Codec, reason)
	}
	return sel, reason, nil
}

func asSelectError(err error, target **SelectError) bool {
	se, ok := err.(*SelectError)
	if ok {
		*target = se
	}
	return ok
}
