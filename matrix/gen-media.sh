#!/bin/sh
# gen-media.sh — generate the small synthetic media set used by the ffprobe
# conformance fixtures. Run once with a full-featured ffmpeg (libx264, libx265,
# libvpx, libopus, lame); the outputs are committed under ffprobe/testdata/media.
#
# usage: matrix/gen-media.sh [outdir]
set -eu
out=${1:-ffprobe/testdata/media}
mkdir -p "$out"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

FF="ffmpeg -hide_banner -loglevel error -y"
V="-f lavfi -i testsrc2=size=32x32:rate=25:duration=1"
A="-f lavfi -i sine=frequency=440:sample_rate=48000:duration=1"

cat > "$tmp/chapters.txt" <<'EOF'
;FFMETADATA1
title=Muxmix Test
artist=muxmix
[CHAPTER]
TIMEBASE=1/1000
START=0
END=500
title=Intro
[CHAPTER]
TIMEBASE=1/1000
START=500
END=1000
title=Outro
EOF
printf 'hello from muxmix\n' > "$tmp/attachment.txt"
cat > "$tmp/subs.srt" <<'EOF'
1
00:00:00,000 --> 00:00:00,500
first

2
00:00:00,500 --> 00:00:01,000
second
EOF

# 1. Plain mp4: h264 + aac, chapters, container tags.
$FF $V $A -i "$tmp/chapters.txt" -map 0:v -map 1:a -map_metadata 2 \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p -g 12 -c:a aac -b:a 32k \
  -movflags +faststart "$out/basic.mp4"

# 2. Matroska with everything: video, two audio languages, forced subtitle,
#    attachment, per-stream tags and dispositions.
$FF $V $A -f lavfi -i "sine=frequency=880:sample_rate=48000:duration=1" -i "$tmp/subs.srt" \
  -attach "$tmp/attachment.txt" -metadata:s:t mimetype=text/plain \
  -map 0:v -map 1:a -map 2:a -map 3:s \
  -c:v libx264 -preset ultrafast -pix_fmt yuv420p -c:a:0 aac -b:a:0 32k -c:a:1 libopus -b:a:1 32k -c:s srt \
  -metadata:s:a:0 language=eng -metadata:s:a:0 title=English \
  -metadata:s:a:1 language=deu -metadata:s:a:1 title=Deutsch \
  -metadata:s:s:0 language=eng -disposition:a:0 default -disposition:s:0 forced \
  -metadata title=Multi "$out/multi.mkv"

# 3. MPEG-TS with two programs (exercises programs[].streams nesting).
$FF $V $A -f lavfi -i "sine=frequency=880:sample_rate=48000:duration=1" \
  -map 0:v -map 1:a -map 2:a -c:v libx264 -preset ultrafast -pix_fmt yuv420p -c:a aac -b:a 32k \
  -program title=First:st=0:st=1 -program title=Second:st=0:st=2 -f mpegts "$out/programs.ts"

# 4. WebM: vp9 + opus.
$FF $V $A -c:v libvpx-vp9 -deadline realtime -cpu-used 8 -c:a libopus -b:a 32k "$out/vp9.webm"

# 5. Display-matrix side data on the stream (rotation).
$FF -display_rotation 90 -i "$out/basic.mp4" -c copy -map 0:v "$out/rotated.mp4"

# 6. HDR10 metadata: mastering display + content light level side data.
$FF $V -c:v libx265 -preset ultrafast -pix_fmt yuv420p10le \
  -color_primaries bt2020 -color_trc smpte2084 -colorspace bt2020nc -color_range tv \
  -x265-params "master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400:log-level=none" \
  -tag:v hvc1 "$out/hdr.mp4"

# 7. Audio-only containers.
$FF $A -c:a flac "$out/audio.flac"
$FF -f lavfi -i "sine=frequency=440:sample_rate=8000:duration=0.5" -c:a pcm_s16le "$out/audio.wav"

# 8. mp3 with an attached cover picture (attached_pic disposition).
$FF -f lavfi -i "color=c=red:size=16x16:duration=1" -frames:v 1 "$tmp/cover.png"
$FF $A -i "$tmp/cover.png" -map 0:a -map 1:v -c:a libmp3lame -b:a 32k -c:v png \
  -id3v2_version 3 -metadata:s:v title=Cover -metadata:s:v comment=Cover \
  -disposition:v attached_pic "$out/cover.mp3"

# 9. Raw elementary stream: no container timing, N/A values everywhere.
$FF $V -c:v libx264 -preset ultrafast -pix_fmt yuv420p -bsf:v h264_mp4toannexb -f h264 "$out/raw.h264"

# 10. QuickTime with a timecode track (data stream) and a bt709 tagged video.
$FF $V -c:v libx264 -preset ultrafast -pix_fmt yuv420p -timecode 01:00:00:00 \
  -color_primaries bt709 -color_trc bt709 -colorspace bt709 "$out/timecode.mov"

# 11. IAMF stream groups (ffmpeg 7.0+); skipped when the muxer is unavailable.
if ffmpeg -hide_banner -h muxer=iamf >/dev/null 2>&1; then
  $FF $A -f lavfi -i "sine=frequency=880:sample_rate=48000:duration=1" \
    -map 0:a -map 1:a -c:a libopus -b:a 32k \
    -stream_group "type=iamf_audio_element:id=1:st=0:st=1,demixing=parameter_id=998,recon_gain=parameter_id=101,layer=ch_layout=mono,layer=ch_layout=stereo" \
    -stream_group "type=iamf_mix_presentation:id=2:stg=0:annotations=en-us=Default,submix=parameter_id=100:parameter_rate=48000|element=stg=0:parameter_id=100:annotations=en-us=Sub|layout=sound_system=stereo" \
    -streamid 0:0 -streamid 1:1 "$out/groups.iamf" || { rm -f "$out/groups.iamf"; echo "iamf generation failed (non-fatal)" >&2; }
fi

ls -la "$out"
