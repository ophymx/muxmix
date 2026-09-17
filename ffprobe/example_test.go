package ffprobe_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"time"

	"github.com/ophymx/muxmix/ffprobe"
)

// Probe with no options shows the format and every stream. The helpers on
// Result and Stream fall back sensibly: Duration reads the container, then
// the longest stream (including Matroska's DURATION tag); Rotation reads
// the display matrix, then the legacy rotate tag.
func ExampleProbe() {
	ctx := context.Background()
	res, err := ffprobe.Probe(ctx, "movie.mkv")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		log.Fatal("no such file")
	case errors.Is(err, ffprobe.ErrInvalidData):
		log.Fatal("not a media file")
	case err != nil:
		log.Fatal(err)
	}

	fmt.Println(res.Format.FormatName, res.Duration(), res.Format.BitRate.Int64())
	if v := res.VideoStream(); v != nil {
		w, h := v.Resolution()
		fmt.Printf("%s %dx%d rotated %d fps %.3f %d frames\n",
			v.CodecName, w, h, v.Rotation(), v.FrameRate().Float64(), v.FrameCount())
	}
	for _, a := range res.AudioStreams() {
		fmt.Println(a.Language(), a.CodecName, a.Channels.OrInt(0), a.Bitrate())
	}
}

// Frames streams decoded frames one at a time. Leaving the loop early
// kills ffprobe; the last value carries any error it reported.
func ExampleProber_Frames() {
	ctx := context.Background()
	p := ffprobe.New(ffprobe.WithTimeout(time.Minute))

	var keyframes []time.Duration
	for f, err := range p.Frames(ctx, "movie.mp4", ffprobe.SelectStreams("v:0")) {
		if err != nil {
			log.Fatal(err)
		}
		if f.KeyFrame.Bool() {
			keyframes = append(keyframes, f.Time())
		}
		if len(keyframes) == 10 {
			break
		}
	}
	fmt.Println(keyframes)
}

// SideDataAs decodes any side data entry into a struct whose json tags name
// the keys ffprobe prints. The typed accessors on Stream (DisplayMatrix,
// MasteringDisplay, ...) are built on it.
func ExampleSideDataAs() {
	var s ffprobe.Stream
	_ = json.Unmarshal([]byte(`{
		"index": 0,
		"side_data_list": [
			{"side_data_type": "Display Matrix", "displaymatrix": "...", "rotation": -90},
			{"side_data_type": "CPB properties", "max_bitrate": 4000000, "buffer_size": 8000000}
		]
	}`), &s)

	type cpb struct {
		MaxBitrate ffprobe.Int `json:"max_bitrate"`
		BufferSize ffprobe.Int `json:"buffer_size"`
	}
	if props, ok := ffprobe.SideDataAs[cpb](s.SideDataList, ffprobe.SideDataCPBProperties); ok {
		fmt.Println(props.MaxBitrate, props.BufferSize)
	}
	if dm := s.DisplayMatrix(); dm != nil {
		fmt.Println(dm.Rotation, dm.Degrees(), s.Rotation())
	}
	// Output:
	// 4000000 8000000
	// -90.000000 90 90
}

// Color reads the stream's colour signalling and, when the container does
// not carry HDR metadata, decodes the first frame for the SEI that HEVC
// and AV1 streams usually keep there.
func ExampleColor() {
	ctx := context.Background()
	c, err := ffprobe.Color(ctx, "movie.mkv")
	if err != nil {
		log.Fatal(err)
	}
	if c == nil {
		fmt.Println("no video stream")
		return
	}
	fmt.Println(c.PixFmt, c.Primaries, c.Transfer, c.IsHDR())
	if c.Mastering != nil {
		fmt.Printf("mastered at %g nits (from frame: %v)\n", c.Mastering.MaxNits(), c.FromFrame)
	}
}

// Sections that only newer releases print are gated on the version, which
// is cached per Prober so the check costs one process in total.
func ExampleProber_Version() {
	ctx := context.Background()
	p := ffprobe.New()

	opts := []ffprobe.Option{ffprobe.ShowChapters()}
	if v, err := p.Version(ctx); err == nil && v.AtLeast(7, 0) {
		opts = append(opts, ffprobe.ShowStreamGroups())
	}
	res, err := p.Probe(ctx, "movie.iamf", opts...)
	if err != nil {
		log.Fatal(err)
	}
	for _, g := range res.StreamGroups {
		fmt.Println(g.Type, len(g.Streams))
	}
}

// AtLeast understands release, distribution and git snapshot spellings.
func ExampleVersionInfo_AtLeast() {
	for _, s := range []string{"7.1.5", "n8.0", "6.1.1-3ubuntu5", "4.4.2-0ubuntu0.22.04.1", "N-118000-g8f5d6f3"} {
		v := &ffprobe.VersionInfo{Program: ffprobe.ProgramVersion{Version: s}}
		fmt.Printf("%-24s >= 7.0: %-5v snapshot: %v\n", s, v.AtLeast(7, 0), v.Snapshot())
	}
	// Output:
	// 7.1.5                    >= 7.0: true  snapshot: false
	// n8.0                     >= 7.0: true  snapshot: false
	// 6.1.1-3ubuntu5           >= 7.0: false snapshot: false
	// 4.4.2-0ubuntu0.22.04.1   >= 7.0: false snapshot: false
	// N-118000-g8f5d6f3        >= 7.0: true  snapshot: true
}
