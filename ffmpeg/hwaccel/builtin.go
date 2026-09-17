package hwaccel

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func init() {
	Register(vaapiBackend{})
	Register(cudaBackend{})
	Register(qsvBackend{})
	Register(videoToolboxBackend{})
	RegisterAlias("nvenc", CUDA)
}

// codecTable maps codec names to a backend's encoder names.
type codecTable map[string]string

func (t codecTable) encoder(codec string) (string, error) {
	codec = normalizeCodec(codec)
	if codec == "" {
		return "", fmt.Errorf("codec is required")
	}
	encoder, ok := t[codec]
	if !ok {
		return "", fmt.Errorf("codec %q is not supported", codec)
	}
	return encoder, nil
}

func joinFilters(extra []string, tail ...string) string {
	parts := make([]string, 0, len(extra)+len(tail))
	for _, f := range extra {
		if f = strings.TrimSpace(f); f != "" {
			parts = append(parts, f)
		}
	}
	return strings.Join(append(parts, tail...), ",")
}

func probeBase() []string { return []string{"-hide_banner", "-loglevel", "error"} }

func probeTail() []string {
	return []string{"-f", "lavfi", "-i", "color=s=16x16:d=0.1", "-frames:v", "1", "-f", "null", "-"}
}

// ─── VAAPI ─────────────────────────────────────────────────────────────────

type vaapiBackend struct{}

func (vaapiBackend) Kind() Kind { return VAAPI }

func (vaapiBackend) DefaultDevices() []string { return renderNodes() }

func (vaapiBackend) ProbeArgs(device string) ([]string, error) {
	if device == "" {
		return nil, fmt.Errorf("vaapi probe device is required")
	}
	args := append(probeBase(), "-init_hw_device", "vaapi=probe:"+device,
		"-f", "lavfi", "-i", "color=s=16x16:d=0.1",
		"-vf", "format=nv12,hwupload",
		"-frames:v", "1", "-f", "null", "-")
	return args, nil
}

func (vaapiBackend) InputArgs(device string) ([]string, error) {
	if device == "" {
		device = defaultVAAPIDevice()
	}
	if device == "" {
		return nil, fmt.Errorf("vaapi device is required")
	}
	return []string{"-vaapi_device", device}, nil
}

func (vaapiBackend) Filter(extra ...string) (string, error) {
	return joinFilters(extra, "format=nv12", "hwupload"), nil
}

func (vaapiBackend) VideoCodec(codec string) (string, error) {
	return codecTable{
		"av1": "av1_vaapi", "h264": "h264_vaapi", "hevc": "hevc_vaapi",
		"mjpeg": "mjpeg_vaapi", "vp8": "vp8_vaapi", "vp9": "vp9_vaapi",
	}.encoder(codec)
}

// ─── CUDA / NVENC ──────────────────────────────────────────────────────────

type cudaBackend struct{}

func (cudaBackend) Kind() Kind { return CUDA }

func (cudaBackend) DefaultDevices() []string { return []string{""} }

func (cudaBackend) ProbeArgs(device string) ([]string, error) {
	spec := "cuda=probe"
	if device != "" {
		spec += ":" + device
	}
	args := append(probeBase(), "-init_hw_device", spec,
		"-f", "lavfi", "-i", "color=s=16x16:d=0.1",
		"-vf", "hwupload_cuda",
		"-frames:v", "1", "-f", "null", "-")
	return args, nil
}

func (cudaBackend) InputArgs(device string) ([]string, error) {
	args := []string{"-hwaccel", "cuda"}
	if device != "" {
		args = append(args, "-hwaccel_device", device)
	}
	return args, nil
}

func (cudaBackend) Filter(extra ...string) (string, error) {
	return joinFilters(extra, "hwupload_cuda"), nil
}

func (cudaBackend) VideoCodec(codec string) (string, error) {
	return codecTable{"av1": "av1_nvenc", "h264": "h264_nvenc", "hevc": "hevc_nvenc"}.encoder(codec)
}

// ─── QSV ───────────────────────────────────────────────────────────────────

type qsvBackend struct{}

func (qsvBackend) Kind() Kind { return QSV }

func (qsvBackend) DefaultDevices() []string { return renderNodes() }

func (qsvBackend) ProbeArgs(device string) ([]string, error) {
	spec := "qsv=probe"
	if device != "" {
		spec += ":" + device
	}
	return append(append(probeBase(), "-init_hw_device", spec), probeTail()...), nil
}

func (qsvBackend) InputArgs(device string) ([]string, error) {
	args := []string{"-hwaccel", "qsv"}
	if device != "" {
		args = append(args, "-qsv_device", device)
	}
	return args, nil
}

func (qsvBackend) Filter(extra ...string) (string, error) {
	return joinFilters(extra, "format=nv12", "hwupload"), nil
}

func (qsvBackend) VideoCodec(codec string) (string, error) {
	return codecTable{
		"av1": "av1_qsv", "h264": "h264_qsv", "hevc": "hevc_qsv",
		"mjpeg": "mjpeg_qsv", "vp8": "vp8_qsv", "vp9": "vp9_qsv",
	}.encoder(codec)
}

// ─── VideoToolbox ──────────────────────────────────────────────────────────

type videoToolboxBackend struct{}

func (videoToolboxBackend) Kind() Kind { return VideoToolbox }

func (videoToolboxBackend) DefaultDevices() []string { return []string{""} }

func (videoToolboxBackend) ProbeArgs(string) ([]string, error) {
	return append(append(probeBase(), "-init_hw_device", "videotoolbox=probe"), probeTail()...), nil
}

func (videoToolboxBackend) InputArgs(string) ([]string, error) { return nil, nil }

func (videoToolboxBackend) Filter(extra ...string) (string, error) {
	return joinFilters(extra), nil
}

func (videoToolboxBackend) VideoCodec(codec string) (string, error) {
	return codecTable{
		"h264": "h264_videotoolbox", "hevc": "hevc_videotoolbox", "prores": "prores_videotoolbox",
	}.encoder(codec)
}

// ─── shared helpers ────────────────────────────────────────────────────────

func defaultVAAPIDevice() string {
	if runtime.GOOS == "linux" {
		return "/dev/dri/renderD128"
	}
	return ""
}

// renderNodes lists /dev/dri/renderD* on Linux, or the default VAAPI device
// if it exists, or nothing.
func renderNodes() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	matches, err := filepath.Glob("/dev/dri/renderD*")
	if err != nil {
		return nil
	}
	if len(matches) == 0 {
		if d := defaultVAAPIDevice(); d != "" {
			if _, err := os.Stat(d); err == nil {
				return []string{d}
			}
		}
		return nil
	}
	sort.Strings(matches)
	return matches
}

func normalizeCodec(codec string) string {
	codec = strings.ToLower(strings.TrimSpace(codec))
	if codec == "h265" {
		return "hevc"
	}
	return codec
}
