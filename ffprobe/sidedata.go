package ffprobe

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Side data entries carry keys that depend on their side_data_type; the
// generated types keep them in Extra. The types in this file give the
// common ones names. Decode any entry with SideDataAs, or use the accessors
// on Stream, Packet and Frame.
//
// The type names are the strings ffprobe prints in side_data_type.
const (
	SideDataDisplayMatrix      = "Display Matrix"
	SideDataStereo3D           = "Stereo 3D"
	SideDataSpherical          = "Spherical Mapping"
	SideDataSkipSamples        = "Skip Samples"
	SideDataMasteringDisplay   = "Mastering display metadata"
	SideDataContentLightLevel  = "Content light level metadata"
	SideDataAmbientViewing     = "Ambient viewing environment"
	SideDataDolbyVisionConfig  = "DOVI configuration record"
	SideDataAudioServiceType   = "Audio Service Type"
	SideDataMPEGTSStreamID     = "MPEGTS Stream ID"
	SideDataCPBProperties      = "CPB properties"
	SideDataFrameCropping      = "Frame Cropping"
	SideDataActiveFormat       = "Active Format Description"
	SideDataGOPTimecode        = "GOP timecode"
	SideDataS12MTimecode       = "SMPTE 12-1 timecode"
	SideDataHDR10Plus          = "HDR10+ Dynamic Metadata (SMPTE 2094-40)"
	SideDataFrameHDR10Plus     = "HDR Dynamic Metadata SMPTE2094-40 (HDR10+)"
	SideDataDolbyVisionRPU     = "Dolby Vision RPU Data"
	SideDataDolbyVisionMeta    = "Dolby Vision Metadata"
	SideDataICCProfile         = "ICC profile"
	SideDataUserDataUnregister = "H.26[45] User Data Unregistered SEI message"
)

// SideDataEntry is implemented by SideData and FrameSideData.
type SideDataEntry interface {
	// Type returns the side_data_type string.
	Type() string
	// Fields returns the variable keys ffprobe printed for this entry.
	Fields() map[string]any
}

// Type returns the side_data_type.
func (s *SideData) Type() string { return s.SideDataType }

// Fields returns the entry's variable keys.
func (s *SideData) Fields() map[string]any { return s.Extra }

// Type returns the side_data_type.
func (s *FrameSideData) Type() string { return s.SideDataType }

// Fields returns the entry's variable keys.
func (s *FrameSideData) Fields() map[string]any { return s.Extra }

// DecodeSideData fills v (a pointer to a struct with json tags) from an
// entry's variable keys. The lenient scalar types work as field types.
func DecodeSideData(entry SideDataEntry, v any) error {
	b, err := json.Marshal(entry.Fields())
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// SideDataAs finds the first entry of the given type in a Stream, Packet or
// Frame SideDataList and decodes it into T. It returns false when no such
// entry exists or it does not decode.
//
//	dm, ok := ffprobe.SideDataAs[ffprobe.DisplayMatrix](stream.SideDataList, ffprobe.SideDataDisplayMatrix)
func SideDataAs[T any, E SideDataEntry](list []E, typeName string) (*T, bool) {
	for _, entry := range list {
		if strings.EqualFold(entry.Type(), typeName) {
			var v T
			if err := DecodeSideData(entry, &v); err != nil {
				return nil, false
			}
			return &v, true
		}
	}
	return nil, false
}

// ─── typed side data ───────────────────────────────────────────────────────

// DisplayMatrix is the rotation and 3x3 transform ffmpeg attaches to video.
type DisplayMatrix struct {
	// Rotation in degrees exactly as ffprobe prints it, which is
	// av_display_rotation_get's counter-clockwise angle in [-180, 180]: a
	// phone video that must turn 90 degrees clockwise to display upright
	// reports -90, and a file with the legacy "rotate=270" tag reports 90.
	Rotation Seconds `json:"rotation"`
	// Matrix is ffprobe's hex dump of the 3x3 fixed-point matrix.
	Matrix string `json:"displaymatrix"`
}

// Degrees returns the clockwise rotation a player applies before display,
// normalised to 0, 90, 180 or 270: the negation of Rotation, which is what
// ffmpeg's own autorotate and the legacy "rotate" tag use.
func (d *DisplayMatrix) Degrees() int {
	return normaliseDegrees(-int(d.Rotation.Float64()))
}

// Values returns the nine matrix coefficients (16.16 fixed point, as in
// the ISO BMFF tkhd box), or nil when the dump could not be parsed.
func (d *DisplayMatrix) Values() []int32 {
	var out []int32
	for _, line := range strings.Split(d.Matrix, "\n") {
		_, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		for _, f := range strings.Fields(rest) {
			n, err := strconv.ParseInt(f, 10, 64)
			if err != nil {
				return nil
			}
			out = append(out, int32(n))
		}
	}
	if len(out) != 9 {
		return nil
	}
	return out
}

// Stereo3D describes stereoscopic packing.
type Stereo3D struct {
	Type                          string `json:"type"`
	Inverted                      Bool   `json:"inverted"`
	View                          string `json:"view"`
	PrimaryEye                    string `json:"primary_eye"`
	Baseline                      Int    `json:"baseline"`
	HorizontalDisparityAdjustment Rat    `json:"horizontal_disparity_adjustment"`
	HorizontalFieldOfView         Rat    `json:"horizontal_field_of_view"`
}

// Spherical describes 360° video projection.
type Spherical struct {
	Projection  string `json:"projection"`
	Padding     Int    `json:"padding"`
	BoundLeft   Int    `json:"bound_left"`
	BoundTop    Int    `json:"bound_top"`
	BoundRight  Int    `json:"bound_right"`
	BoundBottom Int    `json:"bound_bottom"`
	Yaw         Int    `json:"yaw"`
	Pitch       Int    `json:"pitch"`
	Roll        Int    `json:"roll"`
}

// SkipSamples is the encoder delay and padding on an audio packet.
type SkipSamples struct {
	SkipSamples    Int `json:"skip_samples"`
	DiscardPadding Int `json:"discard_padding"`
	SkipReason     Int `json:"skip_reason"`
	DiscardReason  Int `json:"discard_reason"`
}

// MasteringDisplay is the HDR mastering display colour volume (SMPTE
// ST 2086). Primaries are CIE 1931 chromaticity coordinates; luminance is
// in candela per square metre.
type MasteringDisplay struct {
	RedX         Rat `json:"red_x"`
	RedY         Rat `json:"red_y"`
	GreenX       Rat `json:"green_x"`
	GreenY       Rat `json:"green_y"`
	BlueX        Rat `json:"blue_x"`
	BlueY        Rat `json:"blue_y"`
	WhitePointX  Rat `json:"white_point_x"`
	WhitePointY  Rat `json:"white_point_y"`
	MinLuminance Rat `json:"min_luminance"`
	MaxLuminance Rat `json:"max_luminance"`
}

// HasPrimaries reports whether the primaries were present.
func (m *MasteringDisplay) HasPrimaries() bool { return m.RedX.Valid() && m.WhitePointX.Valid() }

// HasLuminance reports whether the luminance range was present.
func (m *MasteringDisplay) HasLuminance() bool { return m.MaxLuminance.Valid() }

// MaxNits returns the mastering display's peak luminance in cd/m² (1000
// for a typical HDR10 grade), or 0 when absent.
func (m *MasteringDisplay) MaxNits() float64 { return m.MaxLuminance.Float64() }

// MinNits returns the mastering display's black level in cd/m² (0.0001
// or 0.005 for a typical HDR10 grade), or 0 when absent.
func (m *MasteringDisplay) MinNits() float64 { return m.MinLuminance.Float64() }

// ContentLightLevel is the HDR content light level (CTA-861.3).
type ContentLightLevel struct {
	MaxContent Int `json:"max_content"` // MaxCLL in cd/m²
	MaxAverage Int `json:"max_average"` // MaxFALL in cd/m²
}

// AmbientViewingEnvironment is the H.274 ambient viewing environment.
type AmbientViewingEnvironment struct {
	AmbientIlluminance Rat `json:"ambient_illuminance"`
	AmbientLightX      Rat `json:"ambient_light_x"`
	AmbientLightY      Rat `json:"ambient_light_y"`
}

// DolbyVisionConfig is the Dolby Vision configuration record.
type DolbyVisionConfig struct {
	VersionMajor            Int    `json:"dv_version_major"`
	VersionMinor            Int    `json:"dv_version_minor"`
	Profile                 Int    `json:"dv_profile"`
	Level                   Int    `json:"dv_level"`
	RPUPresent              Bool   `json:"rpu_present_flag"`
	ELPresent               Bool   `json:"el_present_flag"`
	BLPresent               Bool   `json:"bl_present_flag"`
	BLSignalCompatibilityID Int    `json:"dv_bl_signal_compatibility_id"`
	MDCompression           string `json:"dv_md_compression"`
}

// AudioServiceType is the AC-3 style audio service type.
type AudioServiceType struct {
	ServiceType Int `json:"service_type"`
}

// CPBProperties are the coded picture buffer constraints.
type CPBProperties struct {
	MaxBitrate Int `json:"max_bitrate"`
	MinBitrate Int `json:"min_bitrate"`
	AvgBitrate Int `json:"avg_bitrate"`
	BufferSize Int `json:"buffer_size"`
	VBVDelay   Int `json:"vbv_delay"`
}

// FrameCropping is the cropping ffmpeg applies after decoding.
type FrameCropping struct {
	Top    Int `json:"crop_top"`
	Bottom Int `json:"crop_bottom"`
	Left   Int `json:"crop_left"`
	Right  Int `json:"crop_right"`
}

// ─── accessors ─────────────────────────────────────────────────────────────

// DisplayMatrix returns the stream's display matrix, or nil.
func (s *Stream) DisplayMatrix() *DisplayMatrix {
	v, _ := SideDataAs[DisplayMatrix](s.SideDataList, SideDataDisplayMatrix)
	return v
}

// Stereo3D returns the stream's stereoscopic packing, or nil.
func (s *Stream) Stereo3D() *Stereo3D {
	v, _ := SideDataAs[Stereo3D](s.SideDataList, SideDataStereo3D)
	return v
}

// Spherical returns the stream's 360° projection, or nil.
func (s *Stream) Spherical() *Spherical {
	v, _ := SideDataAs[Spherical](s.SideDataList, SideDataSpherical)
	return v
}

// MasteringDisplay returns the stream's HDR mastering metadata, or nil.
func (s *Stream) MasteringDisplay() *MasteringDisplay {
	v, _ := SideDataAs[MasteringDisplay](s.SideDataList, SideDataMasteringDisplay)
	return v
}

// ContentLightLevel returns the stream's HDR light level, or nil.
func (s *Stream) ContentLightLevel() *ContentLightLevel {
	v, _ := SideDataAs[ContentLightLevel](s.SideDataList, SideDataContentLightLevel)
	return v
}

// DolbyVision returns the stream's Dolby Vision configuration, or nil.
func (s *Stream) DolbyVision() *DolbyVisionConfig {
	v, _ := SideDataAs[DolbyVisionConfig](s.SideDataList, SideDataDolbyVisionConfig)
	return v
}

// CPBProperties returns the stream's buffer constraints, or nil.
func (s *Stream) CPBProperties() *CPBProperties {
	v, _ := SideDataAs[CPBProperties](s.SideDataList, SideDataCPBProperties)
	return v
}

// HDR10Plus reports whether the stream header carries HDR10+ dynamic
// metadata.
func (s *Stream) HDR10Plus() bool { return hasSideData(s.SideDataList, SideDataHDR10Plus) }

// IsHDR reports whether the stream header signals HDR: a PQ or HLG
// transfer, mastering display or light level metadata, Dolby Vision or
// HDR10+. Many HEVC and AV1 files carry that metadata only as per-frame
// SEI, which ffprobe's stream section does not show; use Prober.Color,
// which falls back to the first frame, for a reliable answer.
func (s *Stream) IsHDR() bool { return colorOf(s).IsHDR() }

// SkipSamples returns the packet's encoder delay and padding, or nil.
func (p *Packet) SkipSamples() *SkipSamples {
	v, _ := SideDataAs[SkipSamples](p.SideDataList, SideDataSkipSamples)
	return v
}

// DisplayMatrix returns the frame's display matrix, or nil.
func (f *Frame) DisplayMatrix() *DisplayMatrix {
	v, _ := SideDataAs[DisplayMatrix](f.SideDataList, SideDataDisplayMatrix)
	return v
}

// MasteringDisplay returns the frame's HDR mastering metadata, or nil.
func (f *Frame) MasteringDisplay() *MasteringDisplay {
	v, _ := SideDataAs[MasteringDisplay](f.SideDataList, SideDataMasteringDisplay)
	return v
}

// ContentLightLevel returns the frame's HDR light level, or nil.
func (f *Frame) ContentLightLevel() *ContentLightLevel {
	v, _ := SideDataAs[ContentLightLevel](f.SideDataList, SideDataContentLightLevel)
	return v
}

// HDR10Plus reports whether the frame carries HDR10+ dynamic metadata.
func (f *Frame) HDR10Plus() bool { return hasSideData(f.SideDataList, SideDataFrameHDR10Plus) }

// hasSideData reports whether the list has an entry of the given type.
func hasSideData[E SideDataEntry](list []E, typeName string) bool {
	for _, entry := range list {
		if strings.EqualFold(entry.Type(), typeName) {
			return true
		}
	}
	return false
}
