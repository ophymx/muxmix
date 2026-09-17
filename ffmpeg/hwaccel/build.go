package hwaccel

import (
	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

// BuildInputArgs returns the options placed before -i for the backend.
// None yields no arguments; Auto and unregistered kinds are errors.
func BuildInputArgs(kind Kind, device string) ([]string, error) {
	b, err := lookupOrErr(kind, "args")
	if err != nil || b == nil {
		return nil, err
	}
	return b.InputArgs(device)
}

// BuildFilter returns the -vf chain that uploads frames to the backend
// after the given filters. None returns just the filters joined.
func BuildFilter(kind Kind, filters ...string) (string, error) {
	b, err := lookupOrErr(kind, "filter")
	if err != nil {
		return "", err
	}
	if b == nil {
		return joinFilters(filters), nil
	}
	return b.Filter(filters...)
}

// BuildVideoCodec maps a codec name to the backend's encoder. None returns
// the codec name unchanged.
func BuildVideoCodec(kind Kind, codec string) (string, error) {
	b, err := lookupOrErr(kind, "codec")
	if err != nil {
		return "", err
	}
	if b == nil {
		return codecTable{normalizeCodec(codec): normalizeCodec(codec)}.encoder(codec)
	}
	return b.VideoCodec(codec)
}

// BuildEncodeArgs combines input args, -vf and -c:v for one backend.
func BuildEncodeArgs(kind Kind, codec, device string, filters ...string) ([]string, error) {
	args, err := BuildInputArgs(kind, device)
	if err != nil {
		return nil, err
	}
	filter, err := BuildFilter(kind, filters...)
	if err != nil {
		return nil, err
	}
	videoCodec, err := BuildVideoCodec(kind, codec)
	if err != nil {
		return nil, err
	}
	if filter != "" {
		args = append(args, "-vf", filter)
	}
	args = append(args, "-c:v", videoCodec)
	return args, nil
}

// InputOpt returns the input options for a backend as an ffmpeg.Opt, for
// use with ffmpeg.Command:
//
//	cmd.Input(path, hwaccel.InputOpt(kind, device))
func InputOpt(kind Kind, device string) baseffmpeg.Opt {
	args, err := BuildInputArgs(kind, device)
	if err != nil || len(args) == 0 {
		return func(*baseffmpeg.Options) {}
	}
	return baseffmpeg.Raw(args...)
}

// EncodeOpt returns the output options (-vf and -c:v) for encoding with a
// backend as an ffmpeg.Opt.
func EncodeOpt(kind Kind, codec string, filters ...string) (baseffmpeg.Opt, error) {
	filter, err := BuildFilter(kind, filters...)
	if err != nil {
		return nil, err
	}
	videoCodec, err := BuildVideoCodec(kind, codec)
	if err != nil {
		return nil, err
	}
	return func(o *baseffmpeg.Options) {
		if filter != "" {
			o.Add(baseffmpeg.VideoFilter(filter))
		}
		o.Add(baseffmpeg.VideoCodec(videoCodec))
	}, nil
}

// DefaultProbeArgs returns the backend's probe command.
func DefaultProbeArgs(kind Kind, device string) ([]string, error) {
	b, err := lookupOrErr(kind, "probe")
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, lookupNoneError(kind)
	}
	return b.ProbeArgs(device)
}

// DefaultDevices returns the backend's device candidates on this host.
func DefaultDevices(kind Kind) []string {
	b, ok := Lookup(kind)
	if !ok {
		return nil
	}
	return b.DefaultDevices()
}
