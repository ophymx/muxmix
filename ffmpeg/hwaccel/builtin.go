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

// codecs lists the table's codec names in a stable order.
func (t codecTable) codecs() []string {
	names := make([]string, 0, len(t))
	for codec := range t {
		names = append(names, codec)
	}
	sort.Strings(names)
	return names
}

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

// deviceArgs initialises a device named "hw" and points filters at it:
// -init_hw_device vaapi=hw:/dev/dri/renderD128 -filter_hw_device hw.
func deviceArgs(typ, device string) []string {
	spec := typ + "=hw"
	if device != "" {
		spec += ":" + device
	}
	return []string{"-init_hw_device", spec, "-filter_hw_device", "hw"}
}

func probeBase() []string { return []string{"-hide_banner", "-loglevel", "error"} }

// NV12 and P010 are the upload formats the encoder probes try: 8-bit and
// 10-bit 4:2:0. NV12 is what every hardware encoder takes, and what a
// Selection uploads in unless told otherwise.
const (
	NV12 = "nv12"
	P010 = "p010"
)

// probeFormats are tried in order, 8-bit first. A codec that fails in
// 8-bit is not tried deeper.
var probeFormats = []string{NV12, P010}

// orDefault is the format a backend uploads in when the caller named none.
func orDefault(format string) string {
	if format == "" {
		return NV12
	}
	return format
}

// deepUploadFormat is the upload format that keeps a source pixel
// format's bit depth, or "" when it is 8-bit or a layout this pipeline
// does not carry. Only 4:2:0 is covered, which is what hardware encoders
// take; anything else uploads as 8-bit, as it always has.
func deepUploadFormat(pixFmt string) string {
	switch strings.ToLower(strings.TrimSpace(pixFmt)) {
	case "yuv420p10", "yuv420p10le", "yuv420p10be", "p010", "p010le", "p010be":
		return P010
	}
	return ""
}

// probeSize is the frame the encoder probes encode. It is deliberately
// not tiny: NVENC rejects frames narrower than 145 pixels and hevc_vaapi
// needs at least one coding tree unit, so a 16x16 probe would fail on
// hardware that works.
const probeSize = "320x240"

// encoderProbeArgs builds the command that encodes one frame with
// encoder on device, uploaded in format, from the backend's own device
// and filter arguments.
func encoderProbeArgs(kind Kind, device, encoder, format string) ([]string, error) {
	b, err := lookupOrErr(kind)
	if err != nil {
		return nil, err
	}
	if prober, ok := b.(EncoderProber); ok {
		return prober.EncoderProbeArgs(device, encoder)
	}
	device, err = probeDevice(b, device)
	if err != nil {
		return nil, err
	}
	deviceArgs, err := b.DeviceArgs(device)
	if err != nil {
		return nil, err
	}
	chain, err := b.Filter(format)
	if err != nil {
		return nil, err
	}
	args := append(probeBase(), deviceArgs...)
	args = append(args, "-f", "lavfi", "-i", "color=s="+probeSize+":d=0.1")
	if chain != "" {
		args = append(args, "-vf", chain)
	}
	return append(args, "-frames:v", "1", "-c:v", encoder, "-f", "null", "-"), nil
}

// probeDevice falls back to the backend's first default device when none
// is given, so a hand-written ProbeEncoder call needs no device.
func probeDevice(b Backend, device string) (string, error) {
	if device != "" {
		return device, nil
	}
	devices := b.DefaultDevices()
	if len(devices) == 0 {
		return "", fmt.Errorf("no device candidates for %s", b.Kind())
	}
	return devices[0], nil
}

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

func (vaapiBackend) DeviceArgs(device string) ([]string, error) {
	if device == "" {
		device = defaultVAAPIDevice()
	}
	if device == "" {
		return nil, fmt.Errorf("vaapi device is required")
	}
	return deviceArgs("vaapi", device), nil
}

func (vaapiBackend) Filter(format string, extra ...string) (string, error) {
	return joinFilters(extra, "format="+orDefault(format), "hwupload"), nil
}

var vaapiCodecs = codecTable{
	"av1": "av1_vaapi", "h264": "h264_vaapi", "hevc": "hevc_vaapi",
	"mjpeg": "mjpeg_vaapi", "vp8": "vp8_vaapi", "vp9": "vp9_vaapi",
}

func (vaapiBackend) VideoCodec(codec string) (string, error) { return vaapiCodecs.encoder(codec) }

func (vaapiBackend) VideoCodecs() []string { return vaapiCodecs.codecs() }

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

func (cudaBackend) DeviceArgs(device string) ([]string, error) {
	return deviceArgs("cuda", device), nil
}

// Filter uploads without converting when no format is asked for: CUDA
// frames carry the source's own depth, so a 10-bit source stays 10-bit.
func (cudaBackend) Filter(format string, extra ...string) (string, error) {
	if format == "" {
		return joinFilters(extra, "hwupload_cuda"), nil
	}
	return joinFilters(extra, "format="+format, "hwupload_cuda"), nil
}

var cudaCodecs = codecTable{"av1": "av1_nvenc", "h264": "h264_nvenc", "hevc": "hevc_nvenc"}

func (cudaBackend) VideoCodec(codec string) (string, error) { return cudaCodecs.encoder(codec) }

func (cudaBackend) VideoCodecs() []string { return cudaCodecs.codecs() }

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

func (qsvBackend) DeviceArgs(device string) ([]string, error) {
	return deviceArgs("qsv", device), nil
}

func (qsvBackend) Filter(format string, extra ...string) (string, error) {
	return joinFilters(extra, "format="+orDefault(format), "hwupload"), nil
}

var qsvCodecs = codecTable{
	"av1": "av1_qsv", "h264": "h264_qsv", "hevc": "hevc_qsv",
	"mjpeg": "mjpeg_qsv", "vp8": "vp8_qsv", "vp9": "vp9_qsv",
}

func (qsvBackend) VideoCodec(codec string) (string, error) { return qsvCodecs.encoder(codec) }

func (qsvBackend) VideoCodecs() []string { return qsvCodecs.codecs() }

// ─── VideoToolbox ──────────────────────────────────────────────────────────

type videoToolboxBackend struct{}

func (videoToolboxBackend) Kind() Kind { return VideoToolbox }

func (videoToolboxBackend) DefaultDevices() []string { return []string{""} }

func (videoToolboxBackend) ProbeArgs(string) ([]string, error) {
	return append(append(probeBase(), "-init_hw_device", "videotoolbox=probe"), probeTail()...), nil
}

func (videoToolboxBackend) DeviceArgs(string) ([]string, error) { return nil, nil }

func (videoToolboxBackend) Filter(format string, extra ...string) (string, error) {
	if format == "" {
		return joinFilters(extra), nil
	}
	return joinFilters(extra, "format="+format), nil
}

var videoToolboxCodecs = codecTable{
	"h264": "h264_videotoolbox", "hevc": "hevc_videotoolbox", "prores": "prores_videotoolbox",
}

func (videoToolboxBackend) VideoCodec(codec string) (string, error) {
	return videoToolboxCodecs.encoder(codec)
}

func (videoToolboxBackend) VideoCodecs() []string { return videoToolboxCodecs.codecs() }

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
