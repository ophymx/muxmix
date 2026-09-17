package caps_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ophymx/muxmix/ffmpeg"
	"github.com/ophymx/muxmix/ffmpeg/caps"
)

func captureDirs(t *testing.T) []string {
	t.Helper()
	dirs, _ := filepath.Glob(filepath.Join("..", "testdata", "capture", "*"))
	if len(dirs) == 0 {
		t.Skip("no captures")
	}
	return dirs
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Skipf("%s missing", name)
	}
	return string(b)
}

func TestParseListings(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			ver, err := ffmpeg.ParseVersion(read(t, dir, "version.txt"))
			if err != nil {
				t.Fatal(err)
			}

			enc := caps.ParseCodecs(read(t, dir, "encoders.txt"))
			if len(enc) < 100 {
				t.Errorf("encoders = %d", len(enc))
			}
			var x264 *caps.Codec
			for i := range enc {
				if enc[i].Name == "libx264" {
					x264 = &enc[i]
				}
			}
			if x264 == nil || x264.Type != caps.Video || x264.CodecID != "h264" || strings.Contains(x264.Description, "(codec") {
				t.Errorf("libx264 = %+v", x264)
			}
			set := &caps.Set{Encoders: enc}
			if n := len(set.EncodersFor("h264")); n < 1 {
				t.Errorf("EncodersFor(h264) = %d", n)
			}
			if aac := set.Encoder("aac"); aac == nil || aac.Type != caps.Audio || aac.CodecID != "aac" {
				t.Errorf("aac = %+v", aac)
			}

			dec := caps.ParseCodecs(read(t, dir, "decoders.txt"))
			if h := (&caps.Set{Decoders: dec}).Decoder("h264"); h == nil || h.Type != caps.Video || !h.FrameThreading {
				t.Errorf("h264 decoder = %+v", h)
			}

			mux := caps.ParseFormats(read(t, dir, "muxers.txt"))
			s := &caps.Set{Muxers: mux, Demuxers: caps.ParseFormats(read(t, dir, "demuxers.txt"))}
			if m := s.Muxer("mp4"); m == nil || !m.Mux || m.Demux {
				t.Errorf("mp4 muxer = %+v", m)
			}
			if d := s.Demuxer("mp4"); d == nil || !d.Demux || d.Mux || !strings.HasPrefix(d.Name, "mov,mp4") {
				t.Errorf("mp4 demuxer = %+v", d)
			}
			if !s.HasMuxer("null") || !s.HasDemuxer("lavfi") || s.HasMuxer("nonexistent") {
				t.Error("format lookups")
			}

			filters := caps.ParseFilters(read(t, dir, "filters.txt"))
			fs := &caps.Set{Filters: filters}
			if len(filters) < 200 {
				t.Errorf("filters = %d", len(filters))
			}
			if sc := fs.Filter("scale"); sc == nil || sc.Inputs != "V" || sc.Outputs != "V" {
				t.Errorf("scale = %+v", sc)
			} else if sc.Command == ver.AtLeast(8, 1) {
				t.Errorf("scale command flag = %v on %s", sc.Command, ver.Version)
			}
			if ov := fs.Filter("overlay"); ov == nil || ov.Inputs != "VV" || !ov.Timeline {
				t.Errorf("overlay = %+v", ov)
			}
			if src := fs.Filter("testsrc2"); src == nil || !src.IsSource() || src.Outputs != "V" {
				t.Errorf("testsrc2 = %+v", src)
			}
			if sink := fs.Filter("nullsink"); sink == nil || !sink.IsSink() {
				t.Errorf("nullsink = %+v", sink)
			}
			if am := fs.Filter("amix"); am == nil || am.Inputs != "N" {
				t.Errorf("amix = %+v", am)
			}

			pix := caps.ParsePixelFormats(read(t, dir, "pix_fmts.txt"))
			ps := &caps.Set{PixelFormats: pix}
			if p := ps.PixelFormat("yuv420p"); p == nil || p.Components != 3 || p.BitsPerPixel != 12 || !p.Input || !p.Output || p.Hardware {
				t.Errorf("yuv420p = %+v", p)
			} else if ver.AtLeast(7, 0) && (len(p.BitDepths) != 3 || p.BitDepths[0] != 8) {
				t.Errorf("yuv420p bit depths = %v", p.BitDepths)
			}
			var hardware int
			for _, p := range pix {
				if p.Hardware && strings.HasPrefix(p.Name, "vaapi") {
					hardware++
				}
			}
			if hardware == 0 {
				t.Error("no vaapi hardware pixel format")
			}
			if p := ps.PixelFormat("pal8"); p == nil || !p.Paletted {
				t.Errorf("pal8 = %+v", p)
			}

			sf := caps.ParseSampleFormats(read(t, dir, "sample_fmts.txt"))
			if len(sf) < 10 || sf[0].Name != "u8" || sf[0].Depth != 8 {
				t.Errorf("sample formats = %+v", sf)
			}
			var fltp *caps.SampleFormat
			for i := range sf {
				if sf[i].Name == "fltp" {
					fltp = &sf[i]
				}
			}
			if fltp == nil || !fltp.Planar() || fltp.Depth != 32 {
				t.Errorf("fltp = %+v", fltp)
			}

			hw := caps.ParseList(read(t, dir, "hwaccels.txt"))
			if len(hw) == 0 || hw[0] == "Hardware acceleration methods:" {
				t.Errorf("hwaccels = %v", hw)
			}
			bsfs := caps.ParseList(read(t, dir, "bsfs.txt"))
			if !(&caps.Set{BitstreamFilters: bsfs}).HasBitstreamFilter("h264_mp4toannexb") {
				t.Errorf("bsfs = %v", bsfs)
			}
			pr := caps.ParseProtocols(read(t, dir, "protocols.txt"))
			pset := &caps.Set{Protocols: pr}
			if !pset.HasProtocol("file", false) || !pset.HasProtocol("file", true) || !pset.HasProtocol("http", false) {
				t.Errorf("protocols = %+v", pr)
			}
		})
	}
}

func TestParseHelp(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			ver, _ := ffmpeg.ParseVersion(read(t, dir, "version.txt"))

			enc := caps.ParseHelp(read(t, dir, "help-encoder-libx264.txt"))
			if enc.Kind != "encoder" || enc.Name != "libx264" || !strings.Contains(enc.Description, "H.264") {
				t.Errorf("header = %+v", enc)
			}
			if !enc.SupportsPixelFormat("yuv420p") || enc.SupportsPixelFormat("rgb24") {
				t.Errorf("pixel formats = %v", enc.PixelFormats)
			}
			if len(enc.Capabilities) == 0 || enc.Threading == "" {
				t.Errorf("capabilities = %v threading = %q", enc.Capabilities, enc.Threading)
			}
			crf := enc.Option("crf")
			if crf == nil || crf.Type != "float" || crf.Min != "-1" || crf.Max != "FLT_MAX" || crf.Default != "-1" || !crf.Encoding() || !crf.VideoOption() || !strings.Contains(crf.Help, "constant quality") {
				t.Errorf("crf = %+v", crf)
			}
			preset := enc.Option("preset")
			if preset == nil || preset.Type != "string" || preset.Default != "medium" {
				t.Errorf("preset = %+v", preset)
			}
			aq := enc.Option("aq-mode")
			if aq == nil || len(aq.Constants) < 3 || aq.Constant("variance") == nil || aq.Constant("variance").Value != "1" {
				t.Errorf("aq-mode = %+v", aq)
			}
			if len(enc.OptionGroups) != 1 || !strings.Contains(enc.OptionGroups[0].Name, "libx264") {
				t.Errorf("groups = %+v", enc.OptionGroups)
			}

			dec := caps.ParseHelp(read(t, dir, "help-decoder-h264.txt"))
			if dec.Kind != "decoder" || dec.Name != "h264" || dec.Option("enable_er") == nil || !dec.Option("enable_er").Decoding() || len(dec.HardwareDevices) == 0 {
				t.Errorf("decoder = %+v", dec)
			}

			mux := caps.ParseHelp(read(t, dir, "help-muxer-mp4.txt"))
			if mux.Kind != "muxer" || mux.Name != "mp4" || mux.MimeType != "video/mp4" || mux.DefaultCodecs[caps.Video] != "h264" || mux.DefaultCodecs[caps.Audio] != "aac" || len(mux.Extensions) != 1 || mux.Extensions[0] != "mp4" {
				t.Errorf("mp4 = %+v", mux)
			}
			if mf := mux.Option("movflags"); mf == nil || mf.Type != "flags" || mf.Constant("faststart") == nil {
				t.Errorf("movflags = %+v", mf)
			}

			dem := caps.ParseHelp(read(t, dir, "help-demuxer-mov.txt"))
			if dem.Kind != "demuxer" || !strings.Contains(strings.Join(dem.Extensions, ","), "m4a") || dem.Option("ignore_editlist") == nil {
				t.Errorf("mov = %+v", dem)
			}

			sc := caps.ParseHelp(read(t, dir, "help-filter-scale.txt"))
			if sc.Kind != "filter" || sc.Name != "scale" || len(sc.Inputs) != 1 || sc.Inputs[0].Type != caps.Video || len(sc.Outputs) != 1 {
				t.Errorf("scale = %+v", sc)
			}
			if w := sc.Option("w"); w == nil || !w.Filtering() {
				t.Errorf("w = %+v", w)
			} else if ver.AtLeast(6, 0) && !w.Runtime() {
				t.Errorf("w should be a runtime option on %s: %+v", ver.Version, w)
			}
			if cm := sc.Option("in_color_matrix"); cm == nil {
				t.Error("in_color_matrix missing")
			} else if ver.AtLeast(7, 0) && (cm.Constant("bt709") == nil || cm.Constant("bt709").Value != "1") {
				t.Errorf("in_color_matrix = %+v", cm)
			}

			bsf := caps.ParseHelp(read(t, dir, "help-bsf-h264_mp4toannexb.txt"))
			if bsf.Kind != "bsf" || bsf.Name != "h264_mp4toannexb" || len(bsf.Codecs) != 1 || bsf.Codecs[0] != "h264" {
				t.Errorf("bsf = %+v", bsf)
			}

			if unknown := caps.ParseHelp("Codec 'nope' is not recognized by FFmpeg.\n"); unknown.Name != "" {
				t.Errorf("unknown = %+v", unknown)
			}
		})
	}
}

func TestDetectLive(t *testing.T) {
	if err := ffmpeg.ValidateInstall(); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	set, err := caps.Detect(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if set.Version == nil || len(set.Encoders) == 0 || len(set.Filters) == 0 || !set.HasMuxer("mp4") || !set.HasProtocol("file", true) {
		t.Errorf("set = %+v", set)
	}
	h, err := caps.FilterHelp(ctx, nil, "scale")
	if err != nil || h.Option("w") == nil {
		t.Errorf("scale help = %+v %v", h, err)
	}
	if _, err := caps.EncoderHelp(ctx, nil, "definitely_not_an_encoder"); err == nil {
		t.Error("expected error for unknown encoder")
	}
}
