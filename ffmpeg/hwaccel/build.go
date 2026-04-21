package hwaccel

import (
	"fmt"
	"strings"
)

func BuildInputArgs(kind Kind, device string) ([]string, error) {
	kind = NormalizeKind(string(kind))
	switch kind {
	case None:
		return nil, nil
	case Auto:
		return nil, fmt.Errorf("cannot build args for unresolved hwaccel %q", kind)
	case VAAPI:
		if device == "" {
			device = defaultVAAPIDevice()
		}
		if device == "" {
			return nil, fmt.Errorf("vaapi device is required")
		}
		return []string{"-vaapi_device", device}, nil
	case CUDA:
		args := []string{"-hwaccel", "cuda"}
		if device != "" {
			args = append(args, "-hwaccel_device", device)
		}
		return args, nil
	case QSV:
		args := []string{"-hwaccel", "qsv"}
		if device != "" {
			args = append(args, "-qsv_device", device)
		}
		return args, nil
	case VideoToolbox:
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported hwaccel %q", kind)
	}
}

func BuildFilter(kind Kind, filters ...string) (string, error) {
	kind = NormalizeKind(string(kind))
	parts := make([]string, 0, len(filters)+2)
	for _, filter := range filters {
		filter = strings.TrimSpace(filter)
		if filter != "" {
			parts = append(parts, filter)
		}
	}

	switch kind {
	case None, VideoToolbox:
	case Auto:
		return "", fmt.Errorf("cannot build filter for unresolved hwaccel %q", kind)
	case VAAPI, QSV:
		parts = append(parts, "format=nv12", "hwupload")
	case CUDA:
		parts = append(parts, "hwupload_cuda")
	default:
		return "", fmt.Errorf("unsupported hwaccel %q", kind)
	}

	return strings.Join(parts, ","), nil
}

func BuildVideoCodec(kind Kind, codec string) (string, error) {
	kind = NormalizeKind(string(kind))
	codec = normalizeCodec(codec)
	if codec == "" {
		return "", fmt.Errorf("codec is required")
	}

	switch kind {
	case None:
		return codec, nil
	case Auto:
		return "", fmt.Errorf("cannot build codec for unresolved hwaccel %q", kind)
	case VAAPI:
		return lookupCodec(codec, map[string]string{
			"av1":   "av1_vaapi",
			"h264":  "h264_vaapi",
			"hevc":  "hevc_vaapi",
			"mjpeg": "mjpeg_vaapi",
			"vp8":   "vp8_vaapi",
			"vp9":   "vp9_vaapi",
		})
	case CUDA:
		return lookupCodec(codec, map[string]string{
			"av1":  "av1_nvenc",
			"h264": "h264_nvenc",
			"hevc": "hevc_nvenc",
		})
	case QSV:
		return lookupCodec(codec, map[string]string{
			"av1":   "av1_qsv",
			"h264":  "h264_qsv",
			"hevc":  "hevc_qsv",
			"mjpeg": "mjpeg_qsv",
			"vp8":   "vp8_qsv",
			"vp9":   "vp9_qsv",
		})
	case VideoToolbox:
		return lookupCodec(codec, map[string]string{
			"h264":   "h264_videotoolbox",
			"hevc":   "hevc_videotoolbox",
			"prores": "prores_videotoolbox",
		})
	default:
		return "", fmt.Errorf("unsupported hwaccel %q", kind)
	}
}

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

func normalizeCodec(codec string) string {
	codec = strings.ToLower(strings.TrimSpace(codec))
	switch codec {
	case "h265":
		return "hevc"
	default:
		return codec
	}
}

func lookupCodec(codec string, mapping map[string]string) (string, error) {
	encoder, ok := mapping[codec]
	if !ok {
		return "", fmt.Errorf("codec %q is not supported", codec)
	}
	return encoder, nil
}
