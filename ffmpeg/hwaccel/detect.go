package hwaccel

import (
	"context"
	"fmt"
	"slices"
	"strings"

	baseffmpeg "github.com/ophymx/muxmix/ffmpeg"
)

func Detect(ctx context.Context, runner baseffmpeg.Runner) (Support, error) {
	if runner == nil {
		runner = baseffmpeg.DefaultRunner
	}

	hwaccels, err := runDetectCommand(ctx, runner, "-hide_banner", "-hwaccels")
	if err != nil {
		return Support{}, fmt.Errorf("detect ffmpeg hwaccels: %w", err)
	}

	encoders, err := runDetectCommand(ctx, runner, "-hide_banner", "-encoders")
	if err != nil {
		return Support{}, fmt.Errorf("detect ffmpeg encoders: %w", err)
	}

	return ParseDetection(hwaccels, encoders), nil
}

func ParseDetection(hwaccelsOutput, encodersOutput string) Support {
	support := Support{
		Accels:   make(map[Kind]bool),
		Encoders: ParseVideoEncoders(encodersOutput),
	}
	for _, kind := range ParseHardwareAccelerators(hwaccelsOutput) {
		support.Accels[kind] = true
	}
	return support
}

func ParseHardwareAccelerators(output string) []Kind {
	seen := make(map[Kind]bool)
	for line := range strings.SplitSeq(output, "\n") {
		kind := NormalizeKind(strings.TrimSpace(line))
		if kind == None || kind == Auto {
			continue
		}
		seen[kind] = true
	}
	kinds := make([]Kind, 0, len(seen))
	for kind := range seen {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

func ParseVideoEncoders(output string) map[string]bool {
	encoders := make(map[string]bool)
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !strings.Contains(fields[0], "V") {
			continue
		}
		encoders[strings.ToLower(fields[1])] = true
	}
	return encoders
}

func runDetectCommand(ctx context.Context, runner baseffmpeg.Runner, args ...string) (string, error) {
	result, err := runner.RunWithOptions(ctx, baseffmpeg.RunOptions{
		Args:               args,
		DisableDefaultArgs: true,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(result.Stdout) + "\n" + string(result.Stderr)), nil
}
